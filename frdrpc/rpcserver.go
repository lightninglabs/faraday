// Package frdrpc contains the proto files, generated code and server logic
// for faraday's grpc server which serves requests for close recommendations.
//
// The Faraday server interface is implemented by the RPCServer struct.
// To keep this file readable, each function implemented by the interface
// has a file named after the function call which contains rpc parsing
// code for the request and response. If the call requires extensive
// additional logic, and unexported function with the same name should
// be created in this file as well.
package frdrpc

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"sync"
	"sync/atomic"

	proxy "github.com/grpc-ecosystem/grpc-gateway/runtime"
	"github.com/lightninglabs/faraday/accounting"
	"github.com/lightninglabs/faraday/chain"
	"github.com/lightninglabs/faraday/fiat"
	"github.com/lightninglabs/faraday/recommend"
	"github.com/lightninglabs/faraday/resolutions"
	"github.com/lightninglabs/faraday/revenue"
	"github.com/lightninglabs/lndclient"
	"google.golang.org/grpc"
)

var (
	// customMarshalerOption is the configuratino we use for the JSON
	// marshaler of the REST proxy. The default JSON marshaler only sets
	// OrigName to true, which instructs it to use the same field names as
	// specified in the proto file and not switch to camel case. What we
	// also want is that the marshaler prints all values, even if they are
	// falsey.
	customMarshalerOption = proxy.WithMarshalerOption(
		proxy.MIMEWildcard, &proxy.JSONPb{
			OrigName:     true,
			EmitDefaults: true,
		},
	)

	// maxMsgRecvSize is the largest message our REST proxy will receive. We
	// set this to 200MiB atm.
	maxMsgRecvSize = grpc.MaxCallRecvMsgSize(1 * 1024 * 1024 * 200)

	// maxInvoiceQueries is the maximum number of invoices we request from
	// lnd at a time.
	maxInvoiceQueries = 1000

	// maxPaymentQueries is the maximum number of invoices we request from
	// lnd at a time. It is less than the number of invoices we request
	// because payments have a lot of htlc data.
	maxPaymentQueries = 500

	// maxForwardQueries is the maximum number of forwards we request from
	// lnd at a time. It is more than the number of invoices we request
	// because forwards have less data.
	maxForwardQueries = 2000

	// errServerAlreadyStarted is the error that is returned if the server
	// is requested to start while it's already been started.
	errServerAlreadyStarted = fmt.Errorf("server can only be started once")

	// ErrBitcoinNodeRequired is required when an endpoint which requires
	// a bitcoin node backend is hit and we are not connected to one.
	ErrBitcoinNodeRequired = errors.New("bitcoin node required")
)

// RPCServer implements the faraday service, serving requests over grpc.
type RPCServer struct {
	// To be used atomically.
	started int32

	// To be used atomically.
	stopped int32

	// cfg contains closures and settings required for operation.
	cfg *Config

	// grpcServer is the main gRPC RPCServer that this RPC server will
	// register itself with and accept client requests from.
	grpcServer *grpc.Server

	// rpcListener is the listener to use when starting the gRPC server.
	rpcListener net.Listener

	// restServer is the REST proxy server.
	restServer *http.Server

	restCancel func()
	wg         sync.WaitGroup
}

// Config provides closures and settings required to run the rpc server.
type Config struct {
	// Lnd is a client which can be used to query lnd.
	Lnd lndclient.LndServices

	// RPCListen is the address:port that the gRPC server should listen on.
	RPCListen string

	// RESTListen is the address:port that the REST server should listen on.
	RESTListen string

	// CORSOrigin specifies the CORS header that should be set on REST
	// responses. No header is added if the value is empty.
	CORSOrigin string

	// BitcoinClient is set if the client opted to connect to a bitcoin
	// backend, if not, it will be nil.
	BitcoinClient chain.BitcoinClient
}

