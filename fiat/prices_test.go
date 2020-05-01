package fiat

import (
	"testing"
	"time"

	"github.com/lightningnetwork/lnd/lnwire"
)

// TestGetPrice tests getting price from a set of price data.
func TestGetPrice(t *testing.T) {
	now := time.Now()
	oneHourAgo := now.Add(time.Hour * -1)
	twoHoursAgo := now.Add(time.Hour * -2)

	tests := []struct {
		name          string
		prices        []*usdPrice
		request       *PriceRequest
		expectedErr   error
		expectedPrice float64
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
			prices: []*usdPrice{
				{
					timestamp: now,
					price:     10000,
				},
			},
			request: &PriceRequest{
				Value:     1,
				Timestamp: oneHourAgo,
			},
			expectedErr:   nil,
			expectedPrice: msatToUSD(10000, 1),
		},
		{
			name: "timestamp equals data point timestamp",
			prices: []*usdPrice{
				{
					timestamp: oneHourAgo,
					price:     10000,
				},
				{
					timestamp: now,
					price:     10000,
				},
			},
			request: &PriceRequest{
				Value:     2,
				Timestamp: now,
			},
			expectedErr:   nil,
			expectedPrice: msatToUSD(10000, 2),
		},
		{
			name: "timestamp after range",
			prices: []*usdPrice{
				{
					timestamp: twoHoursAgo,
					price:     20000,
				},
				{
					timestamp: oneHourAgo,
					price:     10000,
				},
			},
			request: &PriceRequest{
				Value:     3,
				Timestamp: now,
			},
			expectedErr:   nil,
			expectedPrice: msatToUSD(10000, 3),
		},
		{
			name: "timestamp between prices, aggregated",
			prices: []*usdPrice{
				{
					timestamp: twoHoursAgo,
					price:     20000,
				},
				{
					timestamp: now,
					price:     10000,
				},
			},
			request: &PriceRequest{
				Value:     3,
				Timestamp: oneHourAgo,
			},
			expectedErr:   nil,
			expectedPrice: msatToUSD((20000+10000)/2, 3),
		},
	}

	for _, test := range tests {
		test := test

		t.Run(test.name, func(t *testing.T) {
			price, err := getPrice(test.prices, test.request)
			if err != test.expectedErr {
				t.Fatalf("expected: %v, got: %v",
					test.expectedErr, err)
			}

			if price != test.expectedPrice {
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
		price        float64
		expectedFiat float64
	}{
		{
			name:         "1 sat not rounded down",
			amount:       1000,
			price:        10000,
			expectedFiat: 0.0001,
		},
		{
			name:         "1 msat rounded down",
			amount:       1,
			price:        10000,
			expectedFiat: 0,
		},
		{
			name:         "1 btc + 1 msat rounded down",
			amount:       100000000001,
			price:        10000,
			expectedFiat: 10000,
		},
	}

	for _, test := range tests {
		test := test

		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			amt := msatToUSD(test.price, test.amount)
			if amt != test.expectedFiat {
				t.Fatalf("expected: %v, got: %v",
					test.expectedFiat, amt)
			}
		})
	}
}
