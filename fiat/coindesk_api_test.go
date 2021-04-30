package fiat

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestParseCoinDeskData adds a test which checks that we appropriately parse
// the price and timestamp data returned by coindesk's api.
func TestParseCoinDeskData(t *testing.T) {
	tests := []struct {
		name  string
		price float64
		date  string
	}{
		{
			name:  "no decimal",
			price: 10000,
			date:  "2021-04-16",
		},
		{
			name:  "with decimal",
			price: 10.1,
			date:  "2021-04-17",
		},
	}

	for _, test := range tests {
		test := test

		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			price := decimal.NewFromFloat(test.price)

			timestamp, err := time.Parse(coinDeskTimeFormat, test.date)
			assert.NoError(t, err)

			// Create the struct we expect to receive from coindesk and marshal it
			// into bytes.
			resps := coinDeskResponse{
				Data: map[string]float64{
					test.date: test.price,
				},
			}

			bytes, err := json.Marshal(resps)
			assert.NoError(t, err)

			prices, err := parseCoinDeskData(bytes)
			require.NoError(t, err)

			expectedPrices := []*USDPrice{
				{
					Price:     price,
					Timestamp: timestamp,
				},
			}

			require.True(t, expectedPrices[0].Price.Equal(prices[0].Price))
			require.True(t, expectedPrices[0].Timestamp.Equal(prices[0].Timestamp))
		})
	}
}
