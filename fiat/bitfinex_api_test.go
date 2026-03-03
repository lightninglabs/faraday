package fiat

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/jarcoal/httpmock"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

// TestParseBitfinexData tests parsing of the candle array format returned
// by the Bitfinex v2 public candles endpoint.
func TestParseBitfinexData(t *testing.T) {
	t.Parallel()

	// Bitfinex returns: [MTS, OPEN, CLOSE, HIGH, LOW, VOLUME].
	// We use the CLOSE field (index 2).
	ts1 := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	ts2 := time.Date(2024, 1, 1, 1, 0, 0, 0, time.UTC)

	input := []byte(`[
		[` + fmt.Sprintf("%d", ts1.UnixMilli()) +
		`, 42000.0, 42100.5, 42200.0, 41900.0, 12.5],
		[` + fmt.Sprintf("%d", ts2.UnixMilli()) +
		`, 42100.0, 42300.0, 42400.0, 42050.0, 8.3]
	]`)

	prices, err := parseBitfinexData(input)
	require.NoError(t, err)

	expected := []*Price{
		{
			Timestamp: ts1,
			Price:     decimal.NewFromFloat(42100.5),
			Currency:  "USD",
		},
		{
			Timestamp: ts2,
			Price:     decimal.NewFromFloat(42300.0),
			Currency:  "USD",
		},
	}
	require.Equal(t, expected, prices)
}

// TestParseBitfinexDataShortRow verifies that rows with fewer than 3
// elements are silently skipped.
func TestParseBitfinexDataShortRow(t *testing.T) {
	t.Parallel()

	input := []byte(`[[1700000000000, 42000.0], [1700003600000, ` +
		`42100.0, 42200.0, 42300.0, 42050.0, 5.0]]`)
	prices, err := parseBitfinexData(input)
	require.NoError(t, err)
	require.Len(t, prices, 1)
	require.Equal(t, decimal.NewFromFloat(42200.0), prices[0].Price)
}

// TestBitfinexRawPriceData tests the paging logic of rawPriceData using a
// mocked HTTP transport.
func TestBitfinexRawPriceData(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Hour)
	start := now.Add(-time.Hour * 4)

	mock := httpmock.NewMockTransport()
	client := &http.Client{Transport: mock}

	const numCandles = 4
	candles := make([][]float64, numCandles)
	for i := range candles {
		ts := start.Add(time.Duration(i) * time.Hour)
		candles[i] = []float64{
			float64(ts.UnixMilli()),
			float64(45000 + i), // open
			float64(50000 + i), // close
			float64(55000 + i), // high
			float64(44000 + i), // low
			1.0,                // volume
		}
	}

	expected := make([]*Price, numCandles)
	for i := range expected {
		expected[i] = &Price{
			Timestamp: start.Add(time.Hour * time.Duration(i)),
			Price:     decimal.NewFromFloat(float64(50000 + i)),
			Currency:  "USD",
		}
	}

	mock.RegisterResponder(
		"GET", `=~https://api-pub.bitfinex.com/.*`,
		httpmock.NewJsonResponderOrPanic(200, candles),
	)

	api := newBitfinexAPI(GranularityHour)
	api.client = client

	ctx := context.Background()
	out, err := api.rawPriceData(ctx, start, now)
	require.NoError(t, err)
	require.EqualValues(t, expected, out)
}

// TestBitfinexRawPriceDataNoDuplicateBoundaries verifies that page boundaries
// do not produce duplicate timestamps when multiple requests are made.
func TestBitfinexRawPriceDataNoDuplicateBoundaries(t *testing.T) {
	t.Parallel()

	end := time.Now().UTC().Truncate(time.Hour)
	start := end.Add(-time.Duration(bitfinexCandleCap+1) * time.Hour)

	mock := httpmock.NewMockTransport()
	client := &http.Client{Transport: mock}

	var calls int
	mock.RegisterResponder(
		"GET", `=~https://api-pub.bitfinex.com/.*`,
		func(req *http.Request) (*http.Response, error) {
			calls++

			query := req.URL.Query()
			startMS, err := strconv.ParseInt(
				query.Get("start"), 10, 64,
			)
			if err != nil {
				return nil, err
			}

			endMS, err := strconv.ParseInt(query.Get("end"), 10, 64)
			if err != nil {
				return nil, err
			}

			// Simulate an inclusive API that returns both
			// boundaries.
			candles := [][]float64{
				{
					float64(startMS), 0, float64(startMS),
					0, 0, 1,
				},
				{
					float64(endMS), 0, float64(endMS),
					0, 0, 1,
				},
			}

			return httpmock.NewJsonResponse(200, candles)
		},
	)

	api := newBitfinexAPI(GranularityHour)
	api.client = client

	out, err := api.rawPriceData(context.Background(), start, end)
	require.NoError(t, err)
	require.GreaterOrEqual(t, calls, 2, "expected paging to occur")

	seen := make(map[int64]struct{}, len(out))
	for _, price := range out {
		ts := price.Timestamp.UnixMilli()
		_, ok := seen[ts]
		require.False(t, ok, "duplicate timestamp: %v", price.Timestamp)

		seen[ts] = struct{}{}
	}
}
