package main

import (
	"context"
	"fmt"
	"os"
	"path"
	"strings"
	"time"

	"github.com/lightninglabs/faraday/frdrpc"
	"github.com/urfave/cli"
)

var onChainReportCommand = cli.Command{
	Name:     "nodereport",
	Category: "reporting",
	Usage:    "Get a report of node activity.",
	Flags: []cli.Flag{
		cli.Int64Flag{
			Name: "start_time",
			Usage: "(optional) The unix timestamp in seconds " +
				"from which the report should be generated," +
				"defaults to one week ago",
		},
		cli.Int64Flag{
			Name: "end_time",
			Usage: "(optional) The unix timestamp in seconds" +
				"until which the report should be generated." +
				"If not set, the report will be produced " +
				"until the present.",
		},
		cli.StringFlag{
			Name: "csv_path",
			Usage: "A path to write node_report.csv to. If not " +
				"set, the command will output the report. " +
				"Note that write permissions are required.",
		},
	},
	Action: queryOnChainReport,
}

func queryOnChainReport(ctx *cli.Context) error {
	client, cleanup := getClient(ctx)
	defer cleanup()

	// Set start and end times from user specified values, defaulting
	// to zero if they are not set.
	req := &frdrpc.NodeReportRequest{
		StartTime: uint64(ctx.Int64("start_time")),
		EndTime:   uint64(ctx.Int64("end_time")),
	}

	// If start time is zero, default to a week ago.
	if req.StartTime == 0 {
		weekAgo := time.Now().Add(time.Hour * 24 * 7 * -1)
		req.StartTime = uint64(weekAgo.Unix())
	}

	rpcCtx := context.Background()
	report, err := client.NodeReport(rpcCtx, req)
	if err != nil {
		return err
	}

	// If we did not request a csv, just print the response and return.
	if !ctx.IsSet("csv_path") {
		printRespJSON(report)
		return nil
	}

	csvPath := ctx.String("csv_path")
	fmt.Printf("Outputting node_report.csv to %v\n", csvPath)

	file, err := os.Create(path.Join(csvPath, "node_report.csv"))
	if err != nil {
		return err
	}

	defer func() {
		if err := file.Close(); err != nil {
			fmt.Printf("could not close file: %v\n", err)
		}
	}()

	csvStrs := []string{CSVHeaders}
	for _, report := range report.Reports {
		csvStrs = append(csvStrs, csv(report))
	}
	csvString := strings.Join(csvStrs, "\n")

	_, err = file.WriteString(csvString)
	return err
}
