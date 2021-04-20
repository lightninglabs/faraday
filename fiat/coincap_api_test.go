package fiat

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

// TestCoinCapGetPrices tests splitting of a query period into the number of
// requests required to obtain the desired granularity.
func TestCoinCapGetPrices(t *testing.T) {
	now := time.Now()
	twoDaysAgo := now.Add(time.Hour * -24 * 2)

	tests := []struct {
		name              string
		granularity       Granularity
		startTime         time.Time
		endTime           time.Time
		expectedCallCount int
		expectedErr       error
	}{
		{
			name:              "single point in time",
			granularity:       GranularityMinute,
			startTime:         now,
			endTime:           now,
			expectedCallCount: 1,
			expectedErr:       nil,
		},
		{
			// One minute has a maximum 24H period, two days ago
			// less the buffer we add to our start time should
			// produce exactly 2 queries.
			name:              "exact period including buffer",
			granularity:       GranularityMinute,
			startTime:         twoDaysAgo.Add(time.Minute),
			endTime:           now,
			expectedCallCount: 2,
			expectedErr:       nil,
		},
		{
			// One minute has a maximum 24H period, two days ago
			// should require 3 due to our buffer of 1 minute.
			name:              "extra for buffer",
			granularity:       GranularityMinute,
			startTime:         twoDaysAgo,
			endTime:           now,
			expectedCallCount: 3,
			expectedErr:       nil,
		},
	}

	for _, test := range tests {
		test := test

		t.Run(test.name, func(t *testing.T) {
			mock := &fakeQuery{}

			// Create a mocked query function which will track
			// our call count and error as required for the test.
			query := func(_, _ time.Time,
				_ Granularity) ([]byte, error) {

				if err := mock.call(); err != nil {
					return nil, err
				}

				return nil, nil
			}

			// Create a mocked convert function.
			convert := func([]byte) ([]*Price, error) {
				return nil, nil
			}

			coinCapAPI := coinCapAPI{
				granularity:  test.granularity,
				queryHistory: query,
				convert:      convert,
			}

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			_, err := coinCapAPI.GetPrices(
				ctx, test.startTime, test.endTime, "USD",
			)
			if err != test.expectedErr {
				t.Fatalf("expected: %v,got: %v",
					test.expectedErr, err)
			}

			if test.expectedCallCount != mock.callCount {
				t.Fatalf("expected call count: %v, got: %v",
					test.expectedCallCount,
					mock.callCount)
			}
		})
	}
}

// TestBestGranularity tests getting of the lowest granularity possible for
// a given query duration.
func TestBestGranularity(t *testing.T) {
	tests := []struct {
		name        string
		duration    time.Duration
		granularity Granularity
		err         error
	}{
		{
			name:        "equal to interval max",
			duration:    GranularityMinute.maximumQuery,
			granularity: GranularityMinute,
			err:         nil,
		},
		{
			name:        "less than interval",
			duration:    time.Second,
			granularity: GranularityMinute,
			err:         nil,
		},
		{
			name:        "within 15 min period",
			duration:    Granularity15Minute.maximumQuery - 100,
			granularity: Granularity15Minute,
			err:         nil,
		},
		{
			name:        "too long",
			duration:    GranularityDay.maximumQuery + 1,
			granularity: Granularity{},
			err:         ErrQueryTooLong,
		},
	}

	for _, test := range tests {
		test := test

		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			best, err := BestGranularity(test.duration)
			require.Equal(t, test.err, err)
			require.Equal(t, test.granularity, best)
		})
	}
}

// TestParseCoinCapData adds a test which checks that we appropriately parse
// the price and timestamp data returned by coincap's api.
func TestParseCoinCapData(t *testing.T) {
	var (
		// Create two prices, one which is a float to ensure that we
		// are correctly parsing them.
		price1 = decimal.NewFromFloat(10.1)
		price2 = decimal.NewFromInt(110000)

		// Create two timestamps, each representing our time in
		// milliseconds.
		time1 = time.Unix(10000, 0)
		time2 = time.Unix(2000, 0)
	)

	// Create the struct we expect to receive from coincap and marshal it
	// into bytes. We set our timestamps to Unix() *1000 so that our time
	// stamps are expressed in milliseconds.
	resps := coinCapResponse{
		Data: []*coinCapDataPoint{
			{
				Price:     price1.String(),
				Timestamp: time1.Unix() * 1000,
			},
			{
				Price:     price2.String(),
				Timestamp: time2.Unix() * 1000,
			},
		},
	}

	bytes, err := json.Marshal(resps)
	require.NoError(t, err)

	prices, err := parseCoinCapData(bytes)
	require.NoError(t, err)

	expectedPrices := []*Price{
		{
			Price:     price1,
			Timestamp: time1,
			Currency:  "USD",
		},
		{
			Price:     price2,
			Timestamp: time2,
			Currency:  "USD",
		},
	}

	require.Equal(t, expectedPrices, prices)
}

// TestParseAndFindCoinCapRate
func TestParseAndFindCoinCapRate(t *testing.T) {
	var (
		amtStr1 = "0.068"
		amtStr2 = "1.19"

		curr1 = "ZAR"
		curr2 = "EUR"
		curr3 = "AUD"
	)

	amt1, err := decimal.NewFromString(amtStr1)
	require.NoError(t, err)

	amt2, err := decimal.NewFromString(amtStr2)
	require.NoError(t, err)

	ratesData := &coinCapRatesResponse{
		Data: []*coinCapRatePoint{
			{
				RateUsd: amtStr1,
				Symbol:  curr1,
			},
			{
				RateUsd: amtStr2,
				Symbol:  curr2,
			},
		},
	}

	data, err := json.Marshal(ratesData)
	require.NoError(t, err)

	res, err := parseAndFindCoinCapRate(data, curr1)
	require.NoError(t, err)
	require.Equal(t, amt1, res)

	res, err = parseAndFindCoinCapRate(data, curr2)
	require.NoError(t, err)
	require.Equal(t, amt2, res)

	_, err = parseAndFindCoinCapRate(data, curr3)
	require.Equal(t, err, errCurrencySymbolNotFound)
}
