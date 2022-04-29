package frdrpcserver

import (
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/lightninglabs/faraday/fiat"
	"github.com/lightninglabs/faraday/frdrpc"
)

func priceCfgFromRPC(rpcBackend frdrpc.FiatBackend,
	rpcGranularity frdrpc.Granularity, disable bool, start, end time.Time,
	prices []*frdrpc.BitcoinPrice) (*fiat.PriceSourceConfig, error) {

	backend, err := fiatBackendFromRPC(rpcBackend)
	if err != nil {
		return nil, err
	}

	if len(prices) > 0 && backend != fiat.CustomPriceBackend {
		return nil, errors.New(
			"custom price points provided but custom fiat " +
				"backend not set",
		)
	}

	var (
		granularity *fiat.Granularity
		pricePoints []*fiat.Price
	)

	// Get additional values for backends that require additional
	// information.
	switch backend {
	case fiat.CoinCapPriceBackend:
		granularity, err = granularityFromRPC(
			rpcGranularity, disable, end.Sub(start),
		)

	case fiat.CustomPriceBackend:
		pricePoints, err = pricePointsFromRPC(prices)
		if err != nil {
			return nil, err
		}

		err = validateCustomPricePoints(pricePoints, start)
	}
	if err != nil {
		return nil, err
	}

	return &fiat.PriceSourceConfig{
		Backend:     backend,
		Granularity: granularity,
		PricePoints: pricePoints,
	}, nil
}

// granularityFromRPC gets a granularity enum value from a rpc request,
// defaulting getting the best granularity for the period being queried.
func granularityFromRPC(g frdrpc.Granularity, disableFiat bool,
	duration time.Duration) (*fiat.Granularity, error) {

	// If we do not need fiat prices, we can return nil granularity.
	if disableFiat {
		return nil, nil
	}

	switch g {
	// If granularity is not set, allow it to default to the best
	// granularity that we can get for the query period.
	case frdrpc.Granularity_UNKNOWN_GRANULARITY:
		best, err := fiat.BestGranularity(duration)
		if err != nil {
			return nil, err
		}

		return &best, nil

	case frdrpc.Granularity_MINUTE:
		return &fiat.GranularityMinute, nil

	case frdrpc.Granularity_FIVE_MINUTES:
		return &fiat.Granularity5Minute, nil

	case frdrpc.Granularity_FIFTEEN_MINUTES:
		return &fiat.Granularity15Minute, nil

	case frdrpc.Granularity_THIRTY_MINUTES:
		return &fiat.Granularity30Minute, nil

	case frdrpc.Granularity_HOUR:
		return &fiat.GranularityHour, nil

	case frdrpc.Granularity_SIX_HOURS:
		return &fiat.Granularity6Hour, nil

	case frdrpc.Granularity_TWELVE_HOURS:
		return &fiat.Granularity12Hour, nil

	case frdrpc.Granularity_DAY:
		return &fiat.GranularityDay, nil

	default:
		return nil, fmt.Errorf("unknown granularity: %v", g)
	}
}

func fiatBackendFromRPC(backend frdrpc.FiatBackend) (fiat.PriceBackend, error) {
	switch backend {
	case frdrpc.FiatBackend_UNKNOWN_FIATBACKEND:
		return fiat.UnknownPriceBackend, nil

	case frdrpc.FiatBackend_COINCAP:
		return fiat.CoinCapPriceBackend, nil

	case frdrpc.FiatBackend_COINDESK:
		return fiat.CoinDeskPriceBackend, nil

	case frdrpc.FiatBackend_CUSTOM:
		return fiat.CustomPriceBackend, nil

	case frdrpc.FiatBackend_COINGECKO:
		return fiat.CoinGeckoPriceBackend, nil

	default:
		return fiat.UnknownPriceBackend,
			fmt.Errorf("unknown fiat backend: %v", backend)
	}
}

func parseExchangeRateRequest(req *frdrpc.ExchangeRateRequest) ([]time.Time,
	*fiat.PriceSourceConfig, error) {

	if len(req.Timestamps) == 0 {
		return nil, nil, errors.New("at least one timestamp required")
	}

	timestamps := make([]time.Time, len(req.Timestamps))

	for i, timestamp := range req.Timestamps {
		timestamps[i] = time.Unix(int64(timestamp), 0)
	}

	// Sort timestamps in ascending order so that we can get the duration
	// we're querying over.
	sort.SliceStable(timestamps, func(i, j int) bool {
		return timestamps[i].Before(timestamps[j])
	})

	// Get our start and end times, these may be the same if we have a
	// single timestamp.
	start, end := timestamps[0], timestamps[len(timestamps)-1]

	cfg, err := priceCfgFromRPC(
		req.FiatBackend, req.Granularity, false, start, end,
		req.CustomPrices,
	)
	if err != nil {
		return nil, nil, err
	}

	return timestamps, cfg, nil
}

func exchangeRateResponse(
	prices map[time.Time]*fiat.Price) *frdrpc.ExchangeRateResponse {

	fiatVals := make([]*frdrpc.ExchangeRate, 0, len(prices))

	for ts, price := range prices {
		fiatVals = append(fiatVals, &frdrpc.ExchangeRate{
			Timestamp: uint64(ts.Unix()),
			BtcPrice: &frdrpc.BitcoinPrice{
				Price:          price.Price.String(),
				PriceTimestamp: uint64(price.Timestamp.Unix()),
				Currency:       price.Currency,
			},
		})
	}

	return &frdrpc.ExchangeRateResponse{
		Rates: fiatVals,
	}
}
