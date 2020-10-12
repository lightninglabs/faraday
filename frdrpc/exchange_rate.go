package frdrpc

import (
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/lightninglabs/faraday/fiat"
)

// granularityFromRPC gets a granularity enum value from a rpc request,
// defaulting getting the best granularity for the period being queried.
func granularityFromRPC(g Granularity, disableFiat bool,
	duration time.Duration) (*fiat.Granularity, error) {

	// If we do not need fiat prices, we can return nil granularity.
	if disableFiat {
		return nil, nil
	}

	switch g {
	// If granularity is not set, allow it to default to the best
	// granularity that we can get for the query period.
	case Granularity_UNKNOWN_GRANULARITY:
		best, err := fiat.BestGranularity(duration)
		if err != nil {
			return nil, err
		}

		return &best, nil

	case Granularity_MINUTE:
		return &fiat.GranularityMinute, nil

	case Granularity_FIVE_MINUTES:
		return &fiat.Granularity5Minute, nil

	case Granularity_FIFTEEN_MINUTES:
		return &fiat.Granularity15Minute, nil

	case Granularity_THIRTY_MINUTES:
		return &fiat.Granularity30Minute, nil

	case Granularity_HOUR:
		return &fiat.GranularityHour, nil

	case Granularity_SIX_HOURS:
		return &fiat.Granularity6Hour, nil

	case Granularity_TWELVE_HOURS:
		return &fiat.Granularity12Hour, nil

	case Granularity_DAY:
		return &fiat.GranularityDay, nil

	default:
		return nil, fmt.Errorf("unknown granularity: %v", g)
	}
}

func parseExchangeRateRequest(req *ExchangeRateRequest) ([]time.Time,
	*fiat.Granularity, error) {

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

	granularity, err := granularityFromRPC(
		req.Granularity, false, end.Sub(start),
	)
	if err != nil {
		return nil, nil, err
	}

	return timestamps, granularity, nil
}

func exchangeRateResponse(prices map[time.Time]*fiat.USDPrice) *ExchangeRateResponse {
	fiatVals := make([]*ExchangeRate, 0, len(prices))

	for ts, price := range prices {
		fiatVals = append(fiatVals, &ExchangeRate{
			Timestamp: uint64(ts.Unix()),
			BtcPrice: &BitcoinPrice{
				Price:          price.Price.String(),
				PriceTimestamp: uint64(price.Timestamp.Unix()),
			},
		})
	}

	return &ExchangeRateResponse{
		Rates: fiatVals,
	}
}
