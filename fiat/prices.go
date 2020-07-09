package fiat

import (
	"context"
	"errors"
	"time"

	"github.com/lightningnetwork/lnd/lnwire"
	"github.com/shopspring/decimal"
)

var (
	errNoPrices        = errors.New("no price data provided")
	errDuplicateLabel  = errors.New("duplicate label in request set")
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

// GetPrices gets a set of prices for a set of timestamped requests.
func GetPrices(ctx context.Context,
	requests []*PriceRequest) (map[string]decimal.Decimal, error) {

	if len(requests) == 0 {
		return nil, nil
	}

	log.Debugf("getting prices for: %v requests", len(requests))

	// Make sure that every label that in the request set is unique.
	uniqueLabels := make(map[string]bool, len(requests))
	for _, request := range requests {
		_, ok := uniqueLabels[request.Identifier]
		if ok {
			return nil, errDuplicateLabel
		}

		uniqueLabels[request.Identifier] = true
	}

	// Get the minimum and maximum timestamps for our set of requests
	// so that we can efficiently query for price data.
	start, end := getQueryableDuration(requests)

	granularity, err := BestGranularity(end.Sub(start))
	if err != nil {
		return nil, err
	}

	priceData, err := CoinCapPriceData(ctx, start, end, granularity)
	if err != nil {
		return nil, err
	}

	// Prices will map transaction identifiers to their USD prices.
	var prices = make(map[string]decimal.Decimal, len(requests))

	for _, request := range requests {
		price, err := GetPrice(priceData, request.Timestamp)
		if err != nil {
			return nil, err
		}

		prices[request.Identifier] = MsatToUSD(
			price.Price, request.Value,
		)
	}

	return prices, nil
}

// CoinCapPriceData obtains price data over a given range for coincap.
func CoinCapPriceData(ctx context.Context, start, end time.Time,
	granularity Granularity) ([]*USDPrice, error) {

	coinCapBackend := newCoinCapAPI(granularity)
	return coinCapBackend.GetPrices(ctx, start, end)
}

// getQueryableDuration gets the smallest and largest timestamp from a set of
// requests so that we can query for an appropriate set of price data.
func getQueryableDuration(requests []*PriceRequest) (time.Time, time.Time) {
	var start, end time.Time
	// Iterate through our min and max times and get the time range over
	// which we need to get price information.
	for _, req := range requests {
		if start.IsZero() || start.After(req.Timestamp) {
			start = req.Timestamp
		}

		if end.IsZero() || end.Before(req.Timestamp) {
			end = req.Timestamp
		}
	}

	return start, end
}

// MsatToUSD converts a msat amount to usd. Note that this function coverts
// values to Bitcoin values, then gets the fiat price for that BTC value.
func MsatToUSD(price decimal.Decimal, amt lnwire.MilliSatoshi) decimal.Decimal {
	msatDecimal := decimal.NewFromInt(int64(amt))

	// We are quoted price per whole bitcoin. We need to scale this price
	// down to price per msat - 1 BTC * 10000000 sats * 1000 msats.
	pricePerMSat := price.Div(decimal.NewFromInt(100000000000))

	return pricePerMSat.Mul(msatDecimal)
}

// GetPrice gets the price for a given time from a set of price data. This
// function expects the price data to be sorted with ascending timestamps and
// for first timestamp in the price data to be before any timestamp we are
// querying. The last datapoint's timestamp may be before the timestamp we are
// querying. If a request lies between two price points, we just return the
// earlier price.
func GetPrice(prices []*USDPrice, timestamp time.Time) (*USDPrice, error) {
	if len(prices) == 0 {
		return nil, errNoPrices
	}

	var lastPrice *USDPrice

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
