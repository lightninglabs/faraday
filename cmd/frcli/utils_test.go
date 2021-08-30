package main

import (
	"testing"

	"github.com/lightninglabs/faraday/frdrpc"
)

// TestFilterPrices checks that the filterPrices function correctly filters
// prices based on given start and end timestamps.
func TestFilterPrices(t *testing.T) {
	tests := []struct {
		name           string
		prices         []*frdrpc.BitcoinPrice
		startTime      int64
		endTime        int64
		expectedPrices []*frdrpc.BitcoinPrice
		expectErr      bool
	}{
		{
			name: "test that prices are sorted correctly",
			prices: []*frdrpc.BitcoinPrice{
				{PriceTimestamp: 200},
				{PriceTimestamp: 300},
				{PriceTimestamp: 100},
			},
			startTime: 100,
			endTime:   400,
			expectedPrices: []*frdrpc.BitcoinPrice{
				{PriceTimestamp: 100},
				{PriceTimestamp: 200},
				{PriceTimestamp: 300},
			},
		},
		{
			name: "error if end time is before start time",
			prices: []*frdrpc.BitcoinPrice{
				{PriceTimestamp: 100},
				{PriceTimestamp: 200},
				{PriceTimestamp: 300},
			},
			startTime: 200,
			endTime:   100,
			expectErr: true,
		},
		{
			name: "error if no timestamp before or equal to start " +
				"time is provided",
			prices: []*frdrpc.BitcoinPrice{
				{PriceTimestamp: 100},
				{PriceTimestamp: 200},
			},
			startTime: 50,
			endTime:   100,
			expectErr: true,
		},
		{
			name: "check correct filtering of prices",
			prices: []*frdrpc.BitcoinPrice{
				{PriceTimestamp: 100},
				{PriceTimestamp: 200},
				{PriceTimestamp: 300},
				{PriceTimestamp: 400},
				{PriceTimestamp: 500},
				{PriceTimestamp: 600},
			},
			startTime: 250,
			endTime:   400,
			expectedPrices: []*frdrpc.BitcoinPrice{
				{PriceTimestamp: 200},
				{PriceTimestamp: 300},
			},
		},
		{
			name: "equal start and end timestamps",
			prices: []*frdrpc.BitcoinPrice{
				{PriceTimestamp: 100},
				{PriceTimestamp: 200},
				{PriceTimestamp: 300},
			},
			startTime: 200,
			endTime:   200,
			expectedPrices: []*frdrpc.BitcoinPrice{
				{PriceTimestamp: 200},
			},
		},
	}

	for _, test := range tests {
		test := test

		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			prices, err := filterPrices(
				test.prices, test.startTime, test.endTime,
			)
			if err != nil {
				if test.expectErr {
					return
				}
				t.Fatalf("expected no error, got: %v", err)
			}

			if len(prices) != len(test.expectedPrices) {
				t.Fatalf("expected %d prices, got %d",
					len(test.expectedPrices), len(prices))
			}

			for i, p := range prices {
				if p.PriceTimestamp != test.expectedPrices[i].PriceTimestamp {
					t.Fatalf("expected timestamp "+
						"%d at index %d, got timestamp %d",
						test.expectedPrices[i].PriceTimestamp,
						i, p.PriceTimestamp)
				}
			}
		})
	}
}
