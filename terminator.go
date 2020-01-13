// Package terminator contains the main function for the terminator.
package terminator

import (
	"context"
	"fmt"
	"time"

	"github.com/lightninglabs/loop/lndclient"
	"github.com/lightninglabs/terminator/dataset"
	"github.com/lightninglabs/terminator/insights"
	"github.com/lightninglabs/terminator/recommend"
	"github.com/lightninglabs/terminator/revenue"
	"github.com/lightninglabs/terminator/utils"
	"github.com/lightningnetwork/lnd/lnrpc"
)

var (
	// revenueFileName is the name of the file that the revenue report is
	// optionally written to.
	revenueFileName = "revenue.json"

	// insightsFileName is the name of the file that channel insights are
	// optionally written to.
	insightsFileName = "insights.json"

	// recsFileName is the name of the file that close recommendations
	// are optionally written to.
	recsFileName = "recommendations.json"
)

// Main is the real entry point for terminator. It is required to ensure that
// defers are properly executed when os.Exit() is called.
func Main() error {
	config, err := loadConfig()
	if err != nil {
		return fmt.Errorf("error loading config: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// NewBasicClient get a lightning rpc client with
	client, err := lndclient.NewBasicClient(
		config.RPCServer,
		config.TLSCertPath,
		config.MacaroonDir,
		config.network,
		lndclient.MacFilename(config.MacaroonFile),
	)
	if err != nil {
		return fmt.Errorf("cannot connect to lightning "+
			"client: %v", err)
	}

	openChannels, err := client.ListChannels(
		ctx, &lnrpc.ListChannelsRequest{},
	)
	if err != nil {
		return err
	}

	// Get a revenue report for the period specified.
	revenueReport, err := revenue.GetRevenueReport(&revenue.Config{
		OpenChannels: openChannels.Channels,
		ClosedChannels: func() ([]*lnrpc.ChannelCloseSummary, error) {
			resp, err := client.ClosedChannels(
				ctx, &lnrpc.ClosedChannelsRequest{},
			)
			if err != nil {
				return nil, err
			}

			return resp.Channels, nil
		},
		ForwardingHistory: func(offset, maxEvents uint32) (
			events []*lnrpc.ForwardingEvent, u uint32, e error) {
			// Query for events from the revenue period offset
			// until the present.
			start := uint64(
				time.Now().Add(config.RevenuePeriod * -1).Unix(),
			)

			resp, err := client.ForwardingHistory(
				ctx, &lnrpc.ForwardingHistoryRequest{
					StartTime:    start,
					EndTime:      uint64(time.Now().Unix()),
					IndexOffset:  offset,
					NumMaxEvents: maxEvents,
				},
			)
			if err != nil {
				return nil, 0, err
			}
			return resp.ForwardingEvents, resp.LastOffsetIndex, nil
		},
	})
	if err != nil {
		return err
	}

	if config.WritePath != "" {
		if err := utils.WriteJSONToPath(
			revenueReport, config.WritePath, revenueFileName,
		); err != nil {
			return fmt.Errorf("could not revenue reenue to file: %v",
				err)
		}
	} else {
		log.Info("Revenue Report:")
		log.Info(revenueReport)
	}

	// Gather relevant insights on all channels.
	channels, err := insights.GetChannels(&insights.Config{
		OpenChannels: openChannels.Channels,
		CurrentHeight: func() (u uint32, e error) {
			resp, err := client.GetInfo(ctx,
				&lnrpc.GetInfoRequest{})
			if err != nil {
				return 0, err
			}

			return resp.BlockHeight, nil
		},
		RevenueReport: revenueReport,
	})
	if err != nil {
		return err
	}

	// If a write path is set, write channel insights to file, otherwise
	// log.
	if config.WritePath != "" {
		if err = utils.WriteJSONToPath(
			channels, config.WritePath, insightsFileName,
		); err != nil {
			return fmt.Errorf("could not write insights to "+
				"file: %v", err)
		}
	} else {
		log.Info("Channel insights:")
		for _, channel := range channels {
			log.Info(channel)
		}
	}

	// Get channel close recommendations for the current set of open public
	// channels.
	report, err := recommend.CloseRecommendations(
		&recommend.CloseRecommendationConfig{
			// ChannelInsights provides all of the node's open
			// channels with uptime and revenue data.
			ChannelInsights: channels,

			// For the first iteration of the terminator, do not allow users
			// to configure recommendations to penalize weak outliers.
			StrongOutlier: true,

			// Set the minimum monitor time to the value provided
			// in our config.
			MinimumMonitored: config.MinimumMonitored,
		})
	if err == dataset.ErrTooFewValues {
		log.Infof("no channels are eligible for close at present")
		return nil
	} else if err != nil {
		return fmt.Errorf("could not get close "+
			"recommendations: %v", err)
	}

	// If a write path is set, write close recommendations to file,
	// otherwise log.
	if config.WritePath != "" {
		if err = utils.WriteJSONToPath(
			report, config.WritePath, recsFileName,
		); err != nil {
			return fmt.Errorf("could not write insights to "+
				"file: %v", err)
		}
	} else {
		log.Info("Close recommendations:")
		log.Info(report)
	}

	log.Info("That's all for now. I will be back.")

	return nil
}
