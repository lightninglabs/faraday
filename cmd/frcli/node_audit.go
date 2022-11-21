package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path"
	"strings"
	"time"

	"github.com/lightninglabs/faraday/frdrpc"
	"github.com/urfave/cli"
)

var onChainReportCommand = cli.Command{
	Name:     "audit",
	Category: "reporting",
	Usage:    "Get a report of node activity.",
	Description: `
	Create a report containing all of your node's activity over 
	the period specified. Transactions are sorted into a set of 
	lightning-specific entry types. Fiat values can optionally be 
	included using the --enable_fiat flag. To write the report 
	directly to a csv, set a target directory using the --csv_path 
	flag.

	These reports can optionally be created with custom categories. 
	This requires providing a name for the category, and a set of 
	regular expressions which identify the labels of transactions 
	belonging in the category. To directly string match, just provide
	the string itself. These regular expressions are matched against 
	the labels for on chain transactions and the invoices on memos 
	(at present we cannot match forwarding events and payments).

	Categories should be expressed as a json array with the 
	following format:
	--categories='[
		{ 
			"name": "test", 
			"on_chain": true, 
			"label_patterns": ["test[0-9]*", "example(1|2)"] 
		},
		{
			"name": "category 2",
			...
		},
	]'
`,
	Flags: []cli.Flag{
		cli.Int64Flag{
			Name: "start_time",
			Usage: "(optional) The unix timestamp in seconds " +
				"from which the report should be generated, " +
				"defaults to one week ago",
		},
		cli.Int64Flag{
			Name: "end_time",
			Usage: "(optional) The unix timestamp in seconds " +
				"until which the report should be generated. " +
				"If not set, the report will be produced " +
				"until the present.",
		},
		cli.StringFlag{
			Name: "csv_path",
			Usage: "A path to write node_report.csv to. If not " +
				"set, the command will output the report. " +
				"Note that write permissions are required.",
		},
		cli.BoolFlag{
			Name:  "enable_fiat",
			Usage: "Create a report with fiat conversions.",
		},
		cli.StringFlag{
			Name: "categories",
			Usage: "A set of custom categories to create the " +
				"report with, expressed as a json array.",
		},
		cli.BoolFlag{
			Name: "loop-category",
			Usage: "Add a custom category called 'loop' " +
				"containing all transactions associated with " +
				"Lightning Labs Loop swaps. Note that this " +
				"category currently does not include off " +
				"chain payments.",
		},
		cli.BoolFlag{
			Name: "pool-category",
			Usage: "Add a custom category called 'pool' " +
				"containing all transactions associated with " +
				"Lightning Labs Pool trades. Note that this " +
				"category currently does not include off " +
				"chain payments.",
		},
		fiatBackendFlag,
		cli.StringFlag{
			Name: "prices_csv_path",
			Usage: "Path to a CSV file containing custom fiat " +
				"price data. This is only required if " +
				"'fiat_backend' is set to 'custom'.",
		},
		cli.StringFlag{
			Name: "custom_price_currency",
			Usage: "The currency that the custom prices are " +
				"quoted in. This is only required if " +
				"'fiat_backend' is set to 'custom'.",
		},
	},
	Action: queryOnChainReport,
}

func queryOnChainReport(ctx *cli.Context) error {
	client, cleanup := getClient(ctx)
	defer cleanup()

	fiatBackend, err := parseFiatBackend(ctx.String("fiat_backend"))
	if err != nil {
		return err
	}

	startTime := ctx.Int64("start_time")
	endTime := ctx.Int64("end_time")

	// nolint: prealloc
	var filteredPrices []*frdrpc.BitcoinPrice

	if fiatBackend == frdrpc.FiatBackend_CUSTOM {
		customPrices, err := parsePricesFromCSV(
			ctx.String("prices_csv_path"),
			ctx.String("custom_price_currency"),
		)
		if err != nil {
			return err
		}

		filteredPrices, err = filterPrices(
			customPrices, startTime, endTime,
		)
		if err != nil {
			return err
		}
	}

	// Set start and end times from user specified values, defaulting
	// to zero if they are not set.
	req := &frdrpc.NodeAuditRequest{
		StartTime:    uint64(startTime),
		EndTime:      uint64(endTime),
		DisableFiat:  !ctx.IsSet("enable_fiat"),
		FiatBackend:  fiatBackend,
		CustomPrices: filteredPrices,
	}

	// If start time is zero, default to a week ago.
	if req.StartTime == 0 {
		weekAgo := time.Now().Add(time.Hour * 24 * 7 * -1)
		req.StartTime = uint64(weekAgo.Unix())
	}

	var (
		categoryStr = ctx.String("categories")
		categories  []*frdrpc.CustomCategory
	)

	// If we have custom categories set, unmarshal them and add them to
	// our request.
	if categoryStr != "" {
		err := json.Unmarshal([]byte(categoryStr), &categories)
		if err != nil {
			return err
		}
		req.CustomCategories = categories
	}

	if ctx.Bool("loop-category") {
		req.CustomCategories = append(
			req.CustomCategories,
			&frdrpc.CustomCategory{
				Name:     "loop",
				OnChain:  true,
				OffChain: true,
				LabelPatterns: []string{
					"loopd --",
					"swap",
				},
			})
	}

	if ctx.Bool("pool-category") {
		req.CustomCategories = append(
			req.CustomCategories, &frdrpc.CustomCategory{
				Name:     "pool",
				OnChain:  true,
				OffChain: true,
				LabelPatterns: []string{
					"poold --",
				},
			},
		)
	}

	rpcCtx := context.Background()
	report, err := client.NodeAudit(rpcCtx, req)
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

	var headers string
	if len(report.Reports) > 0 {
		headers = fmt.Sprintf(
			CSVHeaders,
			report.Reports[0].BtcPrice.Currency,
		)
	}

	csvStrs := []string{headers}
	for _, report := range report.Reports {
		csvStrs = append(csvStrs, writeToCSV(report))
	}
	csvString := strings.Join(csvStrs, "\n")

	_, err = file.WriteString(csvString)
	return err
}
