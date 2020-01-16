// Package terminator contains the main function for the terminator.
package terminator

import (
	"context"
	"fmt"

	"github.com/lightninglabs/loop/lndclient"
	"github.com/lightninglabs/terminator/recommend"
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
		return fmt.Errorf("cannot connect to lightning client: %v", err)
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

			// For the first iteration of the terminator, we set a
			// multiplier which will only detect extreme values so
			// that we conservatively recommend closes.
			OutlierMultiplier: recommend.DefaultOutlierMultiplier,

			// Set the minimum monitor time to the value provided in our config.
			MinimumMonitored: config.MinimumMonitored,
		})
	if err != nil {
		return fmt.Errorf("could not get close recommendations: %v", err)
	}

	log.Infof("Considering: %v channels for closure from a "+
		"total of: %v.", report.ConsideredChannels,
		report.TotalChannels)

	log.Info("Outlier Recommendations:")
	for channel, rec := range report.OutlierRecommendations {
		log.Infof("%v: Value: %v, Recommend Close: %v", channel,
			rec.Value, rec.RecommendClose)
	}

	log.Info("Threshold Recommendations:")
	for channel, rec := range report.ThresholdRecommendations {
		log.Infof("%v: Value: %v, Recommend Close: %v", channel,
			rec.Value, rec.RecommendClose)
	}

	log.Info("That's all for now. I will be back.")

	return nil
}
