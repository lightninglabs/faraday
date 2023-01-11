package fiat

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestCoinGeckoApiRanges tests that we can split up time spans into ranges with
// different granularity.
func TestCoinGeckoApiRanges(t *testing.T) {
	// Freeze the current time.
	now := time.Now()

	tests := []struct {
		name  string
		start time.Time
		end   time.Time
		want  []timeRange
	}{
		{
			name:  "range in the last 90 days",
			start: now.AddDate(0, 0, -89),
			end:   now.Add(-time.Hour),
			want: []timeRange{
				{
					start: now.AddDate(0, 0, -89),
					end:   now.Add(-time.Hour),
				},
			},
		},
		{
			name:  "range before the last 90 days",
			start: now.AddDate(0, 0, -95),
			end:   now.AddDate(0, 0, -90),
			want: []timeRange{
				{
					start: now.AddDate(0, 0, -95),
					end:   now.AddDate(0, 0, -90),
				},
			},
		},
		{
			name:  "range between 95 days and yesterday",
			start: now.AddDate(0, 0, -95),
			end:   now.AddDate(0, 0, -1),
			want: []timeRange{
				{
					start: now.AddDate(0, 0, -95),
					end:   now.AddDate(0, 0, -89),
				},
				{
					start: now.AddDate(0, 0, -89),
					end:   now.AddDate(0, 0, -1),
				},
			},
		},
	}

	for _, tc := range tests {
		tc := tc

		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			c := &coinGeckoAPI{}

			apiRanges := c.apiRanges(now, tc.start, tc.end)

			require.Equal(t, tc.want, apiRanges)
		})
	}
}
