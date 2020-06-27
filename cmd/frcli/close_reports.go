package main

import (
	"context"

	"github.com/lightninglabs/faraday/frdrpc"
	"github.com/urfave/cli"
)

var closeReportCommand = cli.Command{
	Name:     "closereport",
	Category: "reporting",
	Usage:    "Get a report for a specific channel close.",
	Description: `
	Get a close report for a closed channel which provides a detailed 
	account of the on chain fees we paid to resolve the channel on chain.`,
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
	},
	Action: queryCloseReport,
}

func queryCloseReport(ctx *cli.Context) error {
	client, cleanup := getClient(ctx)
	defer cleanup()

	// Show command help if the channel point was not provided.
	if ctx.NArg() == 0 {
		return cli.ShowCommandHelp(ctx, "closereport")
	}

	outpoint, err := parseChannelPoint(ctx)
	if err != nil {
		return err
	}

	req := &frdrpc.CloseReportRequest{
		ChannelPoint: outpoint.String(),
	}

	rpcCtx := context.Background()
	report, err := client.CloseReport(rpcCtx, req)
	if err != nil {
		return err
	}

	printRespJSON(report)
	return nil
}