// NewRPCServer returns a server which will listen for rpc requests on the
// rpc listen address provided. Note that the server returned is not running,
// and should be started using Start().
func NewRPCServer(cfg *Config) *RPCServer {
	var opts []grpc.ServerOption
	grpcServer := grpc.NewServer(opts...)

	return &RPCServer{
		cfg:        cfg,
		grpcServer: grpcServer,
	}
}

// Start starts the listener and server.
func (s *RPCServer) Start() error {
	if atomic.AddInt32(&s.started, 1) != 1 {
		return errServerAlreadyStarted
	}

	// Start the gRPC RPCServer listening for HTTP/2 connections.
	log.Info("Starting gRPC listener")
	grpcListener, err := net.Listen("tcp", s.cfg.RPCListen)
	if err != nil {
		return fmt.Errorf("RPC RPCServer unable to listen on %v",
			s.cfg.RPCListen)

	}
	s.rpcListener = grpcListener
	log.Infof("gRPC server listening on %s", grpcListener.Addr())

	RegisterFaradayServerServer(s.grpcServer, s)

	// We'll also create and start an accompanying proxy to serve clients
	// through REST. An empty address indicates REST is disabled.
	if s.cfg.RESTListen != "" {
		log.Infof("Starting REST proxy listener ")
		restListener, err := net.Listen("tcp", s.cfg.RESTListen)
		if err != nil {
			return fmt.Errorf("REST server unable to listen on %v",
				s.cfg.RESTListen)

		}
		log.Infof("REST server listening on %s", restListener.Addr())

		// We'll dial into the local gRPC server so we need to set some
		// gRPC dial options and CORS settings.
		var restCtx context.Context
		restCtx, s.restCancel = context.WithCancel(context.Background())
		mux := proxy.NewServeMux(customMarshalerOption)
		var restHandler http.Handler = mux
		if s.cfg.CORSOrigin != "" {
			restHandler = allowCORS(restHandler, s.cfg.CORSOrigin)
		}
		proxyOpts := []grpc.DialOption{
			grpc.WithInsecure(),
			grpc.WithDefaultCallOptions(maxMsgRecvSize),
		}
		err = RegisterFaradayServerHandlerFromEndpoint(
			restCtx, mux, s.cfg.RPCListen, proxyOpts,
		)
		if err != nil {
			return err
		}
		s.restServer = &http.Server{Handler: restHandler}

		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			err := s.restServer.Serve(restListener)
			// ErrServerClosed is always returned when the proxy is
			// shut down, so don't log it.
			if err != nil && err != http.ErrServerClosed {
				log.Error(err)
			}
		}()
	} else {
		log.Infof("REST proxy disabled")
	}

	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		if err := s.grpcServer.Serve(s.rpcListener); err != nil {
			log.Errorf("could not serve grpc server: %v", err)
		}
	}()

	return nil
}

// StartAsSubserver is an alternative to Start where the RPC server does not
// create its own gRPC server but registers to an existing one. The same goes
// for REST (if enabled), instead of creating an own mux and HTTP server, we
// register to an existing one.
func (s *RPCServer) StartAsSubserver(lndClient lndclient.LndServices) error {
	if atomic.AddInt32(&s.started, 1) != 1 {
		return errServerAlreadyStarted
	}

	s.cfg.Lnd = lndClient
	return nil
}

// Stop stops the grpc listener and server.
func (s *RPCServer) Stop() error {
	if atomic.AddInt32(&s.stopped, 1) != 1 {
		return nil
	}

	if s.restServer != nil {
		s.restCancel()
		err := s.restServer.Close()
		if err != nil {
			log.Errorf("unable to close REST listener: %v", err)
		}
	}

	// Stop the grpc server and wait for all go routines to terminate.
	if s.grpcServer != nil {
		s.grpcServer.Stop()
	}
	s.wg.Wait()

	return nil
}

