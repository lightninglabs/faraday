package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net"
	"os"
	"strconv"

	"github.com/btcsuite/btcd/chaincfg/chainhash"
	"github.com/btcsuite/btcd/wire"
	"github.com/lightninglabs/faraday/frdrpc"
	"github.com/lightninglabs/protobuf-hex-display/jsonpb"
	"github.com/lightninglabs/protobuf-hex-display/proto"
	"github.com/lightningnetwork/lnd/lncfg"
	"github.com/lightningnetwork/lnd/macaroons"
	"github.com/urfave/cli"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"gopkg.in/macaroon.v2"
)

var (
	// maxMsgRecvSize is the largest message our client will receive. We
	// set this to 200MiB atm.
	maxMsgRecvSize = grpc.MaxCallRecvMsgSize(1 * 1024 * 1024 * 200)

	// defaultMacaroonTimeout is the default macaroon timeout in seconds
	// that we set when sending it over the line.
	defaultMacaroonTimeout int64 = 60
)

// fatal logs and error and exits.
func fatal(err error) {
	_, _ = fmt.Fprintf(os.Stderr, "[frcli] %v\n", err)
	os.Exit(1)
}

// printRespJSON prints a proto message as json.
func printRespJSON(resp proto.Message) {
	jsonMarshaler := &jsonpb.Marshaler{
		OrigName:     true,
		EmitDefaults: true,
		Indent:       "    ",
	}

	jsonStr, err := jsonMarshaler.MarshalToString(resp)
	if err != nil {
		fmt.Println("unable to decode response: ", err)
		return
	}

	fmt.Println(jsonStr)
}

func printJSON(resp interface{}) {
	b, err := json.Marshal(resp)
	if err != nil {
		fatal(err)
	}

	var out bytes.Buffer
	_ = json.Indent(&out, b, "", "\t")
	out.WriteString("\n")
	_, _ = out.WriteTo(os.Stdout)
}

// getClient returns a faraday client.
func getClient(ctx *cli.Context) (frdrpc.FaradayServerClient, func()) {
	conn := getClientConn(ctx)

	cleanUp := func() {
		if err := conn.Close(); err != nil {
			fatal(err)
		}
	}

	return frdrpc.NewFaradayServerClient(conn), cleanUp
}

// getClientConn gets a client connection to the address provided by the
// rpcserver flag.
func getClientConn(ctx *cli.Context) *grpc.ClientConn {
	// We need to use a custom dialer so we can also connect to unix sockets
	// and not just TCP addresses.
	genericDialer := clientAddressDialer(defaultRPCPort)

	opts := []grpc.DialOption{
		grpc.WithContextDialer(genericDialer),
		grpc.WithDefaultCallOptions(maxMsgRecvSize),
	}

	switch {
	// If a TLS certificate file is specified, we need to load it and build
	// transport credentials with it.
	case ctx.GlobalIsSet(tlsCertFlag.Name):
		creds, err := credentials.NewClientTLSFromFile(
			ctx.GlobalString(tlsCertFlag.Name), "",
		)
		if err != nil {
			fatal(err)
		}

		// Macaroons are only allowed to be transmitted over a TLS
		// enabled connection.
		if ctx.GlobalIsSet(macaroonPathFlag.Name) {
			opts = append(opts, readMacaroon(
				ctx.GlobalString(macaroonPathFlag.Name),
			))
		}

		opts = append(opts, grpc.WithTransportCredentials(creds))

	// By default, if no certificate is supplied, we assume the RPC server
	// runs without TLS.
	default:
		opts = append(opts, grpc.WithInsecure())
	}

	conn, err := grpc.Dial(ctx.GlobalString("rpcserver"), opts...)
	if err != nil {
		fatal(fmt.Errorf("unable to connect to RPC server: %v", err))
	}

	return conn
}

// ClientAddressDialer parsed client address and returns a dialer.
func clientAddressDialer(defaultPort string) func(context.Context,
	string) (net.Conn, error) {

	return func(ctx context.Context, addr string) (net.Conn, error) {
		parsedAddr, err := lncfg.ParseAddressString(
			addr, defaultPort, net.ResolveTCPAddr,
		)
		if err != nil {
			return nil, err
		}

		d := net.Dialer{}
		return d.DialContext(
			ctx, parsedAddr.Network(), parsedAddr.String(),
		)
	}
}

// readMacaroon tries to read the macaroon file at the specified path and create
// gRPC dial options from it.
//
// TODO(guggero): Provide this function in lnd's macaroon package and use it
// from there.
func readMacaroon(macPath string) grpc.DialOption {
	// Load the specified macaroon file.
	macBytes, err := ioutil.ReadFile(macPath)
	if err != nil {
		fatal(fmt.Errorf("unable to read macaroon path : %v", err))
	}

	mac := &macaroon.Macaroon{}
	if err = mac.UnmarshalBinary(macBytes); err != nil {
		fatal(fmt.Errorf("unable to decode macaroon: %v", err))
	}

	macConstraints := []macaroons.Constraint{
		// We add a time-based constraint to prevent replay of the
		// macaroon. It's good for 60 seconds by default to make up for
		// any discrepancy between client and server clocks, but leaking
		// the macaroon before it becomes invalid makes it possible for
		// an attacker to reuse the macaroon. In addition, the validity
		// time of the macaroon is extended by the time the server clock
		// is behind the client clock, or shortened by the time the
		// server clock is ahead of the client clock (or invalid
		// altogether if, in the latter case, this time is more than 60
		// seconds).
		macaroons.TimeoutConstraint(defaultMacaroonTimeout),
	}

	// Apply constraints to the macaroon.
	constrainedMac, err := macaroons.AddConstraints(mac, macConstraints...)
	if err != nil {
		fatal(err)
	}

	// Now we append the macaroon credentials to the dial options.
	cred := macaroons.NewMacaroonCredential(constrainedMac)
	return grpc.WithPerRPCCredentials(cred)
}

// parseChannelPoint parses a funding txid and output index from the command
// line. Both named options as well as unnamed parameters are supported.
func parseChannelPoint(ctx *cli.Context) (*wire.OutPoint, error) {
	args := ctx.Args()

	var txid string
	switch {
	case ctx.IsSet("funding_txid"):
		txid = ctx.String("funding_txid")

	case args.Present():
		txid = args.First()
		args = args.Tail()
	default:
		return nil, fmt.Errorf("funding txid argument missing")
	}

	hash, err := chainhash.NewHashFromStr(txid)
	if err != nil {
		return nil, err
	}

	channelPoint := &wire.OutPoint{
		Hash: *hash,
	}

	switch {
	case ctx.IsSet("output_index"):
		channelPoint.Index = uint32(ctx.Int("output_index"))

	case args.Present():
		index, err := strconv.ParseUint(args.First(), 10, 32)
		if err != nil {
			return nil, fmt.Errorf("unable to decode output "+
				"index: %v", err)
		}
		channelPoint.Index = uint32(index)
	}

	return channelPoint, nil
}
