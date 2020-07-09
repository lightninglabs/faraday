package fiat

import (
	"testing"
	"time"

	"github.com/lightningnetwork/lnd/lnwire"
	"github.com/shopspring/decimal"
)

// TestGetPrice tests getting price from a set of price data.
func TestGetPrice(t *testing.T) {
	now := time.Now()
	oneHourAgo := now.Add(time.Hour * -1)
	twoHoursAgo := now.Add(time.Hour * -2)

	price10K := decimal.New(10000, 1)
	price20K := decimal.New(20000, 1)
	avg := decimal.Avg(price10K, price20K)

	tests := []struct {
		name          string
		prices        []*USDPrice
		request       *PriceRequest
		expectedErr   error
		expectedPrice decimal.Decimal
	}{
		{
			name:   "no prices",
			prices: nil,
			request: &PriceRequest{
				Value:     1,
				Timestamp: oneHourAgo,
			},
			expectedErr: errNoPrices,
		},
		{
			name: "timestamp before range",
			prices: []*USDPrice{
				{
					Timestamp: now,
					Price:     price10K,
				},
			},
			request: &PriceRequest{
				Value:     1,
				Timestamp: oneHourAgo,
			},
			expectedErr:   nil,
			expectedPrice: MsatToUSD(price10K, 1),
		},
		{
			name: "timestamp equals data point timestamp",
			prices: []*USDPrice{
				{
					Timestamp: oneHourAgo,
					Price:     price10K,
				},
				{
					Timestamp: now,
					Price:     price10K,
				},
			},
			request: &PriceRequest{
				Value:     2,
				Timestamp: now,
			},
			expectedErr:   nil,
			expectedPrice: MsatToUSD(price10K, 2),
		},
		{
			name: "timestamp after range",
			prices: []*USDPrice{
				{
					Timestamp: twoHoursAgo,
					Price:     price10K,
				},
				{
					Timestamp: oneHourAgo,
					Price:     price10K,
				},
			},
			request: &PriceRequest{
				Value:     3,
				Timestamp: now,
			},
			expectedErr:   nil,
			expectedPrice: MsatToUSD(price10K, 3),
		},
		{
			name: "timestamp between prices, aggregated",
			prices: []*USDPrice{
				{
					Timestamp: twoHoursAgo,
					Price:     price20K,
				},
				{
					Timestamp: now,
					Price:     price10K,
				},
			},
			request: &PriceRequest{
				Value:     3,
				Timestamp: oneHourAgo,
			},
			expectedErr:   nil,
			expectedPrice: MsatToUSD(avg, 3),
		},
	}

	for _, test := range tests {
		test := test

		t.Run(test.name, func(t *testing.T) {
			price, err := GetPrice(test.prices, test.request)
			if err != test.expectedErr {
				t.Fatalf("expected: %v, got: %v",
					test.expectedErr, err)
			}

			if !price.Equal(test.expectedPrice) {
				t.Fatalf("expected: %v, got: %v",
					test.expectedPrice, price)
			}
		})
	}
}

// TestGetQueryableDuration tests getting min/max from a set of timestamps.
func TestGetQueryableDuration(t *testing.T) {
	now := time.Now()
	yesterday := now.Add(time.Hour * -24)

	tests := []struct {
		name        string
		requests    []*PriceRequest
		expectStart time.Time
		expectEnd   time.Time
	}{
		{
			name: "single ts",
			requests: []*PriceRequest{
				{
					Timestamp: now,
				},
			},
			expectStart: now,
			expectEnd:   now,
		},
		{
			name: "different ts",
			requests: []*PriceRequest{
				{
					Timestamp: now,
				},
				{
					Timestamp: yesterday,
				},
			},
			expectStart: yesterday,
			expectEnd:   now,
		},
		{
			name: "duplicate ts",
			requests: []*PriceRequest{
				{
					Timestamp: now,
				},
				{
					Timestamp: now,
				},
				{
					Timestamp: yesterday,
				},
			},
			expectStart: yesterday,
			expectEnd:   now,
		},
	}

	for _, test := range tests {
		test := test

		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			start, end := getQueryableDuration(test.requests)
			if !start.Equal(test.expectStart) {
				t.Fatalf("expected: %v, got: %v",
					test.expectStart, start)
			}

			if !end.Equal(test.expectEnd) {
				t.Fatalf("expected: %v, got: %v",
					test.expectEnd, end)
			}
		})
	}
}

// TestMSatToUsd tests conversion of msat to usd. This
func TestMSatToUsd(t *testing.T) {
	tests := []struct {
		name         string
		amount       lnwire.MilliSatoshi
		price        decimal.Decimal
		expectedFiat decimal.Decimal
	}{
		{
			name:         "1 sat not rounded down",
			amount:       1000,
			price:        decimal.NewFromInt(10000),
			expectedFiat: decimal.NewFromFloat(0.0001),
		},
		{
			name:         "1 msat not rounded down",
			amount:       1,
			price:        decimal.NewFromInt(10000),
			expectedFiat: decimal.NewFromFloat(0.0000001),
		},
		{
			name:         "1 btc + 1 msat not rounded down",
			amount:       100000000001,
			price:        decimal.NewFromInt(10000),
			expectedFiat: decimal.NewFromFloat(10000.0000001),
		},
	}

	for _, test := range tests {
		test := test

		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			amt := MsatToUSD(test.price, test.amount)
			if !amt.Equals(test.expectedFiat) {
				t.Fatalf("expected: %v, got: %v",
					test.expectedFiat, amt)
			}
		})
	}
}
