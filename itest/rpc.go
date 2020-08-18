package itest

import (
	"fmt"

	"github.com/btcsuite/btcd/rpcclient"
	"github.com/lightninglabs/faraday/frdrpc"
	"google.golang.org/grpc"
)

// getBitcoindClient returns an rpc client connection to the running bitcoind
// daemon.
func getBitcoindClient() (*rpcclient.Client, error) {
	connCfg := &rpcclient.ConnConfig{
		Host:         "localhost:18443",
		User:         "devuser",
		Pass:         "devpass",
		HTTPPostMode: true,
		DisableTLS:   true,
	}

	return rpcclient.New(connCfg, nil)
}

// getFaradayClient returns an rpc client connection to the running faraday
// instance.
func getFaradayClient(address string) (frdrpc.FaradayServerClient, error) {
	opts := []grpc.DialOption{
		grpc.WithInsecure(),
	}

	conn, err := grpc.Dial(address, opts...)
	if err != nil {
		return nil, fmt.Errorf("unable to connect to RPC server: %v",
			err)
	}

	return frdrpc.NewFaradayServerClient(conn), nil
}
