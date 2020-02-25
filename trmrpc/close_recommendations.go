package trmrpc

import (
	"context"
	"sort"
	"time"

	"github.com/lightninglabs/terminator/insights"
	"github.com/lightninglabs/terminator/recommend"
)

// parseRecommendationRequest parses a close recommendation request and
// returns the config required to get recommendations.
func parseRecommendationRequest(ctx context.Context, cfg *Config,
	req *CloseRecommendationRequest) *recommend.CloseRecommendationConfig {

	// Create a close recommendations config with the minimum monitored
	// value provided in the request and the default outlier multiplier.
	return &recommend.CloseRecommendationConfig{
		ChannelInsights: func() ([]*insights.ChannelInfo, error) {
			return channelInsights(ctx, cfg)
		},
		Metric: recommend.UptimeMetric,
		MinimumMonitored: time.Second *
			time.Duration(req.MinimumMonitored),
	}
}

// parseOutlierRequest parses a rpc outlier recommendation request and returns
// the close recommendation config and multiplier required.
func parseOutlierRequest(ctx context.Context, cfg *Config,
	req *OutlierRecommendationsRequest) (
	*recommend.CloseRecommendationConfig, float64) {

	multiplier := recommend.DefaultOutlierMultiplier
	if req.OutlierMultiplier != 0 {
		multiplier = float64(req.OutlierMultiplier)
	}

	return parseRecommendationRequest(ctx, cfg, req.RecRequest), multiplier
}

// parseThresholdRequest parses a rpc threshold recommendation request and
// returns the close recommendation config and threshold required. The above
// threshold boolean is inverted to allow for
// a default that returns values below a threshold.
func parseThresholdRequest(ctx context.Context, cfg *Config,
	req *ThresholdRecommendationsRequest) (
	*recommend.CloseRecommendationConfig, float64) {

	return parseRecommendationRequest(ctx, cfg, req.RecRequest),
		float64(req.ThresholdValue)
}

// rpcResponse parses the response obtained getting a close recommendation
// and converts it to a close recommendation response.
func rpcResponse(report *recommend.Report) *CloseRecommendationsResponse {
	resp := &CloseRecommendationsResponse{
		TotalChannels:      int32(report.TotalChannels),
		ConsideredChannels: int32(report.ConsideredChannels),
	}

	for chanPoint, rec := range report.Recommendations {
		resp.Recommendations = append(
			resp.Recommendations, &Recommendation{
				ChanPoint:      chanPoint,
				Value:          float32(rec.Value),
				RecommendClose: rec.RecommendClose,
			},
		)
	}

	// Sort the recommendations returned by value.
	sort.SliceStable(resp.Recommendations, func(i, j int) bool {
		return resp.Recommendations[i].Value <
			resp.Recommendations[j].Value
	})

	return resp
}
