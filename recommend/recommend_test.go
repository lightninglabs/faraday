package recommend

import (
	"errors"
	"testing"
	"time"

	"github.com/lightninglabs/terminator/dataset"
	"github.com/lightningnetwork/lnd/lnrpc"
)

// TestCloseRecommendations tests CloseRecommendations for error cases where
// the function provided to list channels fails or the config provided is
// invalid. It also has cases for calls which return not enough channels, and
// the minimum acceptable number of channels. It does not test the report
// provided, because that will be covered by further tests.
func TestCloseRecommendations(t *testing.T) {
	var openChanErr = errors.New("intentional test err")

	tests := []struct {
		name         string
		OpenChannels func() ([]*lnrpc.Channel, error)
		MinMonitored time.Duration
		expectedErr  error
	}{
		{
			name: "no channels",
			OpenChannels: func() ([]*lnrpc.Channel, error) {
				return nil, nil
			},
			MinMonitored: time.Hour,
			expectedErr:  nil,
		},
		{
			name: "open channels fails",
			OpenChannels: func() ([]*lnrpc.Channel, error) {
				return nil, openChanErr
			},
			MinMonitored: time.Hour,
			expectedErr:  openChanErr,
		},
		{
			name: "zero min monitored",
			OpenChannels: func() ([]*lnrpc.Channel, error) {
				return nil, nil
			},
			MinMonitored: 0,
			expectedErr:  errZeroMinMonitored,
		},
		{
			name: "enough channels",
			OpenChannels: func() ([]*lnrpc.Channel, error) {
				return []*lnrpc.Channel{
					{
						ChannelPoint: "a:1",
						Lifetime:     int64(time.Hour.Seconds()),
					},
					{
						ChannelPoint: "b:2",
						Lifetime:     int64(time.Hour.Seconds()),
					},
					{
						ChannelPoint: "c:3",
						Lifetime:     int64(time.Hour.Seconds()),
					},
				}, nil
			},
			MinMonitored: time.Hour,
			expectedErr:  nil,
		},
	}

	for _, test := range tests {
		test := test

		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			_, err := CloseRecommendations(&CloseRecommendationConfig{
				OpenChannels:      test.OpenChannels,
				OutlierMultiplier: 3,
				MinimumMonitored:  test.MinMonitored,
			})
			if err != test.expectedErr {
				t.Fatalf("expected: %v, got: %v", test.expectedErr, err)
			}
		})
	}
}

// TestGetCloseRecs tests the generating of close recommendations for a set of
// channels. It also contains a test case for when there are too few channels
// to calculate outliers, to test that the error is silenced and no
// recommendations are provided.
func TestGetCloseRecs(t *testing.T) {
	tests := []struct {
		name              string
		channelUptimes    map[string]float64
		expectedRecs      map[string]Recommendation
		outlierMultiplier float64
	}{
		{
			name: "not enough values, all false",
			channelUptimes: map[string]float64{
				"a:0": 0.7,
			},
			expectedRecs: map[string]Recommendation{
				"a:0": {
					Value:          0.7,
					RecommendClose: false,
				},
			},
			outlierMultiplier: 2,
		},
		{
			name: "similar values, weak outlier no recommendations",
			channelUptimes: map[string]float64{
				"a:0":  0.7,
				"a:1":  0.6,
				"a:20": 0.5,
			},
			outlierMultiplier: 1.5,
			expectedRecs: map[string]Recommendation{
				"a:0":  {Value: 0.7, RecommendClose: false},
				"a:1":  {Value: 0.6, RecommendClose: false},
				"a:20": {Value: 0.5, RecommendClose: false},
			},
		},
		{
			name: "similar values, strong outlier no recommendations",
			channelUptimes: map[string]float64{
				"a:0": 0.7,
				"a:1": 0.6,
				"a:2": 0.5,
			},
			outlierMultiplier: 3,
			expectedRecs: map[string]Recommendation{
				"a:0": {Value: 0.7, RecommendClose: false},
				"a:1": {Value: 0.6, RecommendClose: false},
				"a:2": {Value: 0.5, RecommendClose: false},
			},
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
			outlierMultiplier: 3,
			expectedRecs: map[string]Recommendation{
				"a:0": {Value: 0.6, RecommendClose: false},
				"a:1": {Value: 0.6, RecommendClose: false},
				"a:2": {Value: 0.5, RecommendClose: false},
				"a:3": {Value: 0.5, RecommendClose: false},
				"a:4": {Value: 0.5, RecommendClose: false},
				"a:5": {Value: 0.1, RecommendClose: true},
			},
		},

		{
			name: "zero multiplier replaced with default",
			channelUptimes: map[string]float64{
				"a:0": 0.6,
				"a:1": 0.6,
				"a:2": 0.5,
				"a:3": 0.5,
				"a:4": 0.5,
				"a:5": 0.1,
			},
			outlierMultiplier: 0,
			expectedRecs: map[string]Recommendation{
				"a:0": {Value: 0.6, RecommendClose: false},
				"a:1": {Value: 0.6, RecommendClose: false},
				"a:2": {Value: 0.5, RecommendClose: false},
				"a:3": {Value: 0.5, RecommendClose: false},
				"a:4": {Value: 0.5, RecommendClose: false},
				"a:5": {Value: 0.1, RecommendClose: true},
			},
		},
	}

	for _, test := range tests {
		test := test

		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			uptimeData := dataset.New(test.channelUptimes)

			recs, err := getCloseRecs(uptimeData, test.outlierMultiplier)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if len(test.expectedRecs) != len(recs) {
				t.Fatalf("expected: %v recommendations, got: %v",
					len(test.expectedRecs), len(recs))
			}

			// Run through our expected set of true recommendations
			// and check that they match the set returned in the report.
			for channel, expectClose := range test.expectedRecs {
				recClose := recs[channel]
				if recClose != expectClose {
					t.Fatalf("expected close rec: %v"+
						" for channel: %v,  got: %v",
						expectClose, channel, recClose)
				}
			}
		})
	}
}

// TestFilterChannels tests filtering of channels based on their lifetime.
func TestFilterChannels(t *testing.T) {
	openChannels := []*lnrpc.Channel{
		{
			ChannelPoint: "a:0",
			Lifetime:     10,
			Uptime:       1,
		},
		{
			ChannelPoint: "a:1",
			Lifetime:     100,
			Uptime:       1,
		},
		{
			ChannelPoint: "a:2",
			Lifetime:     100,
			Uptime:       1,
		},
		{
			ChannelPoint: "a:3",
			Lifetime:     100,
			Uptime:       1,
		},
	}

	tests := []struct {
		name               string
		openChannels       []*lnrpc.Channel
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
					len(test.expectedChanPoints), len(filtered))
			}

			for _, expected := range test.expectedChanPoints {
				if _, ok := filtered[expected]; !ok {
					t.Fatalf("expected channel: %v to be present", expected)
				}
			}
		})
	}
}
