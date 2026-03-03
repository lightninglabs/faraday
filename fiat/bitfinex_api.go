package fiat

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/shopspring/decimal"
)

const (
	// bitfinexHistoryAPI is the endpoint for historical candle data.
	// The URL path encodes the time-frame and trading pair:
	//   /v2/candles/trade:<timeframe>:<symbol>/hist
	bitfinexHistoryAPI = "https://api-pub.bitfinex.com" +
		"/v2/candles/trade:%s:%s/hist"

	// bitfinexDefaultPair is the trading pair used to obtain BTC/USD
	// prices. Trading pair symbols are formed prepending a "t".
	bitfinexDefaultPair = "tBTCUSD"

	// bitfinexDefaultCurrency is the fiat currency returned.
	bitfinexDefaultCurrency = "USD"

	// bitfinexCandleCap is the maximum number of candles the API returns
	// per request.
	bitfinexCandleCap = 10000
)

// bitfinexTimeframe maps a Granularity to the Bitfinex candle key string.
var bitfinexTimeframe = map[Granularity]string{
	GranularityHour: "1h",
	GranularityDay:  "1D",
}

// bitfinexAPI implements the fiatBackend interface using the Bitfinex v2
// public candles endpoint.
type bitfinexAPI struct {
	// granularity controls the candle bucket size (hour or day).
	granularity Granularity

	// pair is the Bitfinex symbol, e.g. "tBTCUSD".
	pair string

	// client is the HTTP client used to make requests.
	client *http.Client
}

// newBitfinexAPI returns a bitfinexAPI that satisfies fiatBackend.
func newBitfinexAPI(g Granularity) *bitfinexAPI {
	return &bitfinexAPI{
		granularity: g,
		pair:        bitfinexDefaultPair,
		client: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// queryBitfinex performs one HTTP request for a single window of up to
// bitfinexCandleCap candles. Timestamps are in milliseconds. The sort=1
// parameter requests ascending order.
func queryBitfinex(start, end time.Time, pair, timeframe string,
	cl *http.Client) ([]byte, error) {

	base := fmt.Sprintf(bitfinexHistoryAPI, timeframe, pair)
	params := url.Values{}
	params.Set("limit", strconv.Itoa(bitfinexCandleCap))
	params.Set("start", strconv.FormatInt(start.UnixMilli(), 10))
	params.Set("end", strconv.FormatInt(end.UnixMilli(), 10))
	params.Set("sort", "1")

	// #nosec G107 – public data
	resp, err := cl.Get(base + "?" + params.Encode())
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	return io.ReadAll(resp.Body)
}

// parseBitfinexData parses the JSON response from the Bitfinex candles
// endpoint.
//
// Bitfinex v2 public candles endpoint
//
//	GET https://api-pub.bitfinex.com
//		/v2/candles/trade:<timeframe>:<symbol>/hist
//
// Response body -- array of fixed-width arrays (when sort=1, ascending):
//
//	[
//	  [ MTS,   OPEN,  CLOSE,  HIGH,  LOW,   VOLUME ],
//	  ...
//	]
//
// Field meanings:
//   - MTS    -- millisecond timestamp (bucket open).
//   - OPEN   -- first execution price during the bucket interval.
//   - CLOSE  -- last execution price during the bucket interval.
//   - HIGH   -- highest execution price during the bucket interval.
//   - LOW    -- lowest execution price during the bucket interval.
//   - VOLUME -- quantity of base asset traded during the bucket interval.
//
// We use the CLOSE price (index 2) to be consistent with the other
// backends.
func parseBitfinexData(data []byte) ([]*Price, error) {
	var raw [][]float64
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, err
	}

	prices := make([]*Price, 0, len(raw))
	for _, c := range raw {
		if len(c) < 3 {
			continue
		}

		ts := time.UnixMilli(int64(c[0])).UTC()
		closePx := decimal.NewFromFloat(c[2])

		prices = append(prices, &Price{
			Timestamp: ts,
			Price:     closePx,
			Currency:  bitfinexDefaultCurrency,
		})
	}

	return prices, nil
}

// rawPriceData satisfies the fiatBackend interface.
func (b *bitfinexAPI) rawPriceData(ctx context.Context,
	startTime, endTime time.Time) ([]*Price, error) {

	tf, ok := bitfinexTimeframe[b.granularity]
	if !ok {
		return nil, fmt.Errorf("bitfinex: unsupported granularity %v",
			b.granularity.label)
	}

	// Each request returns at most bitfinexCandleCap candles. We page
	// forward by advancing start past the last received timestamp.
	chunk := b.granularity.aggregation * bitfinexCandleCap
	start := startTime.Truncate(b.granularity.aggregation)
	end := start.Add(chunk)
	if end.After(endTime) {
		end = endTime
	}

	var all []*Price
	seen := make(map[int64]struct{})
	for start.Before(endTime) {
		queryStart, queryEnd := start, end
		query := func() ([]byte, error) {
			return queryBitfinex(
				queryStart, queryEnd, b.pair, tf, b.client,
			)
		}

		records, err := retryQuery(ctx, query, parseBitfinexData)
		if err != nil {
			return nil, err
		}

		// Bitfinex candles can include boundary timestamps for both
		// start and end. Filter duplicates across page boundaries by
		// timestamp.
		for _, record := range records {
			ts := record.Timestamp.UnixMilli()
			if _, ok := seen[ts]; ok {
				continue
			}

			seen[ts] = struct{}{}
			all = append(all, record)
		}

		start = end
		end = start.Add(chunk)
		if end.After(endTime) {
			end = endTime
		}
	}

	return all, nil
}
