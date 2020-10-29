// Package faraday contains the main function for faraday.
package faraday

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/lightningnetwork/lnd/lnrpc/verrpc"

	"github.com/jessevdk/go-flags"
	"github.com/lightninglabs/lndclient"
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

	if err := ValidateConfig(&config); err != nil {
		return fmt.Errorf("error validating config: %v", err)
	}

	serverTLSCfg, restClientCreds, err := getTLSConfig(&config)
	if err != nil {
		return fmt.Errorf("error loading TLS config: %v", err)
	}

	// By default, we require all subservers to be built because lndclient
	// needs to be able to find each subserver's macaroon when it starts
	// up. However, if we are using a custom macaroon, we can reduce the
	// subservers required to only the subserver that we currently use,
	// because lndclient will look for the same macaroon for each subserver.
	buildTags := []string{
		"signrpc", "walletrpc", "chainrpc", "invoicesrpc",
	}
	if config.Lnd.CustomMacaroon != "" {
		buildTags = []string{"walletrpc"}
	}

	// Connect to the full suite of lightning services offered by lnd's
	// subservers.
	client, err := lndclient.NewLndServices(&lndclient.LndServicesConfig{
		LndAddress:         config.Lnd.RPCServer,
		Network:            lndclient.Network(config.Network),
		MacaroonDir:        config.Lnd.MacaroonDir,
		CustomMacaroonPath: config.Lnd.CustomMacaroon,
		TLSPath:            config.Lnd.TLSCertPath,
		CheckVersion: &verrpc.Version{
			AppMajor:  0,
			AppMinor:  11,
			AppPatch:  1,
			BuildTags: buildTags,
		},
	})
	if err != nil {
		return fmt.Errorf("cannot connect to lightning services: %v",
			err)
	}
	defer client.Close()

	// If we want to get a macaroon recipe, we print out the list of
	// macaroon permissions that we currently require to run faraday.
	if config.MacaroonRecipe {
		subservers := []string{
			"lnrpc", "verrpc", "walletrpc",
		}

		perms, err := lndclient.MacaroonRecipe(
			client.Client, subservers,
		)
		if err != nil {
			fmt.Printf("Could not get macaroon recipe: %v", err)
			os.Exit(1)
		}

		permList := make([]string, len(perms))
		for i, perm := range perms {
			permList[i] = fmt.Sprintf("%v:%v", perm.Entity,
				perm.Action)
		}

		fmt.Println("To generate a custom macaroon for Faraday:")
		fmt.Printf("lncli bakemacaroon --save_to={path} %v\n", strings.Join(permList, " "))
		fmt.Println("To use this macaroon, restart Faraday using --lnd.custommacaroon={path to macaroon}")

		os.Exit(0)
	}

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

	// Catch intercept signals, then start the server.
	if err := signal.Intercept(); err != nil {
		return err
	}
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
