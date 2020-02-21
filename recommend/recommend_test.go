package recommend

import (
	"errors"
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
	var openChanErr = errors.New("intentional test err")

	tests := []struct {
		name         string
		upperOutlier bool
		ChanInsights func() ([]*insights.ChannelInfo, error)
		MinMonitored time.Duration
		expectedErr  error
	}{
		{
			name:         "no channels",
			upperOutlier: false,
			ChanInsights: func() ([]*insights.ChannelInfo, error) {
				return nil, nil
			},
			MinMonitored: time.Hour,
			expectedErr:  nil,
		},
		{
			name:         "channel insights fails",
			upperOutlier: false,
			ChanInsights: func() ([]*insights.ChannelInfo, error) {
				return nil, openChanErr
			},
			MinMonitored: time.Hour,
			expectedErr:  openChanErr,
		},
		{
			name:         "zero min monitored",
			upperOutlier: false,
			ChanInsights: func() ([]*insights.ChannelInfo, error) {
				return nil, nil
			},
			MinMonitored: 0,
			expectedErr:  errZeroMinMonitored,
		},
		{
			name:         "enough channels",
			upperOutlier: false,
			ChanInsights: func() ([]*insights.ChannelInfo, error) {
				return []*insights.ChannelInfo{
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

			recFunc := func(data dataset.Dataset) (
				m map[string]Recommendation, err error) {

				return getOutlierRecs(
					data, DefaultOutlierMultiplier,
					test.upperOutlier,
				)
			}

			_, err := closeRecommendations(
				&CloseRecommendationConfig{
					ChannelInsights:  test.ChanInsights,
					MinimumMonitored: test.MinMonitored,
				},
				recFunc,
			)

			if err != test.expectedErr {
				t.Fatalf("expected: %v, got: %v",
					test.expectedErr, err)
			}
		})
	}
}

// TestOutlierRecommendations tests the generating of close recommendations
// for a set of channels based on whether they are outliers. It also contains
// a test case for when there are too few channels to calculate outliers, to
// test that the error is silenced and no recommendations are provided.
func TestOutlierRecommendations(t *testing.T) {
	tests := []struct {
		name              string
		upperOutlier      bool
		channelUptimes    map[string]float64
		expectedRecs      map[string]Recommendation
		outlierMultiplier float64
	}{
		{
			name:         "not enough values, all false",
			upperOutlier: false,
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
			name: "similar values, weak outlier no " +
				"recommendations",
			upperOutlier: false,
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
			name: "similar values, strong outlier no " +
				"make linrecommendations",
			upperOutlier: false,
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
			name:         "lower outlier recommended for close",
			upperOutlier: false,
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
			name:         "upper outlier recommended for close",
			upperOutlier: true,
			channelUptimes: map[string]float64{
				"a:0": 0.9,
				"a:1": 0.2,
				"a:2": 0.2,
				"a:3": 0.2,
				"a:4": 0.1,
				"a:5": 0.1,
			},
			outlierMultiplier: 3,
			expectedRecs: map[string]Recommendation{
				"a:0": {Value: 0.9, RecommendClose: true},
				"a:1": {Value: 0.2, RecommendClose: false},
				"a:2": {Value: 0.2, RecommendClose: false},
				"a:3": {Value: 0.2, RecommendClose: false},
				"a:4": {Value: 0.1, RecommendClose: false},
				"a:5": {Value: 0.1, RecommendClose: false},
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

			recs, err := getOutlierRecs(
				uptimeData, test.outlierMultiplier,
				test.upperOutlier,
			)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if len(test.expectedRecs) != len(recs) {
				t.Fatalf("expected: %v recommendations, "+
					"got: %v", len(test.expectedRecs),
					len(recs))
			}

			// Run through our expected set of true recommendations
			// and check that they match the set returned in the
			// report.
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

// TestThresholdRecommendations tests getting of recommendations above and
// below a threshold.
func TestThresholdRecommendations(t *testing.T) {
	tests := []struct {
		name           string
		belowThreshold bool
		threshold      float64
		values         map[string]float64
		expectedRecs   map[string]Recommendation
	}{
		{
			name:           "nothing below threshold",
			belowThreshold: true,
			threshold:      0.4,
			values: map[string]float64{
				"a:0": 0.8,
				"a:1": 0.6,
			},
			expectedRecs: map[string]Recommendation{
				"a:0": {Value: 0.8, RecommendClose: false},
				"a:1": {Value: 0.6, RecommendClose: false},
			},
		},
		{
			name:           "one below threshold",
			belowThreshold: true,
			threshold:      0.7,
			values: map[string]float64{
				"a:0": 0.8,
				"a:1": 0.6,
			},
			expectedRecs: map[string]Recommendation{
				"a:0": {Value: 0.8, RecommendClose: false},
				"a:1": {Value: 0.6, RecommendClose: true},
			},
		},
		{
			name:           "one above threshold",
			belowThreshold: false,
			threshold:      0.7,
			values: map[string]float64{
				"a:0": 0.8,
				"a:1": 0.6,
			},
			expectedRecs: map[string]Recommendation{
				"a:0": {Value: 0.8, RecommendClose: true},
				"a:1": {Value: 0.6, RecommendClose: false},
			},
		},
	}

	for _, test := range tests {
		test := test

		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			recs := getThresholdRecs(
				dataset.New(test.values), test.threshold,
				test.belowThreshold,
			)

			if len(test.expectedRecs) != len(recs) {
				t.Fatalf("expected: %v recommendations, "+
					"got: %v", len(test.expectedRecs),
					len(recs))
			}

			// Run through our expected set of true recommendations
			// and check that they match the set returned in the
			// report.
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
	chanInsights := []*insights.ChannelInfo{
		{
			ChannelPoint: "a:0",
			MonitoredFor: 10,
			Uptime:       1,
		},
		{
			ChannelPoint: "a:1",
			MonitoredFor: 100,
			Uptime:       1,
		},
		{
			ChannelPoint: "a:2",
			MonitoredFor: 100,
			Uptime:       1,
		},
		{
			ChannelPoint: "a:3",
			MonitoredFor: 100,
			Uptime:       1,
		},
	}

	tests := []struct {
		name             string
		chanInsights     []*insights.ChannelInfo
		minAge           time.Duration
		expectedChannels map[string]bool
	}{
		{
			name:         "one filtered - monitored time",
			chanInsights: chanInsights,
			minAge:       15,
			expectedChannels: map[string]bool{
				"a:1": true,
				"a:2": true,
				"a:3": true,
			},
		},
		{
			name:         "all channels included",
			chanInsights: chanInsights,
			minAge:       5,
			expectedChannels: map[string]bool{
				"a:0": true,
				"a:1": true,
				"a:2": true,
				"a:3": true,
			},
		},
	}

	for _, test := range tests {
		test := test

		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			filtered := filterChannels(test.chanInsights, test.minAge)

			if len(test.expectedChannels) != len(filtered) {
				t.Fatalf("expected: %v channels, got: %v",
					len(test.expectedChannels),
					len(filtered))
			}

			for _, filteredChan := range filtered {
				_, ok := test.expectedChannels[filteredChan.ChannelPoint]
				if !ok {
					t.Fatalf("unexpected channel: %v",
						filteredChan)
				}
			}
		})
	}
}
