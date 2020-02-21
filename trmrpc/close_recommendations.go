package trmrpc

import (
	"context"
	"time"

	"github.com/lightninglabs/terminator/insights"
	"github.com/lightninglabs/terminator/recommend"
)

// parseRequest parses a rpc close recommendation request and returns the
// close recommendation config that the request requires.
func parseRequest(ctx context.Context, cfg *Config,
	req *CloseRecommendationsRequest) *recommend.CloseRecommendationConfig {

	// Create a close recommendations config with the minimum monitored
	// value provided in the request and the default outlier multiplier.
	recConfig := &recommend.CloseRecommendationConfig{
		ChannelInsights: func() ([]*insights.ChannelInfo, error) {
			return channelInsights(ctx, cfg)
		},
		MinimumMonitored: time.Second *
			time.Duration(req.MinimumMonitored),
		OutlierMultiplier: recommend.DefaultOutlierMultiplier,
	}

	// If a non-zero outlier multiple was provided, set it on the config.
	if req.OutlierMultiplier != 0 {
		recConfig.OutlierMultiplier = float64(req.OutlierMultiplier)
	}

	threshold, ok := req.Threshold.(*CloseRecommendationsRequest_UptimeThreshold)
	if ok {
		recConfig.UptimeThreshold = float64(threshold.UptimeThreshold)
	}

	return recConfig
}

// parseResponse parses the response obtained getting a close recommendation
// and converts it to a close recommendation response.
func parseResponse(report *recommend.Report) *CloseRecommendationsResponse {
	resp := &CloseRecommendationsResponse{
		TotalChannels:      int32(report.TotalChannels),
		ConsideredChannels: int32(report.ConsideredChannels),
	}

	for chanPoint, rec := range report.OutlierRecommendations {
		resp.OutlierRecommendations = append(
			resp.OutlierRecommendations, &Recommendation{
				ChanPoint:      chanPoint,
				Value:          float32(rec.Value),
				RecommendClose: rec.RecommendClose,
			},
		)
	}

	for chanPoint, rec := range report.ThresholdRecommendations {
		resp.ThresholdRecommendations = append(
			resp.ThresholdRecommendations, &Recommendation{
				ChanPoint:      chanPoint,
				Value:          float32(rec.Value),
				RecommendClose: rec.RecommendClose,
			},
		)
	}

	return resp
}
