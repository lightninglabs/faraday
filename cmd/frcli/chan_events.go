package main

import (
	"context"
	"fmt"

	"github.com/lightninglabs/faraday/frdrpc"
	"github.com/urfave/cli"
)

var chanEventsCommand = cli.Command{
	Name:     "chanevents",
	Category: "reporting",
	Usage:    "Get a report of channel events.",
	Description: `
	Get a report for a channel which provides a detailed account of its
	lifecycle events. The server caps each response; if has_more is true,
	fetch the next page by re-running with --last_id set to the previous
	response's last_id. Stop when has_more is false. --start_time and
	--end_time are independent filters and do not need to advance between
	paginated calls.`,
	ArgsUsage: "funding_txid [output_index]",
	Flags: []cli.Flag{
		cli.StringFlag{
			Name:  "funding_txid",
			Usage: "the txid of the channel's funding transaction",
		},
		cli.IntFlag{
			Name: "output_index",
			Usage: "the output index for the funding output of " +
				"the funding transaction",
		},
		cli.Int64Flag{
			Name:  "start_time",
			Usage: "start time of the query range as a unix timestamp",
		},
		cli.Int64Flag{
			Name: "end_time",
			Usage: "end time of the query range as a unix " +
				"timestamp; zero defaults to the server's " +
				"current time",
		},
		cli.UintFlag{
			Name: "max_events",
			Usage: "maximum number of events to return; zero " +
				"uses the server default (capped server-side)",
		},
		cli.Int64Flag{
			Name: "last_id",
			Usage: "pagination cursor; pass the previous " +
				"response's last_id to continue, or zero " +
				"for the first page",
		},
	},
	Action: queryChanEvents,
}

func queryChanEvents(ctx *cli.Context) error {
	client, cleanup := getClient(ctx)
	defer cleanup()

	// Show command help if the channel point was not provided.
	if ctx.NArg() == 0 && ctx.String("funding_txid") == "" {
		return cli.ShowCommandHelp(ctx, "chanevents")
	}

	outpoint, err := parseChannelPoint(ctx)
	if err != nil {
		return err
	}

	startTime := ctx.Int64("start_time")
	endTime := ctx.Int64("end_time")
	if startTime < 0 || endTime < 0 {
		return fmt.Errorf("start_time and end_time must be >= 0")
	}
	if endTime != 0 && startTime > endTime {
		return fmt.Errorf("start_time must be <= end_time")
	}

	req := &frdrpc.ChannelEventsRequest{
		ChanPoint: outpoint.String(),
		StartTime: startTime,
		EndTime:   endTime,
		MaxEvents: uint32(ctx.Uint("max_events")),
		LastId:    ctx.Int64("last_id"),
	}

	rpcCtx := context.Background()
	report, err := client.GetChannelEvents(rpcCtx, req)
	if err != nil {
		return err
	}

	printRespJSON(report)
	return nil
}
