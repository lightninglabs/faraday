package gvnrpc

import (
	"context"
	"time"

	"github.com/lightninglabs/governator/insights"
	"github.com/lightninglabs/governator/revenue"
	"github.com/lightningnetwork/lnd/lnrpc"
)

// channelInsights gets the set of channel insights we need.
func channelInsights(ctx context.Context,
	cfg *Config) ([]*insights.ChannelInfo, error) {

	// Get revenue from a zero start time to the present to cover
	// revenue over the lifetime of all our channels.
	revenueCfg := getRevenueConfig(
		ctx, cfg, 0, uint64(time.Now().Unix()),
	)

	report, err := revenue.GetRevenueReport(revenueCfg)
	if err != nil {
		return nil, err
	}

	return insights.GetChannels(&insights.Config{
		OpenChannels: cfg.wrapListChannels(ctx, false),
		CurrentHeight: func() (u uint32, err error) {
			info, err := cfg.LightningClient.GetInfo(
				ctx, &lnrpc.GetInfoRequest{},
			)
			if err != nil {
				return 0, err
			}

			return info.BlockHeight, nil
		},
		RevenueReport: report,
	})
}

func rpcChannelInsightsResponse(
	insights []*insights.ChannelInfo) *ChannelInsightsResponse {

	rpcInsights := make([]*ChannelInsight, 0, len(insights))

	for _, i := range insights {
		insight := &ChannelInsight{
			ChanPoint:          i.ChannelPoint,
			MonitoredSeconds:   uint64(i.MonitoredFor.Seconds()),
			UptimeSeconds:      uint64(i.Uptime.Seconds()),
			VolumeIncomingMsat: int64(i.VolumeIncoming),
			VolumeOutgoingMsat: int64(i.VolumeOutgoing),
			FeesEarnedMsat:     int64(i.FeesEarned),
			Confirmations:      i.Confirmations,
			Private:            i.Private,
		}

		rpcInsights = append(rpcInsights, insight)
	}

	return &ChannelInsightsResponse{ChannelInsights: rpcInsights}
}
