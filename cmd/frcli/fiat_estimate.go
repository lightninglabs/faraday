package main

import (
	"context"
	"fmt"
	"time"

	"github.com/lightninglabs/faraday/frdrpc"
	"github.com/urfave/cli"
)

var fiatEstimateCommand = cli.Command{
	Name:     "fiat",
	Category: "prices",
	Usage:    "Get fiat pricing for BTC.",
	Flags: []cli.Flag{
		cli.Uint64Flag{
			Name:  "amt_msat",
			Usage: "amount in millisatoshi",
		},
		cli.Int64Flag{
			Name: "timestamp",
			Usage: "the time at which price should be quoted, " +
				"the current price will be used if not supplied",
		},
	},
	Action: queryFiatEstimate,
}

func queryFiatEstimate(ctx *cli.Context) error {
	client, cleanup := getClient(ctx)
	defer cleanup()

	ts := ctx.Int64("timestamp")
	if ts == 0 {
		ts = time.Now().Unix()
	}

	amt := ctx.Uint64("amt_msat")
	if amt == 0 {
		return fmt.Errorf("non-zero amount required")
	}

	// Set start and end times from user specified values, defaulting
	// to zero if they are not set.
	req := &frdrpc.FiatEstimateRequest{
		Requests: []*frdrpc.FiatEstimateRequest_PriceRequest{
			{
				Id:         fmt.Sprintf("%v msat", amt),
				AmountMsat: amt,
				Timestamp:  ts,
			},
		},
	}

	rpcCtx := context.Background()
	recs, err := client.FiatEstimate(rpcCtx, req)
	if err != nil {
		return err
	}

	// Print response with USD suffix so it is more readable.
	for label, value := range recs.FiatValues {
		fmt.Printf("%v: %v USD\n", label, value)
	}

	return nil
}
