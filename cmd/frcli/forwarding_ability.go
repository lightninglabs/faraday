package main

import (
	"context"
	"time"

	"github.com/lightninglabs/faraday/frdrpc"
	"github.com/urfave/cli"
)

// forwardingAbilityCommand is the CLI command for querying the forwarding
// ability of peer pairs over a specified time window.
var forwardingAbilityCommand = cli.Command{
	Name:     "forwardingability",
	Category: "channels",
	Usage:    "Get the forwarding ability of a peer over a given time period.",
	Flags: []cli.Flag{
		cli.Uint64Flag{
			Name:  "start_time",
			Usage: "start time for the report (unix timestamp)",
		},
		cli.Uint64Flag{
			Name:  "end_time",
			Usage: "end time for the report (unix timestamp)",
		},
		cli.Float64Flag{
			Name:  "forward_percentile",
			Usage: "the percentile of successful forward amounts to use as a threshold",
			Value: 50.0,
		},
		cli.Uint64Flag{
			Name:  "threshold_amt_sat",
			Usage: "the threshold amount in satoshis to use",
		},
	},
	Action: queryForwardingAbility,
}

// queryForwardingAbility issues a ForwardingAbility RPC request with parameters
// from the CLI context and prints the response as JSON.
func queryForwardingAbility(ctx *cli.Context) error {
	client, cleanup := getClient(ctx)
	defer cleanup()

	req := &frdrpc.ForwardingAbilityRequest{}

	if ctx.IsSet("start_time") {
		req.StartTime = ctx.Uint64("start_time")
	} else {
		req.StartTime = uint64(
			time.Now().Add(-30 * 24 * time.Hour).Unix(),
		)
	}

	if ctx.IsSet("end_time") {
		req.EndTime = ctx.Uint64("end_time")
	} else {
		req.EndTime = uint64(time.Now().Unix())
	}

	req.ForwardPercentile = float32(ctx.Float64("forward_percentile"))
	req.ThresholdAmtSat = ctx.Uint64("threshold_amt_sat")

	rpcCtx := context.Background()
	resp, err := client.ForwardingAbility(rpcCtx, req)
	if err != nil {
		return err
	}

	printRespJSON(resp)

	return nil
}
