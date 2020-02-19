package main

import (
	"context"

	"github.com/lightninglabs/terminator/trmrpc"
	"github.com/urfave/cli"
)

var channelInsightsCommand = cli.Command{
	Name:     "insights",
	Category: "insights",
	Usage: "List currently open channel with routing and " +
		"uptime information.",
	Action: queryChannelInsights,
}

func queryChannelInsights(ctx *cli.Context) error {
	client, cleanup := getClient(ctx)
	defer cleanup()

	rpcCtx := context.Background()
	resp, err := client.ChannelInsights(
		rpcCtx, &trmrpc.ChannelInsightsRequest{},
	)
	if err != nil {
		return err
	}

	type insightsResp struct {
		*trmrpc.ChannelInsight
		UptimeRatio            float64 `json:"uptime_ratio"`
		RevenuePerConfirmation float64 `json:"revenue_per_confirmation_msat"`
	}
	insights := make([]insightsResp, len(resp.ChannelInsights))
	for i, channel := range resp.ChannelInsights {
		insight := insightsResp{
			ChannelInsight: channel,
			RevenuePerConfirmation: float64(channel.FeesEarnedMsat) /
				float64(channel.Confirmations),
		}

		if channel.MonitoredSeconds != 0 {
			insight.UptimeRatio = float64(channel.UptimeSeconds) /
				float64(channel.MonitoredSeconds)
		}

		insights[i] = insight
	}

	printJSON(insights)

	return nil
}
