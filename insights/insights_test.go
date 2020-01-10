package insights

import (
	"reflect"
	"testing"
	"time"

	"github.com/lightninglabs/terminator/revenue"
	"github.com/lightningnetwork/lnd/lnrpc"
	"github.com/lightningnetwork/lnd/lnwire"
)

// TestGetChannels tests gathering of channel insights from a set of lnrpc
// channels and a revenue report.
func TestGetChannels(t *testing.T) {
	// Make a short channel ID for a channel at height 1000.
	channelHeight1000 := lnwire.ShortChannelID{
		BlockHeight: 1000,
		TxIndex:     1,
		TxPosition:  3,
	}

	// Create an empty revenue report.
	noRevenue := &revenue.Report{
		ChannelPairs: map[string]map[string]revenue.Revenue{},
	}

	// report is a revenue report with the 1000 block channel in it.
	report := &revenue.Report{
		ChannelPairs: map[string]map[string]revenue.Revenue{
			"a:1": {
				"b:1": revenue.Revenue{
					AmountOutgoing: 25,
					AmountIncoming: 10,
					FeesIncoming:   10,
					FeesOutgoing:   10,
				},
				"b:2": revenue.Revenue{
					AmountOutgoing: 0,
					AmountIncoming: 10,
					FeesIncoming:   20,
					FeesOutgoing:   0,
				},
			},
		},
	}

	hourInSeconds := int64(time.Hour.Seconds())

	tests := []struct {
		name             string
		channels         []*lnrpc.Channel
		currentHeight    uint32
		revenue          *revenue.Report
		expectedInsights []*Channel
	}{
		{
			name:             "no channels",
			channels:         []*lnrpc.Channel{},
			currentHeight:    2000,
			revenue:          noRevenue,
			expectedInsights: []*Channel{},
		}, {
			name: "non-zero lifetime",
			channels: []*lnrpc.Channel{
				{
					ChannelPoint: "a:1",
					Lifetime:     hourInSeconds,
					Uptime:       hourInSeconds / 2,
					ChanId:       channelHeight1000.ToUint64(),
				},
			},
			currentHeight: 2000,
			revenue:       noRevenue,
			expectedInsights: []*Channel{
				{
					ChannelPoint:     "a:1",
					MonitoredFor:     time.Hour,
					UptimePercentage: 0.5,
					BlocksOpen:       1000,
					Private:          false,
				},
			},
		},
		{
			name: "zero lifetime ok",
			channels: []*lnrpc.Channel{
				{
					ChannelPoint: "a:1",
					Lifetime:     0,
					Uptime:       0,
					ChanId:       channelHeight1000.ToUint64(),
				},
			},
			currentHeight: 2000,
			revenue:       noRevenue,
			expectedInsights: []*Channel{
				{
					ChannelPoint:     "a:1",
					MonitoredFor:     0,
					UptimePercentage: 0,
					BlocksOpen:       1000,
					Private:          false,
				},
			},
		},
		{
			name: "revenue over 2 blocks",
			channels: []*lnrpc.Channel{
				{
					ChannelPoint: "a:1",
					Lifetime:     hourInSeconds,
					Uptime:       hourInSeconds / 2,
					ChanId:       channelHeight1000.ToUint64(),
				},
			},
			currentHeight: 1002,
			revenue:       report,
			expectedInsights: []*Channel{
				{
					ChannelPoint:     "a:1",
					MonitoredFor:     time.Hour,
					UptimePercentage: 0.5,
					BlocksOpen:       2,
					VolumeIncoming:   20,
					VolumeOutgoing:   25,
					FeesEarned:       20,
					RevenuePerBlock:  10,
					Private:          false,
				},
			},
		},
	}

	for _, test := range tests {
		test := test

		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			insights, err := GetChannels(&Config{
				OpenChannels: test.channels,
				CurrentHeight: func() (u uint32, e error) {
					return test.currentHeight, nil
				},
				RevenueReport: test.revenue,
			})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if len(test.expectedInsights) != len(insights) {
				t.Fatalf("expected: %v insights, got: %v",
					len(test.expectedInsights), len(insights))
			}

			for i, insight := range test.expectedInsights {
				if !reflect.DeepEqual(insights[i], insight) {
					t.Fatalf("expected: %v, got: %v",
						insight, insights[i])
				}
			}

		})

	}
}
