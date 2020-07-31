// Package faraday contains the main function for faraday.
package faraday

import (
	"fmt"

	"github.com/lightninglabs/faraday/chain"
	"github.com/lightninglabs/faraday/frdrpc"
	"github.com/lightninglabs/lndclient"
	"github.com/lightningnetwork/lnd/signal"
)

// Main is the real entry point for faraday. It is required to ensure that
// defers are properly executed when os.Exit() is called.
func Main() error {
	config, err := LoadConfig()
	if err != nil {
		return fmt.Errorf("error loading config: %v", err)
	}

	// Connect to the full suite of lightning services offered by lnd's
	// subservers.
	client, err := lndclient.NewLndServices(&lndclient.LndServicesConfig{
		LndAddress:  config.RPCServer,
		Network:     lndclient.Network(config.network),
		MacaroonDir: config.MacaroonDir,
		TLSPath:     config.TLSCertPath,
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
		Lnd:        client.LndServices,
		RPCListen:  config.RPCListen,
		RESTListen: config.RESTListen,
		CORSOrigin: config.CORSOrigin,
	}

	// If the client chose to connect to a bitcoin client, get one now.
	if config.ChainConn {
		cfg.BitcoinClient, err = chain.NewBitcoinClient(config.Bitcoin)
		if err != nil {
			return err
		}
	}

	server := frdrpc.NewRPCServer(cfg)

	// Catch intercept signals, then start the server.
	signal.Intercept()
	if err := server.Start(); err != nil {
		return err
	}

	// Run until the user terminates.
	<-signal.ShutdownChannel()
	log.Infof("Received shutdown signal.")

	if err := server.Stop(); err != nil {
		return err
	}

	return nil
}
