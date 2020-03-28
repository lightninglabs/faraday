package main

import (
	"context"

	"github.com/lightninglabs/faraday/frdrpc"
	"github.com/urfave/cli"
)

var revenueReportCommand = cli.Command{
	Name:     "revenue",
	Category: "insights",
	Usage:    "Get a pairwise revenue report for a channel.",
	Flags: []cli.Flag{
		cli.StringSliceFlag{
			Name: "chan_points",
			Usage: "(optional) A set of channels to generate a " +
				"revenue report for. If not specified, " +
				"reports will be created for all channels " +
				"that forwarded payments over the period " +
				"provided. A single channel can be set " +
				"directly using --chan_points=txid:outpoint," +
				"multiple channels should be specified using" +
				"a comma separated list in braces " +
				"--chan_points={chan, chan} ",
		},
		cli.Int64Flag{
			Name: "start_time",
			Usage: "(optional) The unix timestamp in seconds " +
				"from which the report should be generated." +
				"If not set, the report will be generated " +
				"from the time of channel open.",
		},
		cli.Int64Flag{
			Name: "end_time",
			Usage: "(optional) The unix timestamp in seconds" +
				"until which the report should be generated." +
				"If not set, the report will be produced " +
				"until the present.",
		},
	},
	Action: queryRevenueReport,
}

func queryRevenueReport(ctx *cli.Context) error {
	client, cleanup := getClient(ctx)
	defer cleanup()

	// Set start and end times from user specified values, defaulting
	// to zero if they are not set.
	req := &frdrpc.RevenueReportRequest{
		StartTime: uint64(ctx.Int64("start_time")),
		EndTime:   uint64(ctx.Int64("end_time")),
	}

	if ctx.IsSet("chan_points") {
		req.ChanPoints = ctx.StringSlice("chan_points")
	}

	rpcCtx := context.Background()
	recs, err := client.RevenueReport(rpcCtx, req)
	if err != nil {
		return err
	}

	printRespJSON(recs)

	return nil
}
