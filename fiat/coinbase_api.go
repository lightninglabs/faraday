package fiat

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/shopspring/decimal"
)

const (
	coinbaseHistoryAPI  = "https://api.exchange.coinbase.com/products/%s/candles"
	coinbaseDefaultPair = "BTC-USD"
	coinbaseCandleCap   = 300   // max buckets.
	coinbaseGranHourSec = 3600  // 1‑hour buckets.
	coinbaseGranDaySec  = 86400 // 1‑day  buckets.
	coinbaseDefaultCurr = "USD"
)

type coinbaseAPI struct {
	// granularity is the price granularity (must be GranularityHour or
	// GranularityDay for coinbase).
	granularity Granularity

	// product is the Coinbase product pair (e.g. BTC-USD).
	product string

	// client is the HTTP client used to make requests.
	client *http.Client
}

// newCoinbaseAPI returns an implementation that satisfies fiatBackend.
func newCoinbaseAPI(g Granularity) *coinbaseAPI {
	return &coinbaseAPI{
		granularity: g,
		product:     coinbaseDefaultPair,
		client: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// queryCoinbase performs one HTTP request for a single <300‑bucket window.
func queryCoinbase(start, end time.Time, product string,
	g Granularity, cl *http.Client) ([]byte, error) {

	url := fmt.Sprintf(coinbaseHistoryAPI, product) +
		fmt.Sprintf("?start=%s&end=%s&granularity=%d",
			start.Format(time.RFC3339),
			end.Format(time.RFC3339),
			int(g.aggregation.Seconds()))

	// #nosec G107 – public data
	resp, err := cl.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	return io.ReadAll(resp.Body)
}

// parseCoinbaseData parses the JSON response from Coinbase's candles endpoint.
//
// Coinbase “product candles” endpoint
//
//	GET https://api.exchange.coinbase.com/products/<product‑id>/candles
//
// Response body ─ array of fixed‑width arrays:
//
//	[
//	  [ time,  low,       high,      open,      close,     volume ],
//	  ...
//	]
//
// Field meanings (per Coinbase docs [1]):
//   - time    – UNIX epoch **seconds** marking the *start* of the bucket (UTC).
//   - low     – lowest trade price during the bucket interval.
//   - high    – highest trade price during the bucket interval.
//   - open    – price of the first trade in the interval.
//   - close   – price of the last trade in the interval.
//   - volume  – amount of the base‑asset traded during the interval.
//
// Additional quirks
//   - Candles are returned in *reverse‑chronological* order (newest‑first).
//   - `granularity` must be one of 60, 300, 900, 3600, 21600, 86400 seconds.
//   - A single request can return at most 300 buckets; larger spans must be
//     paged by adjusting `start`/`end` query parameters.
//
// Example (1‑hour granularity, newest‑first):
//
//	[
//	  [1714632000, 64950.12, 65080.00, 65010.55, 65075.00, 84.213],
//	  [1714628400, 64890.00, 65020.23, 64900.00, 64950.12, 92.441],
//	  ...
//	]
//
// [1] https://docs.cdp.coinbase.com/exchange/reference/exchangerestapi_getproductcandles
func parseCoinbaseData(data []byte) ([]*Price, error) {
	var raw [][]float64
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, err
	}

	prices := make([]*Price, 0, len(raw))
	for _, c := range raw {
		// Historical rate data may be incomplete. No data is published
		// for intervals where there are no ticks.
		if len(c) < 5 {
			continue
		}
		ts := time.Unix(int64(c[0]), 0).UTC()
		closePx := decimal.NewFromFloat(c[4])

		prices = append(prices, &Price{
			Timestamp: ts,
			Price:     closePx,
			Currency:  coinbaseDefaultCurr,
		})
	}
	return prices, nil
}

// rawPriceData satisfies the fiatBackend interface.
func (c *coinbaseAPI) rawPriceData(ctx context.Context,
	startTime, endTime time.Time) ([]*Price, error) {

	// Coinbase cap = 300 * granularity.
	chunk := c.granularity.aggregation * coinbaseCandleCap
	start := startTime.Truncate(c.granularity.aggregation)
	end := start.Add(chunk)
	if end.After(endTime) {
		end = endTime
	}

	var all []*Price
	for start.Before(endTime) {
		query := func() ([]byte, error) {
			return queryCoinbase(
				start, end, c.product, c.granularity, c.client,
			)
		}

		records, err := retryQuery(ctx, query, parseCoinbaseData)
		if err != nil {
			return nil, err
		}
		all = append(all, records...)

		start = end
		end = start.Add(chunk)
		if end.After(endTime) {
			end = endTime
		}
	}

	return all, nil
}
