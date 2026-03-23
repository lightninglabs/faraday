// Package faraday contains the main function for faraday.
package faraday

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	proxy "github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"github.com/jessevdk/go-flags"
	"github.com/lightninglabs/faraday/chain"
	"github.com/lightninglabs/faraday/frdrpc"
	"github.com/lightninglabs/faraday/frdrpcserver"
	"github.com/lightninglabs/faraday/frdrpcserver/perms"
	"github.com/lightninglabs/lndclient"
	"github.com/lightningnetwork/lnd/build"
	"github.com/lightningnetwork/lnd/kvdb"
	"github.com/lightningnetwork/lnd/lncfg"
	"github.com/lightningnetwork/lnd/lnrpc/verrpc"
	"github.com/lightningnetwork/lnd/macaroons"
	"github.com/lightningnetwork/lnd/signal"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/protobuf/encoding/protojson"
	"gopkg.in/macaroon-bakery.v2/bakery"
)

var (
	// customMarshalerOption is the configuration we use for the JSON
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
	// set this to 600MiB atm.
	maxMsgRecvSize = grpc.MaxCallRecvMsgSize(600 * 1024 * 1024)

	// errServerAlreadyStarted is the error that is returned if the server
	// is requested to start while it's already been started.
	errServerAlreadyStarted = fmt.Errorf("server can only be started once")
)

// MinLndVersion is the minimum lnd version required. Note that apis that are
// only available in more recent versions are available at compile time, so this
// version should be bumped if additional functionality is included that depends
// on newer apis.
var MinLndVersion = &verrpc.Version{
	AppMajor: 0,
	AppMinor: 15,
	AppPatch: 4,
}

// Faraday is a struct that houses the faraday daemon and its dependencies.
type Faraday struct {
	*frdrpcserver.RPCServer

	// cfg is the faraday config.
	cfg *Config

	// To be used atomically.
	started int32

	// To be used atomically.
	stopped int32

	lnd *lndclient.GrpcLndServices

	// bitcoinClient is set if the client opted to connect to a bitcoin
	// backend, if not, it will be nil.
	bitcoinClient chain.BitcoinClient

	macaroonService *lndclient.MacaroonService
	macaroonDB      kvdb.Backend

	// grpcServer is the main gRPC server that this service will register
	// itself with and accept client requests from.
	grpcServer *grpc.Server

	// rpcListener is the listener to use when starting the gRPC server.
	rpcListener net.Listener

	// restServer is the REST proxy server.
	restServer *http.Server
	restCancel func()

	wg sync.WaitGroup
}

// New creates a new Faraday instance with the given configuration.
func New(cfg *Config) *Faraday {
	return &Faraday{cfg: cfg}
}

