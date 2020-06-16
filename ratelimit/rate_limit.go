package ratelimit

import (
	"context"
	"fmt"
	"time"

	"github.com/lightningnetwork/lnd/lnrpc/routerrpc"
)

// Run runs the rate limiting interceptor.
func Run(ctx context.Context, client routerrpc.RouterClient) error {
	interceptor, err := client.HtlcInterceptor(ctx)
	if err != nil {
		return err
	}

	last := time.Now()
	for {
		packet, err := interceptor.Recv()
		if err != nil {
			return err
		}

		var resolution routerrpc.ResolveHoldForwardAction

		// If we are receiving too many forwards, drop this packet.
		now := time.Now()
		if now.Sub(last) < 5*time.Second {
			resolution = routerrpc.ResolveHoldForwardAction_FAIL
		} else {
			resolution = routerrpc.ResolveHoldForwardAction_RESUME
			last = now

		}

		fmt.Printf("%v HTLC %v:%v -> %v\n", time.Now(),
			packet.IncomingCircuitKey.ChanId,
			packet.IncomingCircuitKey.HtlcId,
			resolution,
		)

		interceptor.Send(
			&routerrpc.ForwardHtlcInterceptResponse{
				Action:             resolution,
				IncomingCircuitKey: packet.IncomingCircuitKey,
			},
		)
	}

	return nil
}
