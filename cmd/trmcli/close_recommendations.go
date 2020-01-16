package main

import (
	"context"
	"time"

	"github.com/lightninglabs/terminator/trmrpc"
	"github.com/urfave/cli"
)

var (
	defaultMinMonitored      = time.Hour * 24 * 7 * 4 // four weeks in hours
	defaultOutlierMultiplier = 3
)

var closeRecommendationCommand = cli.Command{
	Name:     "closerecs",
	Category: "channel",
	Usage:    "Get close recommendations for currently open channels.",
	Flags: []cli.Flag{
		cli.Int64Flag{
			Name: "min_monitored",
			Usage: "amount of time in seconds a channel should " +
				"be monitored for to be eligible for close",
			Value: int64(defaultMinMonitored.Seconds()),
		},
		cli.StringFlag{
			Name: "outlier_mult",
			Usage: "(optional with outlier strategy) Number of " +
				"inter quartile ranges a channel should be " +
				"from quartiles to be considered an outlier. " +
				"Recommended values are 1.5 for aggressive " +
				"recommendations and 3 for conservative ones.",
		},
		cli.StringFlag{
			Name: "uptime_threshold",
			Usage: "(optional) Uptime percentage threshold " +
				"underneath which a channel will be recommended " +
				"for close.",
		},
	},
	Action: queryCloseRecommendations,
}

func queryCloseRecommendations(ctx *cli.Context) error {
	client, cleanup := getClient(ctx)
	defer cleanup()

	// Set monitored value from cli and default outlier multiplier. The
	// outlier multiplier will be overwritten if the user provided it.
	req := &trmrpc.CloseRecommendationsRequest{
		MinimumMonitored:  ctx.Int64("min_monitored"),
		OutlierMultiplier: float32(defaultOutlierMultiplier),
	}

	// If an a custom outlier multiple was set, use it.
	if ctx.IsSet("outlier_mult") {
		req.OutlierMultiplier = float32(ctx.Float64("outlier_mult"))
	}

	// If an uptime threshold was set, use it.
	if ctx.IsSet("uptime_threshold") {
		uptimeThreshold := float32(ctx.Float64("uptime_threshold"))
		req.Threshold =
			&trmrpc.CloseRecommendationsRequest_UptimeThreshold{
				UptimeThreshold: uptimeThreshold,
			}
	}

	rpcCtx := context.Background()
	recs, err := client.CloseRecommendations(rpcCtx, req)
	if err != nil {
		return err
	}

	printRespJSON(recs)

	return nil
}
