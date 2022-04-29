package frdrpcserver

import (
	"context"
	"time"

	"github.com/lightninglabs/faraday/frdrpc"
	"github.com/lightninglabs/faraday/insights"
	"github.com/lightninglabs/faraday/lndwrap"
	"github.com/lightninglabs/faraday/revenue"
)

// channelInsights gets the set of channel insights we need.
func channelInsights(ctx context.Context,
	cfg *Config) ([]*insights.ChannelInfo, error) {

	// Get revenue from a zero start time to the present to cover
	// revenue over the lifetime of all our channels.
	revenueCfg := getRevenueConfig(
		ctx, cfg, time.Unix(0, 0), time.Now(),
	)

	report, err := revenue.GetRevenueReport(revenueCfg)
	if err != nil {
		return nil, err
	}

	return insights.GetChannels(&insights.Config{
		OpenChannels: lndwrap.ListChannels(
			ctx, cfg.Lnd.Client, false,
		),
		CurrentHeight: func() (u uint32, err error) {
			info, err := cfg.Lnd.Client.GetInfo(ctx)
			if err != nil {
				return 0, err
			}

			return info.BlockHeight, nil
		},
		RevenueReport: report,
	})
}

func rpcChannelInsightsResponse(
	insights []*insights.ChannelInfo) *frdrpc.ChannelInsightsResponse {

	rpcInsights := make([]*frdrpc.ChannelInsight, 0, len(insights))

	for _, i := range insights {
		insight := &frdrpc.ChannelInsight{
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

	return &frdrpc.ChannelInsightsResponse{ChannelInsights: rpcInsights}
}
