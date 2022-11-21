package itest

import (
	"fmt"
	"io/ioutil"

	"github.com/btcsuite/btcd/rpcclient"
	"github.com/lightninglabs/faraday/frdrpc"
	"github.com/lightningnetwork/lnd/macaroons"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"gopkg.in/macaroon.v2"
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
func getFaradayClient(address, tlsCertPath,
	macaroonPath string) (frdrpc.FaradayServerClient, error) {

	tlsCredentials, err := credentials.NewClientTLSFromFile(tlsCertPath, "")
	if err != nil {
		return nil, fmt.Errorf("unable to load TLS cert %s: %v",
			tlsCertPath, err)
	}

	macaroonOptions, err := readMacaroon(macaroonPath)
	if err != nil {
		return nil, fmt.Errorf("unable to load macaroon %s: %v",
			macaroonPath, err)
	}

	opts := []grpc.DialOption{
		grpc.WithTransportCredentials(tlsCredentials),
		macaroonOptions,
	}

	conn, err := grpc.Dial(address, opts...)
	if err != nil {
		return nil, fmt.Errorf("unable to connect to RPC server: %v",
			err)
	}

	return frdrpc.NewFaradayServerClient(conn), nil
}

// readMacaroon tries to read the macaroon file at the specified path and create
// gRPC dial options from it.
func readMacaroon(macaroonPath string) (grpc.DialOption, error) {
	// Load the specified macaroon file.
	macBytes, err := ioutil.ReadFile(macaroonPath)
	if err != nil {
		return nil, fmt.Errorf("unable to read macaroon path : %v", err)
	}

	mac := &macaroon.Macaroon{}
	if err = mac.UnmarshalBinary(macBytes); err != nil {
		return nil, fmt.Errorf("unable to decode macaroon: %v", err)
	}

	// Now we append the macaroon credentials to the dial options.
	cred, err := macaroons.NewMacaroonCredential(mac)
	if err != nil {
		return nil, fmt.Errorf("error creating macaroon credential: %v",
			err)
	}
	return grpc.WithPerRPCCredentials(cred), nil
}
