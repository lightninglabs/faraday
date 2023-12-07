// Package faraday contains the main function for faraday.
package faraday

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/jessevdk/go-flags"
	"github.com/lightninglabs/faraday/chain"
	"github.com/lightninglabs/faraday/frdrpcserver"
	"github.com/lightninglabs/lndclient"
	"github.com/lightningnetwork/lnd/build"
	"github.com/lightningnetwork/lnd/lnrpc/verrpc"
	"github.com/lightningnetwork/lnd/signal"
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
	SetupLoggers(logWriter, shutdownInterceptor)
	err = build.ParseAndSetDebugLevels(config.DebugLevel, logWriter)
	if err != nil {
		return err
	}

	if err := ValidateConfig(&config); err != nil {
		return fmt.Errorf("error validating config: %v", err)
	}

	serverTLSCfg, restClientCreds, err := getTLSConfig(&config)
	if err != nil {
		return fmt.Errorf("error loading TLS config: %v", err)
	}

	// Connect to the full suite of lightning services offered by lnd's
	// subservers.
	client, err := lndclient.NewLndServices(&lndclient.LndServicesConfig{
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
	defer client.Close()

	// Instantiate the faraday gRPC server.
	cfg := &frdrpcserver.Config{
		Lnd:              client.LndServices,
		RPCListen:        config.RPCListen,
		RESTListen:       config.RESTListen,
		CORSOrigin:       config.CORSOrigin,
		TLSServerConfig:  serverTLSCfg,
		RestClientConfig: restClientCreds,
		FaradayDir:       config.FaradayDir,
		MacaroonPath:     config.MacaroonPath,
	}

	// If the client chose to connect to a bitcoin client, get one now.
	if config.ChainConn {
		cfg.BitcoinClient, err = chain.NewBitcoinClient(config.Bitcoin)
		if err != nil {
			return err
		}
	}

	server := frdrpcserver.NewRPCServer(cfg)

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
