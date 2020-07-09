package accounting

import (
	"context"
	"time"

	"github.com/btcsuite/btcutil"
	"github.com/lightninglabs/faraday/fiat"
	"github.com/lightninglabs/faraday/utils"
	"github.com/lightningnetwork/lnd/lnwire"
	"github.com/shopspring/decimal"
)

// msatToFiat is a function which converts a timestamped millisatoshi balance to
// a fiat value.
type msatToFiat func(amount int64, timestamp time.Time) (decimal.Decimal, error)

// satsToMsat converts an amount expressed in sats to msat.
func satsToMsat(sats btcutil.Amount) int64 {
	return int64(sats) * 1000
}

// satsToMsat converts an amount expressed in sats to msat, flipping the
// sign on the value.
func invertedSatsToMsats(sats btcutil.Amount) int64 {
	return satsToMsat(sats) * -1
}

// invertMsat flips the sign value of a msat value.
func invertMsat(msat int64) int64 {
	return msat * -1
}

// getConversion is a helper function which queries coincap for a relevant set
// of price data and returns a convert function which can be used to get
// individual price points from this data.
func getConversion(ctx context.Context, startTime, endTime time.Time,
	disableFiat bool) (msatToFiat, error) {

	// If we don't want fiat values, just return a no-op function.
	if disableFiat {
		return func(_ int64, _ time.Time) (decimal.Decimal, error) {
			return decimal.Zero, nil
		}, nil
	}

	err := utils.ValidateTimeRange(startTime, endTime)
	if err != nil {
		return nil, err
	}

	granularity, err := fiat.BestGranularity(endTime.Sub(startTime))
	if err != nil {
		return nil, err
	}

	// Get price data for our relevant period. We get pricing for the whole
	// period rather than on a per-item level to limit the number of api
	// calls we need to make to our external data source.
	prices, err := fiat.CoinCapPriceData(
		ctx, startTime, endTime, granularity,
	)
	if err != nil {
		return nil, err
	}

	// Create a wrapper function which can be used to get individual price
	// points from our set of price data as we create our report.
	return func(amtMsat int64, ts time.Time) (decimal.Decimal, error) {
		price, err := fiat.GetPrice(prices, ts)
		if err != nil {
			return decimal.Zero, err
		}

		return fiat.MsatToUSD(
			price.Price, lnwire.MilliSatoshi(amtMsat),
		), nil
	}, nil
}
