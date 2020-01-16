// Package terminator contains the main function for the terminator.
package terminator

import (
	"fmt"

	"github.com/lightninglabs/loop/lndclient"
	"github.com/lightninglabs/terminator/trmrpc"
	"github.com/lightningnetwork/lnd/signal"
)

// Main is the real entry point for terminator. It is required to ensure that
// defers are properly executed when os.Exit() is called.
func Main() error {
	config, err := loadConfig()
	if err != nil {
		return fmt.Errorf("error loading config: %v", err)
	}

	// NewBasicClient get a lightning rpc client with
	client, err := lndclient.NewBasicClient(
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

	// Instantiate the terminator gRPC server.
	server := trmrpc.NewRPCServer(
		&trmrpc.Config{
			LightningClient: client,
			RPCListen:       config.RPCListen,
		},
	)

	if err := server.Start(); err != nil {
		return err
	}

	// Run until the user terminates.
	<-signal.ShutdownChannel()
	log.Infof("Received shutdown signal.")

	if err := server.Stop(); err != nil {
		return err
	}

	log.Info("That's all for now. I will be back.")

	return nil
}
