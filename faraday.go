// Package faraday contains the main function for faraday.
package faraday

import (
	"fmt"

	"github.com/lightninglabs/faraday/frdrpc"
	"github.com/lightninglabs/loop/lndclient"
	"github.com/lightningnetwork/lnd/lnrpc"
	"github.com/lightningnetwork/lnd/lnrpc/routerrpc"
	"github.com/lightningnetwork/lnd/signal"
)

// Main is the real entry point for faraday. It is required to ensure that
// defers are properly executed when os.Exit() is called.
func Main() error {
	config, err := LoadConfig()
	if err != nil {
		return fmt.Errorf("error loading config: %v", err)
	}

	// NewBasicClient get a lightning rpc client with
	conn, err := lndclient.NewBasicConn(
		config.RPCServer,
		config.TLSCertPath,
		config.MacaroonDir,
		config.network,
		lndclient.MacFilename(config.MacaroonFile),
	)
	if err != nil {
		return fmt.Errorf("cannot connect to lightning client: %v",
			err)
	}

	// Instantiate the faraday gRPC server.
	server := frdrpc.NewRPCServer(
		&frdrpc.Config{
			LightningClient: lnrpc.NewLightningClient(conn),
			RouterClient:    routerrpc.NewRouterClient(conn),
			RPCListen:       config.RPCListen,
			RESTListen:      config.RESTListen,
			CORSOrigin:      config.CORSOrigin,
		},
	)

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
