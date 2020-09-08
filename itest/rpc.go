package itest

import (
	"fmt"

	"github.com/btcsuite/btcd/rpcclient"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"

	"github.com/lightninglabs/faraday/frdrpc"
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
func getFaradayClient(address, tlsCertPath string) (frdrpc.FaradayServerClient,
	error) {

	tlsCredentials, err := credentials.NewClientTLSFromFile(tlsCertPath, "")
	if err != nil {
		return nil, fmt.Errorf("unable to load TLS cert %s: %v",
			tlsCertPath, err)
	}

	opts := []grpc.DialOption{
		grpc.WithTransportCredentials(tlsCredentials),
	}

	conn, err := grpc.Dial(address, opts...)
	if err != nil {
		return nil, fmt.Errorf("unable to connect to RPC server: %v",
			err)
	}

	return frdrpc.NewFaradayServerClient(conn), nil
}