// OutlierRecommendations provides a set of close recommendations for the
// current set of open channels based on whether they are outliers.
func (s *RPCServer) OutlierRecommendations(ctx context.Context,
	req *OutlierRecommendationsRequest) (*CloseRecommendationsResponse,
	error) {

	cfg, multiplier := parseOutlierRequest(ctx, s.cfg, req)

	report, err := recommend.OutlierRecommendations(cfg, multiplier)
	if err != nil {
		return nil, err
	}

	return rpcResponse(report), nil
}

// ThresholdRecommendations provides a set of close recommendations for the
// current set of open channels based on whether they are above or below a
// given threshold.
func (s *RPCServer) ThresholdRecommendations(ctx context.Context,
	req *ThresholdRecommendationsRequest) (*CloseRecommendationsResponse,
	error) {

	cfg, threshold := parseThresholdRequest(ctx, s.cfg, req)

	report, err := recommend.ThresholdRecommendations(cfg, threshold)
	if err != nil {
		return nil, err
	}

	return rpcResponse(report), nil
}

// RevenueReport returns a pairwise revenue report for a channel
// over the period requested.
func (s *RPCServer) RevenueReport(ctx context.Context,
	req *RevenueReportRequest) (*RevenueReportResponse, error) {

	revenueConfig := parseRevenueRequest(ctx, s.cfg, req)

	report, err := revenue.GetRevenueReport(revenueConfig)
	if err != nil {
		return nil, err
	}

	return rpcRevenueResponse(req.GetChanPoints(), report)
}

// ChannelInsights returns the channel insights for our currently open set
// of channels.
func (s *RPCServer) ChannelInsights(ctx context.Context,
	req *ChannelInsightsRequest) (*ChannelInsightsResponse, error) {

	insights, err := channelInsights(ctx, s.cfg)
	if err != nil {
		return nil, err
	}

	return rpcChannelInsightsResponse(insights), nil
}

// FiatEstimate provides a fiat estimate for a set of timestamped bitcoin
// prices.
func (s *RPCServer) ExchangeRate(ctx context.Context,
	req *ExchangeRateRequest) (*ExchangeRateResponse, error) {

	timestamps, granularity, err := parseExchangeRateRequest(req)
	if err != nil {
		return nil, err
	}

	prices, err := fiat.GetPrices(ctx, timestamps, granularity)
	if err != nil {
		return nil, err
	}

	return exchangeRateResponse(prices), nil
}

// NodeReport returns an on chain report for the period requested.
func (s *RPCServer) NodeReport(ctx context.Context,
	req *NodeReportRequest) (*NodeReportResponse, error) {

	if err := s.requireNode(); err != nil {
		return nil, err
	}

	onChain, offChain, err := parseNodeReportRequest(ctx, s.cfg, req)
	if err != nil {
		return nil, err
	}

	onChainReport, err := accounting.OnChainReport(ctx, onChain)
	if err != nil {
		return nil, err
	}

	offChainReport, err := accounting.OffChainReport(ctx, offChain)
	if err != nil {
		return nil, err
	}

	return rpcReportResponse(append(onChainReport, offChainReport...))
}

// CloseReport returns a close report for the channel provided. Note that this
// endpoint requires connection to an external bitcoind node.
func (s *RPCServer) CloseReport(ctx context.Context,
	req *CloseReportRequest) (*CloseReportResponse, error) {

	if err := s.requireNode(); err != nil {
		return nil, err
	}

	cfg := parseCloseReportRequest(ctx, s.cfg)

	report, err := resolutions.ChannelCloseReport(cfg, req.ChannelPoint)
	if err != nil {
		return nil, err
	}

	return rpcCloseReportResponse(report), nil
}

// requireNode fails if we do not have a connection to a backing bitcoin node.
func (s *RPCServer) requireNode() error {
	if s.cfg.BitcoinClient == nil {
		return ErrBitcoinNodeRequired
	}

	return nil
}

// allowCORS wraps the given http.Handler with a function that adds the
// Access-Control-Allow-Origin header to the response.
func allowCORS(handler http.Handler, origin string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", origin)
		handler.ServeHTTP(w, r)
	})
}
