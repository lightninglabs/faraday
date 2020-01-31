package main

import (
	"os"

	"github.com/urfave/cli"
)

var (
	defaultRPCPort     = "8419"
	defaultRPCHostPort = "localhost:" + defaultRPCPort
)

func main() {
	app := cli.NewApp()
	app.Name = "trmcli"
	app.Usage = "command line tool for terminator"
	app.Flags = []cli.Flag{
		cli.StringFlag{
			Name:  "rpcserver",
			Value: defaultRPCHostPort,
			Usage: "host:port of terminator",
		},
	}
	app.Commands = []cli.Command{
		closeRecommendationCommand,
		revenueReportCommand,
	}

	if err := app.Run(os.Args); err != nil {
		fatal(err)
	}
}
