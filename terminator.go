// Package terminator contains the main function for the terminator.
package terminator

import (
	"context"
	"fmt"
	"time"

	"github.com/lightninglabs/loop/lndclient"
	"github.com/lightninglabs/terminator/insights"
	"github.com/lightninglabs/terminator/recommend"
	"github.com/lightninglabs/terminator/revenue"
	"github.com/lightningnetwork/lnd/lnrpc"
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
	log.Info(revenueReport)

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

	for _, channel := range channels {
		log.Info(channel)
	}

	// Get channel close recommendations for the current set of open public
	// channels.
	report, err := recommend.CloseRecommendations(
		&recommend.CloseRecommendationConfig{
			// OpenChannels provides all of the open, public channels for the
			// node.
			OpenChannels: func() (channels []*lnrpc.Channel, e error) {
				resp, err := client.ListChannels(ctx,
					&lnrpc.ListChannelsRequest{
						PublicOnly: true,
					})
				if err != nil {
					return nil, err
				}

				return resp.Channels, nil
			},

			// For the first iteration of the terminator, do not allow users
			// to configure recommendations to penalize weak outliers.
			StrongOutlier: true,

			// Set the minimum monitor time to the value provided in our config.
			MinimumMonitored: config.MinimumMonitored,
		})
	if err != nil {
		return fmt.Errorf("could not get close recommendations: %v", err)
	}

	log.Infof("Considering: %v channels for closure from a "+
		"total of: %v. Produced %v recommendations.", report.ConsideredChannels,
		report.TotalChannels, len(report.Recommendations))

	for channel, rec := range report.Recommendations {
		log.Infof("%v: %v", channel, rec)
	}

	log.Info("That's all for now. I will be back.")

	return nil
}