// Start starts the listener and server.
func (f *Faraday) Start() error {
	if atomic.AddInt32(&f.started, 1) != 1 {
		return errServerAlreadyStarted
	}

	cfg := &frdrpcserver.Config{
		Lnd:           f.lnd.LndServices,
		BitcoinClient: f.bitcoinClient,
	}

	// Create the RPC server.
	f.RPCServer = frdrpcserver.NewRPCServer(cfg)

	// Prepare the RPC server.
	serverTLSCfg, restClientCreds, err := getTLSConfig(f.cfg)
	if err != nil {
		return fmt.Errorf("error loading TLS config: %v", err)
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
		f.cfg.FaradayDir, lncfg.MacaroonDBName, macDatabaseOpenTimeout,
	)
	if err != nil {
		return err
	}
	shutdownFuncs["macaroondb"] = db.Close

	f.macaroonDB = db
	f.macaroonService, err = lndclient.NewMacaroonService(
		&lndclient.MacaroonServiceConfig{
			RootKeyStore:     rks,
			MacaroonLocation: faradayMacaroonLocation,
			MacaroonPath:     f.cfg.MacaroonPath,
			Checkers: []macaroons.Checker{
				macaroons.IPLockChecker,
			},
			RequiredPerms: perms.RequiredPermissions,
			DBPassword:    macDbDefaultPw,
			LndClient:     &f.lnd.LndServices,
			EphemeralKey:  lndclient.SharedKeyNUMS,
			KeyLocator:    lndclient.SharedKeyLocator,
		},
	)
	if err != nil {
		return fmt.Errorf("error creating macaroon service: %v", err)
	}

	// Start the macaroon service and let it create its default macaroon in
	// case it doesn't exist yet.
	if err := f.macaroonService.Start(); err != nil {
		return fmt.Errorf("error starting macaroon service: %v", err)
	}
	shutdownFuncs["macaroon"] = f.macaroonService.Stop

	// First we add the security interceptor to our gRPC server options that
	// checks the macaroons for validity.
	unaryInterceptor, streamInterceptor, err :=
		f.macaroonService.Interceptors()

	if err != nil {
		return fmt.Errorf("error with macaroon interceptor: %v", err)
	}

	// Add our TLS configuration and then create our server instance. It's
	// important that we let gRPC create the TLS listener and we don't just
	// use tls.NewListener(). Otherwise we run into the ALPN error with non-
	// golang clients.
	tlsCredentials := credentials.NewTLS(serverTLSCfg)
	f.grpcServer = grpc.NewServer(
		grpc.UnaryInterceptor(unaryInterceptor),
		grpc.StreamInterceptor(streamInterceptor),
		grpc.Creds(tlsCredentials),
	)

	// Start the gRPC RPCServer listening for HTTP/2 connections.
	log.Info("Starting gRPC listener")
	f.rpcListener, err = net.Listen("tcp", f.cfg.RPCListen)
	if err != nil {
		return fmt.Errorf("gRPC server unable to listen on %v",
			f.cfg.RPCListen)
	}
	shutdownFuncs["gRPC listener"] = f.rpcListener.Close
	log.Infof("gRPC server listening on %s", f.rpcListener.Addr())

	frdrpc.RegisterFaradayServerServer(f.grpcServer, f)

	// We'll also create and start an accompanying proxy to serve clients
	// through REST. An empty address indicates REST is disabled.
	if f.cfg.RESTListen != "" {
		log.Infof("Starting REST proxy listener ")
		restListener, err := net.Listen("tcp", f.cfg.RESTListen)
		if err != nil {
			return fmt.Errorf("REST server unable to listen on "+
				"%v: %v", f.cfg.RESTListen, err)
		}
		restListener = tls.NewListener(
			restListener, serverTLSCfg,
		)
		shutdownFuncs["REST listener"] = restListener.Close
		log.Infof("REST server listening on %s", restListener.Addr())

		// We'll dial into the local gRPC server so we need to set some
		// gRPC dial options and CORS settings.
		var restCtx context.Context
		restCtx, f.restCancel = context.WithCancel(context.Background())
		mux := proxy.NewServeMux(customMarshalerOption)
		var restHandler http.Handler = mux
		if f.cfg.CORSOrigin != "" {
			restHandler = allowCORS(restHandler, f.cfg.CORSOrigin)
		}
		proxyOpts := []grpc.DialOption{
			grpc.WithTransportCredentials(*restClientCreds),
			grpc.WithDefaultCallOptions(maxMsgRecvSize),
		}

		// With TLS enabled by default, we cannot call 0.0.0.0
		// internally from the REST proxy as that IP address isn't in
		// the cert. We need to rewrite it to the loopback address.
		restProxyDest := f.cfg.RPCListen
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
		f.restServer = &http.Server{
			Handler:           restHandler,
			ReadHeaderTimeout: 3 * time.Second,
		}

		f.wg.Add(1)
		go func() {
			defer f.wg.Done()
			err := f.restServer.Serve(restListener)
			// ErrServerClosed is always returned when the proxy is
			// shut down, so don't log it.
			if err != nil && err != http.ErrServerClosed {
				log.Error(err)
			}
		}()
	} else {
		log.Infof("REST proxy disabled")
	}

	f.wg.Add(1)
	go func() {
		defer f.wg.Done()
		if err := f.grpcServer.Serve(f.rpcListener); err != nil {
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
func (f *Faraday) StartAsSubserver(lndGrpc *lndclient.GrpcLndServices,
	withMacaroonService bool) error {

	log.Infof("Starting Faraday subserver version %s", Version())

	// There should be no reason to start the daemon twice. Therefore,
	// return an error if that's tried. This is mostly to guard against
	// Start and StartAsSubserver both being called.
	if atomic.AddInt32(&f.started, 1) != 1 {
		return errServerAlreadyStarted
	}

	// When starting as a subserver, we get passed in an already established
	// connection to lnd that might be shared among other subservers.
	f.lnd = lndGrpc

	if withMacaroonService {
		// Set up the macaroon service.
		rks, db, err := lndclient.NewBoltMacaroonStore(
			f.cfg.FaradayDir, lncfg.MacaroonDBName,
			macDatabaseOpenTimeout,
		)
		if err != nil {
			return err
		}

		f.macaroonDB = db
		f.macaroonService, err = lndclient.NewMacaroonService(
			&lndclient.MacaroonServiceConfig{
				RootKeyStore:     rks,
				MacaroonLocation: faradayMacaroonLocation,
				MacaroonPath:     f.cfg.MacaroonPath,
				Checkers: []macaroons.Checker{
					macaroons.IPLockChecker,
				},
				RequiredPerms: perms.RequiredPermissions,
				DBPassword:    macDbDefaultPw,
				LndClient:     &lndGrpc.LndServices,
				EphemeralKey:  lndclient.SharedKeyNUMS,
				KeyLocator:    lndclient.SharedKeyLocator,
			},
		)
		if err != nil {
			return fmt.Errorf("error creating macaroon service: %v",
				err)
		}

		// Start the macaroon service and let it create its default
		// macaroon in case it doesn't exist yet.
		if err := f.macaroonService.Start(); err != nil {
			return fmt.Errorf("error starting macaroon service: %v",
				err)
		}
	}

	cfg := &frdrpcserver.Config{
		Lnd:           lndGrpc.LndServices,
		BitcoinClient: f.bitcoinClient,
	}

	// Create the RPC server.
	f.RPCServer = frdrpcserver.NewRPCServer(cfg)

	return nil
}

// ValidateMacaroon extracts the macaroon from the context's gRPC metadata,
// checks its signature, makes sure all specified permissions for the called
// method are contained within and finally ensures all caveat conditions are
// met. A non-nil error is returned if any of the checks fail. This method is
// needed to enable faraday running as an external subserver in the same process
// as lnd but still validate its own macaroons.
func (f *Faraday) ValidateMacaroon(ctx context.Context,
	requiredPermissions []bakery.Op, fullMethod string) error {

	if f.macaroonService == nil {
		return fmt.Errorf("macaroon service not yet initialised")
	}

	// Delegate the call to faraday's own macaroon validator service.
	return f.macaroonService.ValidateMacaroon(
		ctx, requiredPermissions, fullMethod,
	)
}

// Stop stops the grpc listener and server.
func (f *Faraday) Stop() error {
	if atomic.AddInt32(&f.stopped, 1) != 1 {
		return nil
	}

	if f.restServer != nil {
		f.restCancel()
		err := f.restServer.Close()
		if err != nil {
			log.Errorf("unable to close REST listener: %v", err)
		}
	}

	if f.macaroonService != nil {
		if err := f.macaroonService.Stop(); err != nil {
			log.Errorf("Error stopping macaroon service: %v", err)
		}
	}
	if f.macaroonDB != nil {
		if err := f.macaroonDB.Close(); err != nil {
			log.Errorf("Error closing macaroon DB: %v", err)
		}
	}

	// Stop the grpc server and wait for all go routines to terminate.
	if f.grpcServer != nil {
		f.grpcServer.Stop()
	}
	f.wg.Wait()

	f.lnd.Close()

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

// Main is the real entry point for faraday. It is required to ensure that
// defers are properly executed when os.Exit() is called.
func Main() error {
	// Start with a default config.
	config := DefaultConfig()

	// Parse command line options to obtain user specified values.
	if _, err := flags.Parse(&config); err != nil {
		return err
	}

	// Show the version and exit if the version flag was specified.
	appName := filepath.Base(os.Args[0])
	appName = strings.TrimSuffix(appName, filepath.Ext(appName))
	if config.ShowVersion {
		fmt.Println(appName, "version", Version())
		os.Exit(0)
	}

	// Hook interceptor for os signals.
	shutdownInterceptor, err := signal.Intercept()
	if err != nil {
		return err
	}

	// Setup logging before parsing the config.
	logWriter := build.NewRotatingLogWriter()
	subLogMgr := build.NewSubLoggerManager(
		build.NewDefaultLogHandlers(config.Logging, logWriter)...,
	)
	SetupLoggers(subLogMgr, shutdownInterceptor)
	err = build.ParseAndSetDebugLevels(config.DebugLevel, subLogMgr)
	if err != nil {
		return err
	}

	if err := ValidateConfig(&config); err != nil {
		return fmt.Errorf("error validating config: %v", err)
	}

	server := New(&config)

	// Connect to the full suite of lightning services offered by lnd's
	// subservers.
	server.lnd, err = lndclient.NewLndServices(&lndclient.LndServicesConfig{
		LndAddress:         config.Lnd.RPCServer,
		Network:            lndclient.Network(config.Network),
		CustomMacaroonPath: config.Lnd.MacaroonPath,
		TLSPath:            config.Lnd.TLSCertPath,
		CheckVersion:       MinLndVersion,
		RPCTimeout:         config.Lnd.RequestTimeout,
	})
	if err != nil {
		return fmt.Errorf("cannot connect to lightning services: %v",
			err)
	}

	// If the client chose to connect to a bitcoin client, get one now.
	if config.ChainConn {
		server.bitcoinClient, err = chain.NewBitcoinClient(
			config.Bitcoin,
		)
		if err != nil {
			return err
		}
	}

	// Start the server.
	if err := server.Start(); err != nil {
		return err
	}

	// Run until the user terminates.
	<-shutdownInterceptor.ShutdownChannel()
	log.Infof("Received shutdown signal.")

	if err := server.Stop(); err != nil {
		return err
	}

	return nil
}
