// Package frdrpcserver contains the server logic for faraday's grpc server
// which serves requests for close recommendations.
//
// The Faraday server interface is implemented by the RPCServer struct.
// To keep this file readable, each function implemented by the interface
// has a file named after the function call which contains rpc parsing
// code for the request and response. If the call requires extensive
// additional logic, and unexported function with the same name should
// be created in this file as well.
package frdrpcserver

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"

	proxy "github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"github.com/lightninglabs/faraday/accounting"
	"github.com/lightninglabs/faraday/chain"
	"github.com/lightninglabs/faraday/fiat"
	"github.com/lightninglabs/faraday/frdrpc"
	"github.com/lightninglabs/faraday/frdrpcserver/perms"
	"github.com/lightninglabs/faraday/recommend"
	"github.com/lightninglabs/faraday/resolutions"
	"github.com/lightninglabs/faraday/revenue"
	"github.com/lightninglabs/lndclient"
	"github.com/lightningnetwork/lnd/kvdb"
	"github.com/lightningnetwork/lnd/lncfg"
	"github.com/lightningnetwork/lnd/macaroons"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/protobuf/encoding/protojson"
	"gopkg.in/macaroon-bakery.v2/bakery"
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
			MarshalOptions: protojson.MarshalOptions{
				UseProtoNames:   true,
				EmitUnpopulated: true,
			},
		},
	)

	// maxMsgRecvSize is the largest message our REST proxy will receive. We
	// set this to 400MiB atm.
	maxMsgRecvSize = grpc.MaxCallRecvMsgSize(400 * 1024 * 1024)

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

	// Required by the grpc-gateway/v2 library for forward compatibility.
	// Must be after the atomically used variables to not break struct
	// alignment.
	frdrpc.UnimplementedFaradayServerServer

	// cfg contains closures and settings required for operation.
	cfg *Config

	// grpcServer is the main gRPC RPCServer that this RPC server will
	// register itself with and accept client requests from.
	grpcServer *grpc.Server

	// rpcListener is the listener to use when starting the gRPC server.
	rpcListener net.Listener

	// restServer is the REST proxy server.
	restServer *http.Server

	macaroonService *lndclient.MacaroonService
	macaroonDB      kvdb.Backend

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

	// TLSServerConfig is the configuration to serve a secure connection
	// over TLS.
	TLSServerConfig *tls.Config

	// RestClientConfig is the client configuration to connect to a TLS
	// server started with the TLS config above. This is used for the REST
	// proxy that connects internally to the gRPC server and therefore is a
	// TLS client.
	RestClientConfig *credentials.TransportCredentials

	// FaradayDir is the main directory faraday uses. The macaroon database
	// will be created there.
	FaradayDir string

	// MacaroonPath is the full path to the default faraday macaroon file
	// that is created automatically. This path normally is within
	// FaradayDir unless otherwise specified by the user.
	MacaroonPath string
}

// NewRPCServer returns a server which will listen for rpc requests on the
// rpc listen address provided. Note that the server returned is not running,
// and should be started using Start().
func NewRPCServer(cfg *Config) *RPCServer {
	return &RPCServer{
		cfg: cfg,
	}
}

