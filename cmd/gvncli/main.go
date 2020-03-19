package main

import (
	"os"

	"github.com/lightninglabs/governator"
	"github.com/urfave/cli"
)

var (
	defaultRPCPort     = "8419"
	defaultRPCHostPort = "localhost:" + defaultRPCPort
)

func main() {
	app := cli.NewApp()
	app.Name = "gvncli"
	app.Usage = "command line tool for governator"
	app.Version = governator.Version()
	app.Flags = []cli.Flag{
		cli.StringFlag{
			Name:  "rpcserver",
			Value: defaultRPCHostPort,
			Usage: "host:port of governator",
		},
	}
	app.Commands = []cli.Command{
		thresholdRecommendationCommand,
		outlierRecommendationCommand,
		revenueReportCommand,
		channelInsightsCommand,
	}

	if err := app.Run(os.Args); err != nil {
		fatal(err)
	}
}
