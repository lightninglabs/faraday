// Package recommend provides recommendations for closing channels with the
// constraints provided in its close recommendation config. Only open public
// channels that have been monitored for the configurable minimum monitored
// time will be considered for closing.
//
// Channels will be assessed based on the following data points:
// - Uptime percentage
//
// Channels that are outliers within the set of channels that are eligible for
// close recommendation will be recommended for closure.
package recommend

import (
	"errors"
	"time"

	"github.com/lightninglabs/terminator/dataset"
	"github.com/lightningnetwork/lnd/lnrpc"
)

// errZeroMinMonitored is returned when the minimum ages provided by the config
// is zero.
var errZeroMinMonitored = errors.New("must provide a non-zero minimum " +
	"monitor time for channel exclusion")

// CloseRecommendationConfig provides the functions and parameters required to
// provide close recommendations.
type CloseRecommendationConfig struct {
	// OpenChannels is a function which returns all of our currently open,
	// public channels.
	OpenChannels func() ([]*lnrpc.Channel, error)

	// StrongOutlier is set to true if only extreme outliers should be
	// recommended for close. A strong outlier is one which is 3 inter-
	// quartile ranges below the lower quartile (or above the upper quartile)
	// amd a weak outlier is only 1.5 inter-quartile ranges away. Choosing
	// to recommend strong outliers is a more cautious approach, because the
	// recommendations will be more lenient, only recommending extreme outliers
	// for closure.
	StrongOutlier bool

	// MinimumMonitored is the minimum amount of time that a channel must have
	// been monitored for before it is considered for closing.
	MinimumMonitored time.Duration
}

// Report contains a set of close recommendations and information about the
// number of channels considered for close.
type Report struct {
	// TotalChannels is the number of channels that we have.
	TotalChannels int

	// ConsideredChannels is the number of channels that have been monitored
	// for long enough to be considered for close.
	ConsideredChannels int

	// Recommendations is a map of chanel outpoints to a bool which indicates
	// whether we should close the channel.
	Recommendations map[string]bool
}

// CloseRecommendations returns a report which contains information about the
// channels that were considered and a list of close recommendations. Channels
// are considered for close if their uptime percentage is a lower outlier in
// uptime percentage dataset.
func CloseRecommendations(cfg *CloseRecommendationConfig) (*Report, error) {
	// Check that the minimum wait time is non-zero.
	if cfg.MinimumMonitored == 0 {
		return nil, errZeroMinMonitored
	}

	// Get the set of currently open channels.
	channels, err := cfg.OpenChannels()
	if err != nil {
		return nil, err
	}

	// Filter out channels that are below the minimum required age.
	filtered := filterChannels(channels, cfg.MinimumMonitored)

	// Produce a dataset containing uptime percentage for channels that have
	// been monitored for longer than the minimum time.
	uptime := getUptimeDataset(filtered)

	recs, err := getCloseRecs(uptime, cfg.StrongOutlier)
	if err != nil {
		return nil, err
	}

	return &Report{
		TotalChannels:      len(channels),
		ConsideredChannels: len(uptime),
		Recommendations:    recs,
	}, nil
}

// getCloseRecs generates map of channel outpoint strings to booleans indicating
// whether we recommend closing a channel.
func getCloseRecs(uptime dataset.Dataset,
	strongOutlier bool) (map[string]bool, error) {

	outliers, err := uptime.GetOutliers(strongOutlier)
	if err != nil {
		return nil, err
	}

	recommendations := make(map[string]bool)

	for chanpoint, outlier := range outliers {
		// If the channel is a lower outlier, recommend it for closure.
		if outlier.LowerOutlier {
			recommendations[chanpoint] = true
		}
	}

	return recommendations, nil
}

// filterChannels filters out channels that are beneath the minimum age and
// produces a map of channel outpoint strings to rpc channels which contains the
// channels that are eligible for close recommendation.
func filterChannels(openChannels []*lnrpc.Channel,
	minimumAge time.Duration) map[string]*lnrpc.Channel {

	// Create a map which will hold channel point labels to uptime percentage.
	channels := make(map[string]*lnrpc.Channel)

	for _, channel := range openChannels {
		if channel.Lifetime < int64(minimumAge.Seconds()) {
			log.Tracef("Channel: %v has not been monitored for long enough,"+
				" excluding it from consideration", channel.ChannelPoint)
			continue
		}

		channels[channel.ChannelPoint] = channel
	}

	log.Debugf("considering: % channels for close out of %v",
		len(channels), len(openChannels))

	return channels
}

// getUptimeDataset takes a set of channels that are eligible for close and
// produces an uptime dataset.
func getUptimeDataset(
	eligibleChannels map[string]*lnrpc.Channel) dataset.Dataset {

	// Create a map which will hold channel point string label to uptime percentage.
	var channels = make(map[string]float64)

	for outpoint, channel := range eligibleChannels {
		// Calculate the uptime percentage for the channel and add it to the
		// channel -> uptime map.
		uptimePercentage := float64(channel.Uptime) / float64(channel.Lifetime)
		channels[outpoint] = uptimePercentage

		log.Tracef("channel: %v has uptime percentage: %v", outpoint,
			uptimePercentage)
	}

	// Create a dataset for the uptime values we have collected.
	return dataset.New(channels)
}
