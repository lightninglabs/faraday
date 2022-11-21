package main

import (
	"context"
	"fmt"
	"time"

	"github.com/lightninglabs/faraday/frdrpc"
	"github.com/urfave/cli"
)

var (
	defaultMinMonitored      = time.Hour * 24 * 7 * 4 // four weeks in hours
	defaultOutlierMultiplier = 3

	// monitoredFlag is common to recommendation requests.
	monitoredFlag = cli.Int64Flag{
		Name: "min_monitored",
		Usage: "amount of time in seconds a channel should be monitored " +
			"for to be eligible for close",
		Value: int64(defaultMinMonitored.Seconds()),
	}

	// Flags required for threshold close recommendations.
	thresholdFlags = []cli.Flag{
		cli.Float64Flag{
			Name: "uptime",
			Usage: "Ratio of uptime to time monitored, expressed " +
				"in [0;1].",
		},
		cli.Float64Flag{
			Name: "revenue",
			Usage: "threshold revenue (in msat) per confirmation " +
				"beneath which channels will be identified " +
				"for close.",
		},
		cli.Float64Flag{
			Name: "incoming",
			Usage: "threshold incoming volume (in msat) per " +
				"confirmation beneath which channels will be " +
				"identified for close",
		},
		cli.Float64Flag{
			Name: "outgoing",
			Usage: "threshold outgoing volume (in msat) per " +
				"confirmation beneath which channels will be " +
				"identified for close",
		},
		cli.Float64Flag{
			Name: "volume",
			Usage: "threshold total volume (in msat) per " +
				"confirmation beneath which channels will be " +
				"identified for close",
		},
		monitoredFlag,
	}

	// Flags required for outlier close recommendations.
	outlierFlags = []cli.Flag{
		cli.StringFlag{
			Name: "outlier_mult",
			Usage: "(optional with outlier strategy) Number of " +
				"inter quartile ranges a channel should be " +
				"from quartiles to be considered an outlier. " +
				"Recommended values are 1.5 for aggressive " +
				"recommendations and 3 for conservative ones.",
		},
		cli.BoolFlag{
			Name: "uptime",
			Usage: "set to get recommendations based on the " +
				"channel's peer's ratio of uptime to time " +
				"monitored",
		},
		cli.BoolFlag{
			Name: "revenue",
			Usage: "get recommendations based on the " +
				"channel's revenue per confirmation",
		},
		cli.BoolFlag{
			Name: "incoming_volume",
			Usage: "get recommendations based on the " +
				"channel's incoming volume per confirmation",
		},
		cli.BoolFlag{
			Name: "outgoing_volume",
			Usage: "get recommendations based on the " +
				"channel's outgoing volume per confirmation",
		},
		cli.BoolFlag{
			Name: "volume",
			Usage: "get recommendations based on the " +
				"channel's total volume per confirmation",
		},
		monitoredFlag,
	}
)

var thresholdRecommendationCommand = cli.Command{
	Name:     "threshold",
	Category: "recommendations",
	Usage: "Get close recommendations for currently open channels " +
		"based on whether they are below a set threshold.",
	Flags:  thresholdFlags,
	Action: queryThresholdRecommendations,
}

func queryThresholdRecommendations(ctx *cli.Context) error {
	client, cleanup := getClient(ctx)
	defer cleanup()

	// Set monitored value from cli values, this value will always be
	// non-zero because the flag has a default.
	req := &frdrpc.ThresholdRecommendationsRequest{
		RecRequest: &frdrpc.CloseRecommendationRequest{
			MinimumMonitored: ctx.Int64("min_monitored"),
		},
	}

	// Set threshold and metric based on uptime/revenue flags.
	switch {
	case ctx.IsSet("uptime"):
		req.ThresholdValue = float32(ctx.Float64("uptime"))
		req.RecRequest.Metric = frdrpc.CloseRecommendationRequest_UPTIME

	case ctx.IsSet("revenue"):
		req.ThresholdValue = float32(ctx.Float64("revenue"))
		req.RecRequest.Metric = frdrpc.CloseRecommendationRequest_REVENUE

	case ctx.IsSet("incoming"):
		req.ThresholdValue = float32(ctx.Float64("incoming"))
		req.RecRequest.Metric = frdrpc.CloseRecommendationRequest_INCOMING_VOLUME

	case ctx.IsSet("outgoing"):
		req.ThresholdValue = float32(ctx.Float64("outgoing"))
		req.RecRequest.Metric = frdrpc.CloseRecommendationRequest_OUTGOING_VOLUME

	case ctx.IsSet("volume"):
		req.ThresholdValue = float32(ctx.Float64("volume"))
		req.RecRequest.Metric = frdrpc.CloseRecommendationRequest_TOTAL_VOLUME

	default:
		return fmt.Errorf("threshold required")
	}

	rpcCtx := context.Background()
	recs, err := client.ThresholdRecommendations(rpcCtx, req)
	if err != nil {
		return err
	}

	printRespJSON(recs)

	return nil
}

var outlierRecommendationCommand = cli.Command{
	Name:     "outliers",
	Category: "recommendations",
	Usage: "Get close recommendations for currently open channels " +
		"based on whether it is an outlier.",
	Flags:  outlierFlags,
	Action: queryOutlierRecommendations,
}

func queryOutlierRecommendations(ctx *cli.Context) error {
	client, cleanup := getClient(ctx)
	defer cleanup()

	// Set monitored value from cli and default outlier multiplier. The
	// outlier multiplier will be overwritten if the user provided it, and
	// the monitored value will always be non-zero because the flag has a
	// default value.
	req := &frdrpc.OutlierRecommendationsRequest{
		RecRequest: &frdrpc.CloseRecommendationRequest{
			MinimumMonitored: ctx.Int64("min_monitored"),
		},
		OutlierMultiplier: float32(defaultOutlierMultiplier),
	}

	// If an a custom outlier multiple was set, use it.
	if ctx.IsSet("outlier_mult") {
		req.OutlierMultiplier = float32(ctx.Float64("outlier_mult"))
	}

	// Set metric based on uptime or revenue flags.
	switch {
	case ctx.IsSet("uptime"):
		req.RecRequest.Metric = frdrpc.CloseRecommendationRequest_UPTIME

	case ctx.IsSet("revenue"):
		req.RecRequest.Metric = frdrpc.CloseRecommendationRequest_REVENUE

	case ctx.IsSet("incoming_volume"):
		req.RecRequest.Metric = frdrpc.CloseRecommendationRequest_INCOMING_VOLUME

	case ctx.IsSet("outgoing_volume"):
		req.RecRequest.Metric = frdrpc.CloseRecommendationRequest_OUTGOING_VOLUME

	case ctx.IsSet("volume"):
		req.RecRequest.Metric = frdrpc.CloseRecommendationRequest_TOTAL_VOLUME

	default:
		return fmt.Errorf("uptime, revenue or volume related flag " +
			"required")
	}

	rpcCtx := context.Background()
	recs, err := client.OutlierRecommendations(rpcCtx, req)
	if err != nil {
		return err
	}

	printRespJSON(recs)

	return nil
}
