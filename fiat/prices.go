package fiat

import (
	"context"
	"errors"
	"sort"
	"time"

	"github.com/lightningnetwork/lnd/lnwire"
	"github.com/shopspring/decimal"
)

var (
	errNoPrices        = errors.New("no price data provided")
	errPriceOutOfRange = errors.New("timestamp before beginning of price " +
		"dataset")
)

// PriceRequest describes a request for price information.
type PriceRequest struct {
	// Identifier uniquely identifies the request.
	Identifier string

	// Value is the amount of BTC in msat.
	Value lnwire.MilliSatoshi

	// Timestamp is the time at which the price should be obtained.
	Timestamp time.Time
}

// GetPrices gets a set of prices for a set of timestamps.
func GetPrices(ctx context.Context, timestamps []time.Time,
	granularity Granularity) (map[time.Time]*Price, error) {

	if len(timestamps) == 0 {
		return nil, nil
	}

	log.Debugf("getting prices for: %v requests", len(timestamps))

	// Sort our timestamps in ascending order so that we can get the start
	// and end period we need.
	sort.SliceStable(timestamps, func(i, j int) bool {
		return timestamps[i].Before(timestamps[j])
	})

	// Get the earliest and latest timestamps we can, these may be the same
	// timestamp if we have 1 entry, but that's ok.
	start, end := timestamps[0], timestamps[len(timestamps)-1]

	priceData, err := CoinCapPriceData(ctx, start, end, granularity)
	if err != nil {
		return nil, err
	}

	// Prices will map transaction timestamps to their fiat prices.
	var prices = make(map[time.Time]*Price, len(timestamps))

	for _, ts := range timestamps {
		price, err := GetPrice(priceData, ts)
		if err != nil {
			return nil, err
		}

		prices[ts] = price
	}

	return prices, nil
}

// CoinCapPriceData obtains price data over a given range for coincap.
func CoinCapPriceData(ctx context.Context, start, end time.Time,
	granularity Granularity) ([]*Price, error) {

	coinCapBackend := newCoinCapAPI(granularity)
	return coinCapBackend.GetPrices(ctx, start, end, "USD")
}

// MsatToFiat converts a msat amount to fiat. Note that this function coverts
// values to Bitcoin values, then gets the fiat price for that BTC value.
func MsatToFiat(price decimal.Decimal, amt lnwire.MilliSatoshi) decimal.Decimal {
	msatDecimal := decimal.NewFromInt(int64(amt))

	// We are quoted price per whole bitcoin. We need to scale this price
	// down to price per msat - 1 BTC * 10000000 sats * 1000 msats.
	pricePerMSat := price.Div(decimal.NewFromInt(100000000000))

	return pricePerMSat.Mul(msatDecimal)
}

// convertFromUSD converts an array of Price objects denoted in USD into the
// another currency given that currencies USD rate.
func convertFromUSD(prices []*Price, currency string, usdRate decimal.Decimal) []*Price {
	res := make([]*Price, len(prices))
	for i, p := range prices {
		res[i] = &Price{
			Timestamp: p.Timestamp,
			Price:     p.Price.Div(usdRate),
			Currency:  currency,
		}
	}

	return res
}

// GetPrice gets the price for a given time from a set of price data. This
// function expects the price data to be sorted with ascending timestamps and
// for first timestamp in the price data to be before any timestamp we are
// querying. The last datapoint's timestamp may be before the timestamp we are
// querying. If a request lies between two price points, we just return the
// earlier price.
func GetPrice(prices []*Price, timestamp time.Time) (*Price, error) {
	if len(prices) == 0 {
		return nil, errNoPrices
	}

	var lastPrice *Price

	// Run through our prices until we find a timestamp that our price
	// point lies before. Since we always return the previous price, this
	// also works for timestamps that are exactly equal (at the cost of a
	// single extra iteration of this loop).
	for _, price := range prices {
		if timestamp.Before(price.Timestamp) {
			break
		}

		lastPrice = price
	}

	// If we have broken our loop without setting the value of our last
	// price, we have a timestamp that is before the first entry in our
	// series. We expect our range of price points to start before any
	// timestamps we query, so we fail.
	if lastPrice == nil {
		return nil, errPriceOutOfRange
	}

	// Otherwise, we return the last price that was before (or equal to)
	// our timestamp.
	return lastPrice, nil
}
