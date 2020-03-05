package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"

	"github.com/lightninglabs/governator/gvnrpc"
	"github.com/lightninglabs/protobuf-hex-display/jsonpb"
	"github.com/lightninglabs/protobuf-hex-display/proto"
	"github.com/lightningnetwork/lnd/lncfg"
	"github.com/urfave/cli"
	"google.golang.org/grpc"
)

var (
	// maxMsgRecvSize is the largest message our client will receive. We
	// set this to 200MiB atm.
	maxMsgRecvSize = grpc.MaxCallRecvMsgSize(1 * 1024 * 1024 * 200)
)

// fatal logs and error and exits.
func fatal(err error) {
	_, _ = fmt.Fprintf(os.Stderr, "[trmcli] %v\n", err)
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

// getClient returns a governator client.
func getClient(ctx *cli.Context) (gvnrpc.GovernatorServerClient, func()) {
	conn := getClientConn(ctx)

	cleanUp := func() {
		if err := conn.Close(); err != nil {
			fatal(err)
		}
	}

	return gvnrpc.NewGovernatorServerClient(conn), cleanUp
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
		// TODO(carla): add tls and remove this option.
		grpc.WithInsecure(),
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
