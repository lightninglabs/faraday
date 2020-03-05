package main

import (
	"context"

	"github.com/lightninglabs/governator/gvnrpc"
	"github.com/urfave/cli"
)

var channelInsightsCommand = cli.Command{
	Name:     "insights",
	Category: "insights",
	Usage: "List currently open channel with routing and " +
		"uptime information.",
	Action: queryChannelInsights,
}

// insightsResp is used to display additional information that is calculated
// from the channel insight in the cli response.
type insightsResp struct {
	*gvnrpc.ChannelInsight
	UptimeRatio                   float64 `json:"uptime_ratio"`
	RevenuePerConfirmation        float64 `json:"revenue_per_conf_msat"`
	VolumePerConfirmation         float64 `json:"volume_per_conf_msat"`
	IncomingVolumePerConfirmation float64 `json:"incoming_vol_per_conf_msat"`
	OutgoingVolumePerConfirmation float64 `json:"outgoing_vol_per_conf_msat"`
}

func queryChannelInsights(ctx *cli.Context) error {
	client, cleanup := getClient(ctx)
	defer cleanup()

	rpcCtx := context.Background()
	resp, err := client.ChannelInsights(
		rpcCtx, &gvnrpc.ChannelInsightsRequest{},
	)
	if err != nil {
		return err
	}

	insights := make([]insightsResp, len(resp.ChannelInsights))
	for i, channel := range resp.ChannelInsights {
		confirmations := float64(channel.Confirmations)

		insight := insightsResp{
			ChannelInsight: channel,
			RevenuePerConfirmation: float64(channel.FeesEarnedMsat) /
				confirmations,
		}

		if channel.MonitoredSeconds != 0 {
			insight.UptimeRatio = float64(channel.UptimeSeconds) /
				float64(channel.MonitoredSeconds)
		}

		// Calculate incoming, outgoing and total volume per
		// confirmation.
		insight.IncomingVolumePerConfirmation =
			float64(channel.VolumeIncomingMsat) /
				confirmations

		insight.OutgoingVolumePerConfirmation =
			float64(channel.VolumeOutgoingMsat) /
				confirmations

		insight.VolumePerConfirmation =
			insight.IncomingVolumePerConfirmation +
				confirmations

		insights[i] = insight
	}

	printJSON(insights)

	return nil
}