// Start starts the listener and server.
func (s *RPCServer) Start() error {
	if atomic.AddInt32(&s.started, 1) != 1 {
		return errServerAlreadyStarted
	}

	// Depending on how far we got in initializing the server, we might need
	// to clean up certain services that were already started. Keep track of
	// them with this map of service name to shutdown function.
	shutdownFuncs := make(map[string]func() error)
	defer func() {
		for serviceName, shutdownFn := range shutdownFuncs {
			if err := shutdownFn(); err != nil {
				log.Errorf("Error shutting down %s service: %v",
					serviceName, err)
			}
		}
	}()

	// Set up the macaroon service.
	rks, db, err := lndclient.NewBoltMacaroonStore(
		s.cfg.FaradayDir, lncfg.MacaroonDBName, macDatabaseOpenTimeout,
	)
	if err != nil {
		return err
	}
	shutdownFuncs["macaroondb"] = db.Close

	s.macaroonDB = db
	s.macaroonService, err = lndclient.NewMacaroonService(
		&lndclient.MacaroonServiceConfig{
			RootKeyStore:     rks,
			MacaroonLocation: faradayMacaroonLocation,
			MacaroonPath:     s.cfg.MacaroonPath,
			Checkers: []macaroons.Checker{
				macaroons.IPLockChecker,
			},
			RequiredPerms: perms.RequiredPermissions,
			DBPassword:    macDbDefaultPw,
			LndClient:     &s.cfg.Lnd,
			EphemeralKey:  lndclient.SharedKeyNUMS,
			KeyLocator:    lndclient.SharedKeyLocator,
		},
	)
	if err != nil {
		return fmt.Errorf("error creating macroon service: %v", err)
	}

	// Start the macaroon service and let it create its default macaroon in
	// case it doesn't exist yet.
	if err := s.macaroonService.Start(); err != nil {
		return fmt.Errorf("error starting macaroon service: %v", err)
	}
	shutdownFuncs["macaroon"] = s.macaroonService.Stop

	// First we add the security interceptor to our gRPC server options that
	// checks the macaroons for validity.
	unaryInterceptor, streamInterceptor, err := s.macaroonService.Interceptors()
	if err != nil {
		return fmt.Errorf("error with macaroon interceptor: %v", err)
	}

	// Add our TLS configuration and then create our server instance. It's
	// important that we let gRPC create the TLS listener and we don't just
	// use tls.NewListener(). Otherwise we run into the ALPN error with non-
	// golang clients.
	tlsCredentials := credentials.NewTLS(s.cfg.TLSServerConfig)
	s.grpcServer = grpc.NewServer(
		grpc.UnaryInterceptor(unaryInterceptor),
		grpc.StreamInterceptor(streamInterceptor),
		grpc.Creds(tlsCredentials),
	)

	// Start the gRPC RPCServer listening for HTTP/2 connections.
	log.Info("Starting gRPC listener")
	s.rpcListener, err = net.Listen("tcp", s.cfg.RPCListen)
	if err != nil {
		return fmt.Errorf("RPC RPCServer unable to listen on %v",
			s.cfg.RPCListen)
	}
	shutdownFuncs["gRPC listener"] = s.rpcListener.Close
	log.Infof("gRPC server listening on %s", s.rpcListener.Addr())

	frdrpc.RegisterFaradayServerServer(s.grpcServer, s)

	// We'll also create and start an accompanying proxy to serve clients
	// through REST. An empty address indicates REST is disabled.
	if s.cfg.RESTListen != "" {
		log.Infof("Starting REST proxy listener ")
		restListener, err := net.Listen("tcp", s.cfg.RESTListen)
		if err != nil {
			return fmt.Errorf("REST server unable to listen on "+
				"%v: %v", s.cfg.RESTListen, err)
		}
		restListener = tls.NewListener(
			restListener, s.cfg.TLSServerConfig,
		)
		shutdownFuncs["REST listener"] = restListener.Close
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
			grpc.WithTransportCredentials(*s.cfg.RestClientConfig),
			grpc.WithDefaultCallOptions(maxMsgRecvSize),
		}

		// With TLS enabled by default, we cannot call 0.0.0.0
		// internally from the REST proxy as that IP address isn't in
		// the cert. We need to rewrite it to the loopback address.
		restProxyDest := s.cfg.RPCListen
		switch {
		case strings.Contains(restProxyDest, "0.0.0.0"):
			restProxyDest = strings.Replace(
				restProxyDest, "0.0.0.0", "127.0.0.1", 1,
			)

		case strings.Contains(restProxyDest, "[::]"):
			restProxyDest = strings.Replace(
				restProxyDest, "[::]", "[::1]", 1,
			)
		}
		err = frdrpc.RegisterFaradayServerHandlerFromEndpoint(
			restCtx, mux, restProxyDest, proxyOpts,
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

	// If we got here successfully, there's no need to shutdown anything
	// anymore.
	shutdownFuncs = nil

	return nil
}

// StartAsSubserver is an alternative to Start where the RPC server does not
// create its own gRPC server but registers to an existing one. The same goes
// for REST (if enabled), instead of creating an own mux and HTTP server, we
// register to an existing one.
func (s *RPCServer) StartAsSubserver(lndClient lndclient.LndServices,
	withMacaroonService bool) error {

	if atomic.AddInt32(&s.started, 1) != 1 {
		return errServerAlreadyStarted
	}

	if withMacaroonService {
		// Set up the macaroon service.
		rks, db, err := lndclient.NewBoltMacaroonStore(
			s.cfg.FaradayDir, lncfg.MacaroonDBName,
			macDatabaseOpenTimeout,
		)
		if err != nil {
			return err
		}

		s.macaroonDB = db
		s.macaroonService, err = lndclient.NewMacaroonService(
			&lndclient.MacaroonServiceConfig{
				RootKeyStore:     rks,
				MacaroonLocation: faradayMacaroonLocation,
				MacaroonPath:     s.cfg.MacaroonPath,
				Checkers: []macaroons.Checker{
					macaroons.IPLockChecker,
				},
				RequiredPerms: perms.RequiredPermissions,
				DBPassword:    macDbDefaultPw,
				LndClient:     &lndClient,
				EphemeralKey:  lndclient.SharedKeyNUMS,
				KeyLocator:    lndclient.SharedKeyLocator,
			},
		)
		if err != nil {
			return fmt.Errorf("error creating macroon service: %v",
				err)
		}

		// Start the macaroon service and let it create its default
		// macaroon in case it doesn't exist yet.
		if err := s.macaroonService.Start(); err != nil {
			return fmt.Errorf("error starting macaroon service: %v",
				err)
		}
	}

	s.cfg.Lnd = lndClient
	return nil
}

// ValidateMacaroon extracts the macaroon from the context's gRPC metadata,
// checks its signature, makes sure all specified permissions for the called
// method are contained within and finally ensures all caveat conditions are
// met. A non-nil error is returned if any of the checks fail. This method is
// needed to enable faraday running as an external subserver in the same process
// as lnd but still validate its own macaroons.
func (s *RPCServer) ValidateMacaroon(ctx context.Context,
	requiredPermissions []bakery.Op, fullMethod string) error {

	if s.macaroonService == nil {
		return fmt.Errorf("macaroon service not yet initialised")
	}

	// Delegate the call to faraday's own macaroon validator service.
	return s.macaroonService.ValidateMacaroon(
		ctx, requiredPermissions, fullMethod,
	)
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

	if s.macaroonService != nil {
		if err := s.macaroonService.Stop(); err != nil {
			log.Errorf("Error stopping macaroon service: %v", err)
		}
	}
	if s.macaroonDB != nil {
		if err := s.macaroonDB.Close(); err != nil {
			log.Errorf("Error closing macaroon DB: %v", err)
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
	req *frdrpc.OutlierRecommendationsRequest) (*frdrpc.CloseRecommendationsResponse,
	error) {

	if req.RecRequest == nil {
		return nil, errors.New("recommendation request field required")
	}

	log.Debugf("[OutlierRecommendations]: metric: %v, multiplier: %v",
		req.RecRequest.Metric, req.OutlierMultiplier)

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
	req *frdrpc.ThresholdRecommendationsRequest) (*frdrpc.CloseRecommendationsResponse,
	error) {

	if req.RecRequest == nil {
		return nil, errors.New("recommendation request field required")
	}

	log.Debugf("[ThresholdRecommendations]: metric: %v, threshold: %v",
		req.RecRequest.Metric, req.ThresholdValue)

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
	req *frdrpc.RevenueReportRequest) (*frdrpc.RevenueReportResponse, error) {

	log.Debugf("[RevenueReport]: range: %v-%v, channels: %v", req.StartTime,
		req.EndTime, req.ChanPoints)

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
	_ *frdrpc.ChannelInsightsRequest) (*frdrpc.ChannelInsightsResponse, error) {

	log.Debugf("[ChannelInsights]")

	insights, err := channelInsights(ctx, s.cfg)
	if err != nil {
		return nil, err
	}

	return rpcChannelInsightsResponse(insights), nil
}

// ExchangeRate provides a fiat estimate for a set of timestamped bitcoin
// prices.
func (s *RPCServer) ExchangeRate(ctx context.Context,
	req *frdrpc.ExchangeRateRequest) (*frdrpc.ExchangeRateResponse, error) {

	log.Debugf("[FiatEstimate]: %v requests", len(req.Timestamps))

	timestamps, priceCfg, err := parseExchangeRateRequest(req)
	if err != nil {
		return nil, err
	}

	prices, err := fiat.GetPrices(ctx, timestamps, priceCfg)
	if err != nil {
		return nil, err
	}

	return exchangeRateResponse(prices), nil
}

// NodeAudit returns an on chain report for the period requested.
func (s *RPCServer) NodeAudit(ctx context.Context,
	req *frdrpc.NodeAuditRequest) (*frdrpc.NodeAuditResponse, error) {

	log.Debugf("[NodeAudit]: range: %v-%v, fiat: %v", req.StartTime,
		req.EndTime, req.DisableFiat)

	onChain, offChain, err := parseNodeAuditRequest(ctx, s.cfg, req)
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
	req *frdrpc.CloseReportRequest) (*frdrpc.CloseReportResponse, error) {

	log.Debugf("[CloseReport]: %v", req.ChannelPoint)

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
