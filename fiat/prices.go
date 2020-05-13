package fiat

import (
	"context"
	"errors"
	"time"

	"github.com/lightningnetwork/lnd/lnwire"
	"github.com/shopspring/decimal"
)

var (
	errNoPrices       = errors.New("no price data provided")
	errDuplicateLabel = errors.New("duplicate label in request set")
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
func GetPrices(ctx context.Context, requests []*PriceRequest,
	granularity Granularity) (map[string]decimal.Decimal, error) {

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

	priceData, err := CoinCapPriceData(ctx, start, end, granularity)
	if err != nil {
		return nil, err
	}

	// Prices will map transaction identifiers to their USD prices.
	var prices = make(map[string]decimal.Decimal, len(requests))

	for _, request := range requests {
		price, err := GetPrice(priceData, request)
		if err != nil {
			return nil, err
		}

		prices[request.Identifier] = price
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

// msatToUSD converts a msat amount to usd. Note that this function coverts
// values to Bitcoin values, then gets the fiat price for that BTC value. If
// an amount < 1000 msat is given, a zero amount will be returned.
func msatToUSD(price decimal.Decimal, amt lnwire.MilliSatoshi) decimal.Decimal {
	msatDecimal := decimal.NewFromFloat(amt.ToBTC())
	return price.Mul(msatDecimal)
}

// GetPrice gets the price for a timestamped request from a set of price data.
// This function expects the price data to be sorted with ascending timestamps.
// If request lies between two price points, we simply aggregate the two prices.
func GetPrice(prices []*USDPrice, request *PriceRequest) (decimal.Decimal,
	error) {

	lastPrice := decimal.Zero

	if len(prices) == 0 {
		return decimal.Zero, errNoPrices
	}

	for _, price := range prices {
		// Check the optimistic case where the price timestamp matches
		// our timestamp exactly.
		if price.timestamp.Equal(request.Timestamp) {
			return msatToUSD(price.price, request.Value), nil
		}

		// Once we reach a price point that is before our request's
		// timestamp, the request's timestamp lies somewhere between
		// the current price data point and the previous on.
		if request.Timestamp.Before(price.timestamp) {
			// If the last price is 0, the request is after the
			// very first price data point. We do not aggregate in
			// this case.
			if lastPrice.Equal(decimal.Zero) {
				return msatToUSD(price.price, request.Value),
					nil
			}

			// Otherwise, aggregate the price over the current data
			// point and the next one.
			price := decimal.Avg(lastPrice, price.price)
			return msatToUSD(price, request.Value), nil
		}

		lastPrice = price.price
	}

	// If we have fallen through to this point, the price's timestamp falls
	// after our last price data point's timestamp. In this case, we just
	// return the price quoted on that price.
	return msatToUSD(lastPrice, request.Value), nil
}
