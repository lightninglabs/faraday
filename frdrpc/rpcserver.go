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
	"fmt"
	"net"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	proxy "github.com/grpc-ecosystem/grpc-gateway/runtime"
	"github.com/lightninglabs/faraday/accounting"
	"github.com/lightninglabs/faraday/fiat"
	"github.com/lightninglabs/faraday/paginater"
	"github.com/lightninglabs/faraday/recommend"
	"github.com/lightninglabs/faraday/revenue"
	"github.com/lightningnetwork/lnd/lnrpc"
	"github.com/lightningnetwork/lnd/lnrpc/routerrpc"
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
	// LightningClient is a client which can be used to query lnd.
	LightningClient lnrpc.LightningClient

	// RouterClient is a client which can be used to access routing
	// functionality in lnd.
	RouterClient routerrpc.RouterClient

	// RPCListen is the address:port that the gRPC server should listen on.
	RPCListen string

	// RESTListen is the address:port that the REST server should listen on.
	RESTListen string

	// CORSOrigin specifies the CORS header that should be set on REST
	// responses. No header is added if the value is empty.
	CORSOrigin string
}

// wrapListChannels wraps the listchannels call to lnd, with a publicOnly bool
// that can be used to toggle whether private channels are included.
func (c *Config) wrapListChannels(ctx context.Context,
	publicOnly bool) func() ([]*lnrpc.Channel, error) {

	return func() (channels []*lnrpc.Channel, e error) {
		resp, err := c.LightningClient.ListChannels(
			ctx,
			&lnrpc.ListChannelsRequest{
				PublicOnly: publicOnly,
			},
		)
		if err != nil {
			return nil, err
		}

		return resp.Channels, nil
	}
}

func (c *Config) wrapClosedChannels(ctx context.Context) func() (
	[]*lnrpc.ChannelCloseSummary, error) {

	return func() ([]*lnrpc.ChannelCloseSummary, error) {
		resp, err := c.LightningClient.ClosedChannels(
			ctx, &lnrpc.ClosedChannelsRequest{},
		)
		if err != nil {
			return nil, err
		}

		return resp.Channels, nil
	}
}

func (c *Config) wrapGetChainTransactions(ctx context.Context) func() ([]*lnrpc.Transaction, error) {
	return func() (transactions []*lnrpc.Transaction, err error) {
		resp, err := c.LightningClient.GetTransactions(
			ctx, &lnrpc.GetTransactionsRequest{},
		)
		if err != nil {
			return nil, err

		}

		return resp.Transactions, nil
	}
}

// wrapListInvoices makes paginated calls to lnd to get our full set of
// invoices.
func (c *Config) wrapListInvoices(ctx context.Context) ([]*lnrpc.Invoice, error) {
	var invoices []*lnrpc.Invoice

	query := func(offset, maxEvents uint64) (uint64, uint64, error) {
		resp, err := c.LightningClient.ListInvoices(
			ctx, &lnrpc.ListInvoiceRequest{
				IndexOffset:    offset,
				NumMaxInvoices: maxEvents,
			},
		)
		if err != nil {
			return 0, 0, err
		}

		invoices = append(invoices, resp.Invoices...)

		return resp.LastIndexOffset, uint64(len(resp.Invoices)), nil
	}

	// Make paginated calls to the invoices API, starting at offset 0 and
	// querying our max number of invoices each time.
	if err := paginater.QueryPaginated(
		ctx, query, 0, uint64(maxInvoiceQueries),
	); err != nil {
		return nil, err
	}

	return invoices, nil
}

// wrapListPayments makes a set of paginated calls to lnd to get our full set
// of payments.
func (c *Config) wrapListPayments(ctx context.Context) ([]*lnrpc.Payment, error) {
	var payments []*lnrpc.Payment

	query := func(offset, maxEvents uint64) (uint64, uint64, error) {
		resp, err := c.LightningClient.ListPayments(
			ctx, &lnrpc.ListPaymentsRequest{
				IndexOffset: offset,
				MaxPayments: maxEvents,
			},
		)
		if err != nil {
			return 0, 0, err
		}

		payments = append(payments, resp.Payments...)

		return resp.LastIndexOffset, uint64(len(resp.Payments)), nil
	}

	// Make paginated calls to the payments API, starting at offset 0 and
	// querying our max number of payments each time.
	if err := paginater.QueryPaginated(
		ctx, query, 0, uint64(maxPaymentQueries),
	); err != nil {
		return nil, err
	}

	return payments, nil
}

// wrapListForwards makes paginated calls to our forwarding events api.
func (c *Config) wrapListForwards(ctx context.Context, startTime,
	endTime time.Time) ([]*lnrpc.ForwardingEvent, error) {

	var forwards []*lnrpc.ForwardingEvent

	query := func(offset, maxEvents uint64) (uint64, uint64, error) {
		resp, err := c.LightningClient.ForwardingHistory(
			ctx, &lnrpc.ForwardingHistoryRequest{
				StartTime:    uint64(startTime.Unix()),
				EndTime:      uint64(endTime.Unix()),
				IndexOffset:  uint32(offset),
				NumMaxEvents: uint32(maxEvents),
			},
		)
		if err != nil {
			return 0, 0, err
		}

		forwards = append(forwards, resp.ForwardingEvents...)

		return uint64(resp.LastOffsetIndex),
			uint64(len(resp.ForwardingEvents)), nil
	}

	// Make paginated calls to the forwards API, starting at offset 0 and
	// querying our max number of payments each time.
	if err := paginater.QueryPaginated(
		ctx, query, 0, uint64(maxForwardQueries),
	); err != nil {
		return nil, err
	}

	return forwards, nil
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
func (s *RPCServer) StartAsSubserver(lndClient lnrpc.LightningClient) error {
	if atomic.AddInt32(&s.started, 1) != 1 {
		return errServerAlreadyStarted
	}

	s.cfg.LightningClient = lndClient
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
func (s *RPCServer) FiatEstimate(ctx context.Context,
	req *FiatEstimateRequest) (*FiatEstimateResponse, error) {

	granularity, reqs, err := parseFiatRequest(req)
	if err != nil {
		return nil, err
	}

	prices, err := fiat.GetPrices(ctx, reqs, granularity)
	if err != nil {
		return nil, err
	}

	return fiatEstimateResponse(prices), nil
}

// NodeReport returns an on chain report for the period requested.
func (s *RPCServer) NodeReport(ctx context.Context,
	req *NodeReportRequest) (*NodeReportResponse, error) {

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

// allowCORS wraps the given http.Handler with a function that adds the
// Access-Control-Allow-Origin header to the response.
func allowCORS(handler http.Handler, origin string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", origin)
		handler.ServeHTTP(w, r)
	})
}
