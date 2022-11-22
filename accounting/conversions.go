package accounting

import (
	"context"
	"time"

	"github.com/btcsuite/btcd/btcutil"
	"github.com/lightninglabs/faraday/fiat"
	"github.com/lightninglabs/faraday/utils"
)

// fiatPrice is a function which gets the fiat price of bitcoin at a given time.
type fiatPrice func(timestamp time.Time) (*fiat.Price, error)

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
	disableFiat bool, priceCfg *fiat.PriceSourceConfig) (fiatPrice, error) {

	// If we don't want fiat values, just return a price which will yield
	// a zero price and timestamp.
	if disableFiat {
		return func(_ time.Time) (*fiat.Price, error) {
			return &fiat.Price{}, nil
		}, nil
	}

	err := utils.ValidateTimeRange(startTime, endTime)
	if err != nil {
		return nil, err
	}

	fiatClient, err := fiat.NewPriceSource(priceCfg)
	if err != nil {
		return nil, err
	}

	// Get price data for our relevant period. We get pricing for the whole
	// period rather than on a per-item level to limit the number of api
	// calls we need to make to our external data source.
	prices, err := fiatClient.GetPrices(ctx, startTime, endTime)
	if err != nil {
		return nil, err
	}

	// Create a wrapper function which can be used to get individual price
	// points from our set of price data as we create our report.
	return func(ts time.Time) (*fiat.Price, error) {
		return fiat.GetPrice(prices, ts)
	}, nil
}
