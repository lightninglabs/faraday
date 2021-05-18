// Package faraday contains the main function for faraday.
package faraday

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/jessevdk/go-flags"
	"github.com/lightninglabs/lndclient"
	"github.com/lightningnetwork/lnd/build"
	"github.com/lightningnetwork/lnd/signal"

	"github.com/lightninglabs/faraday/chain"
	"github.com/lightninglabs/faraday/frdrpc"
)

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
	logWriter := build.NewRotatingLogWriter()
	SetupLoggers(logWriter, shutdownInterceptor)

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
		LndAddress:  config.Lnd.RPCServer,
		Network:     lndclient.Network(config.Network),
		MacaroonDir: config.Lnd.MacaroonDir,
		TLSPath:     config.Lnd.TLSCertPath,
		// Use the default lnd version check which checks for version
		// v0.11.0 and requires all build tags.
		CheckVersion: nil,
	})
	if err != nil {
		return fmt.Errorf("cannot connect to lightning services: %v",
			err)
	}
	defer client.Close()

	// Instantiate the faraday gRPC server.
	cfg := &frdrpc.Config{
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

	server := frdrpc.NewRPCServer(cfg)

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
