package main

import (
	"os"

	"github.com/lightninglabs/faraday"
	"github.com/urfave/cli"
)

var (
	defaultRPCPort     = "8465"
	defaultRPCHostPort = "localhost:" + defaultRPCPort
	tlsCertFlag        = cli.StringFlag{
		Name: "tlscertpath",
		Usage: "path to faraday's TLS certificate, only " +
			"needed if faraday runs in the same process " +
			"as lnd (GrUB)",
	}
)

func main() {
	app := cli.NewApp()
	app.Name = "frcli"
	app.Usage = "command line tool for faraday"
	app.Version = faraday.Version()
	app.Flags = []cli.Flag{
		cli.StringFlag{
			Name:  "rpcserver",
			Value: defaultRPCHostPort,
			Usage: "host:port of faraday",
		},
		tlsCertFlag,
	}
	app.Commands = []cli.Command{
		thresholdRecommendationCommand,
		outlierRecommendationCommand,
		revenueReportCommand,
		channelInsightsCommand,
		fiatEstimateCommand,
	}

	if err := app.Run(os.Args); err != nil {
		fatal(err)
	}
}
