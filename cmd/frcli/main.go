package main

import (
	"os"

	"github.com/lightninglabs/faraday"
	"github.com/urfave/cli"
)

var (
	defaultRPCPort     = "8465"
	defaultRPCHostPort = "localhost:" + defaultRPCPort
	faradayDirFlag     = cli.StringFlag{
		Name:  "faradaydir",
		Value: faraday.FaradayDirBase,
		Usage: "path to faraday's base directory",
	}
	networkFlag = cli.StringFlag{
		Name: "network, n",
		Usage: "the network faraday is running on e.g. mainnet, " +
			"testnet, etc.",
		Value: faraday.DefaultNetwork,
	}
	tlsCertFlag = cli.StringFlag{
		Name:  "tlscertpath",
		Usage: "path to faraday's TLS certificate",
		Value: faraday.DefaultTLSCertPath,
	}
	macaroonPathFlag = cli.StringFlag{
		Name:  "macaroonpath",
		Usage: "path to macaroon file",
		Value: faraday.DefaultMacaroonPath,
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
		networkFlag,
		faradayDirFlag,
		tlsCertFlag,
		macaroonPathFlag,
	}
	app.Commands = []cli.Command{
		thresholdRecommendationCommand,
		outlierRecommendationCommand,
		revenueReportCommand,
		channelInsightsCommand,
		fiatEstimateCommand,
		onChainReportCommand,
		closeReportCommand,
	}

	if err := app.Run(os.Args); err != nil {
		fatal(err)
	}
}
