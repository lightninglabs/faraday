package main

import (
	"context"
	"fmt"
	"time"

	"github.com/lightningnetwork/lnd/lnwire"
	"github.com/shopspring/decimal"
	"github.com/urfave/cli"

	"github.com/lightninglabs/faraday/fiat"
	"github.com/lightninglabs/faraday/frdrpc"
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
		cli.StringFlag{
			Name: "fiat_backend",
			Usage: "fiat backend to be used. Options include: " +
				"'coincap' (default) and 'coindesk'",
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

	fiatBackend, err := parseFiatBackend(ctx.String("fiat_backend"))
	if err != nil {
		return err
	}

	// Set start and end times from user specified values, defaulting
	// to zero if they are not set.
	req := &frdrpc.ExchangeRateRequest{
		Timestamps:  []uint64{uint64(ts)},
		FiatBackend: fiatBackend,
	}

	rpcCtx := context.Background()
	recs, err := client.ExchangeRate(rpcCtx, req)
	if err != nil {
		return err
	}

	count := len(recs.Rates)
	if count != 1 {
		return fmt.Errorf("unexpected number of fiat estimates: %v",
			count)
	}

	estimate := recs.Rates[0]
	if estimate.Timestamp != uint64(ts) {
		return fmt.Errorf("expected price for: %v, got: %v", ts,
			estimate.Timestamp)
	}

	bitcoinPrice, err := decimal.NewFromString(estimate.BtcPrice.Price)
	if err != nil {
		return err
	}

	fiatVal := fiat.MsatToFiat(bitcoinPrice, lnwire.MilliSatoshi(amt))
	priceTs := time.Unix(int64(estimate.BtcPrice.PriceTimestamp), 0)

	fmt.Printf("%v msat = %v %s, priced at %v\n",
		amt, fiatVal, estimate.BtcPrice.Currency, priceTs)

	return nil
}
