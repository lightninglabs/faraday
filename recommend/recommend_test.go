package recommend

import (
	"testing"
	"time"

	"github.com/lightninglabs/terminator/dataset"
	"github.com/lightninglabs/terminator/insights"
)

// TestCloseRecommendations tests CloseRecommendations for error cases where
// the function provided to list channels fails or the config provided is
// invalid. It also has cases for calls which return not enough channels, and
// the minimum acceptable number of channels. It does not test the report
// provided, because that will be covered by further tests.
func TestCloseRecommendations(t *testing.T) {
	tests := []struct {
		name         string
		OpenChannels []*insights.Channel
		MinMonitored time.Duration
		expectedErr  error
	}{
		{
			name:         "no channels",
			OpenChannels: []*insights.Channel{},
			MinMonitored: time.Hour,
			expectedErr:  dataset.ErrTooFewValues,
		},
		{
			name:         "zero min monitored",
			OpenChannels: []*insights.Channel{},
			MinMonitored: 0,
			expectedErr:  errZeroMinMonitored,
		},
		{
			name: "enough channels",
			OpenChannels: []*insights.Channel{
				{
					ChannelPoint: "a:1",
					MonitoredFor: time.Hour,
				},
				{
					ChannelPoint: "b:2",
					MonitoredFor: time.Hour,
				},
				{
					ChannelPoint: "c:3",
					MonitoredFor: time.Hour,
				},
			},
			MinMonitored: time.Hour,
			expectedErr:  nil,
		},
	}

	for _, test := range tests {
		test := test

		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			_, err := CloseRecommendations(
				&CloseRecommendationConfig{
					ChannelInsights:  test.OpenChannels,
					StrongOutlier:    true,
					MinimumMonitored: test.MinMonitored,
				})
			if err != test.expectedErr {
				t.Fatalf("expected: %v, got: %v",
					test.expectedErr, err)
			}
		})
	}
}

// TestGetCloseRecs tests the generating of close recommendations for a set of
// channels.
func TestGetCloseRecs(t *testing.T) {

	tests := []struct {
		name           string
		channelUptimes map[string]float64
		expectedRecs   map[string]bool
		strongOutlier  bool
	}{
		{
			name: "similar values, weak outlier no recommendations",
			channelUptimes: map[string]float64{
				"a:0":  0.7,
				"a:1":  0.6,
				"a:20": 0.5,
			},
			strongOutlier: false,
			expectedRecs:  map[string]bool{},
		},
		{
			name: "similar values, strong outlier no recommendations",
			channelUptimes: map[string]float64{
				"a:0": 0.7,
				"a:1": 0.6,
				"a:2": 0.5,
			},
			strongOutlier: true,
			expectedRecs:  map[string]bool{},
		},
		{
			name: "lower outlier recommended for close",
			channelUptimes: map[string]float64{
				"a:0": 0.6,
				"a:1": 0.6,
				"a:2": 0.5,
				"a:3": 0.5,
				"a:4": 0.5,
				"a:5": 0.1,
			},
			strongOutlier: true,
			expectedRecs: map[string]bool{
				"a:5": true,
			},
		},
	}

	for _, test := range tests {
		test := test

		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			uptimeData := dataset.New(test.channelUptimes)

			recs, err := getCloseRecs(uptimeData, test.strongOutlier)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			// Run through our expected set of recommendations and check that
			// they match the set returned in the report.
			for channel, expectClose := range test.expectedRecs {
				recClose := recs[channel]
				if recClose != expectClose {
					t.Fatalf("expected close rec: %v for channel: %v,"+
						" got: %v", expectClose, channel, recClose)
				}
			}
		})
	}
}

// TestFilterChannels tests filtering of channels based on their lifetime.
func TestFilterChannels(t *testing.T) {
	openChannels := []*insights.Channel{
		{
			ChannelPoint:     "a:0",
			MonitoredFor:     time.Second * 10,
			UptimePercentage: 1,
		},
		{
			ChannelPoint:     "a:1",
			MonitoredFor:     time.Second * 100,
			UptimePercentage: 1,
		},
		{
			ChannelPoint:     "a:2",
			MonitoredFor:     time.Second * 100,
			UptimePercentage: 1,
		},
		{
			ChannelPoint:     "a:3",
			MonitoredFor:     time.Second * 100,
			UptimePercentage: 1,
		},
	}

	tests := []struct {
		name               string
		openChannels       []*insights.Channel
		minAge             time.Duration
		expectedChanPoints []string
	}{
		{
			name:               "one channel not monitored for long enough",
			openChannels:       openChannels,
			minAge:             time.Second * 15,
			expectedChanPoints: []string{"a:1", "a:2", "a:3"},
		},
		{
			name:               "all channels included",
			openChannels:       openChannels,
			minAge:             time.Second * 5,
			expectedChanPoints: []string{"a:0", "a:1", "a:2", "a:3"},
		},
	}

	for _, test := range tests {
		test := test

		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			filtered := filterChannels(test.openChannels, test.minAge)

			if len(test.expectedChanPoints) != len(filtered) {
				t.Fatalf("expected: %v channels, got: %v",
					len(test.expectedChanPoints),
					len(filtered))
			}

			for i, expected := range test.expectedChanPoints {
				if expected != filtered[i].ChannelPoint {
					t.Fatalf("expected: %v, got: %v",
						expected, filtered[i])
				}
			}
		})
	}
}
