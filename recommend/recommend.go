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

var (
	// errZeroMinMonitored is returned when the minimum age provided
	// by the config is zero.
	errZeroMinMonitored = errors.New("must provide a non-zero minimum " +
		"monitor time for channel exclusion")

	// DefaultOutlierMultiplier is the default value used in close
	// recommendations based on outliers when there is no user provided
	// value.
	DefaultOutlierMultiplier float64 = 3
)

// CloseRecommendationConfig provides the functions and parameters required to
// provide close recommendations.
type CloseRecommendationConfig struct {
	// OpenChannels is a function which returns all of our currently open,
	// public channels.
	OpenChannels func() ([]*lnrpc.Channel, error)

	// OutlierMultiplier is the number of inter quartile ranges a value
	// should be away from the lower/upper quartile to be considered an
	// outlier. Recommended values are 1.5 for more aggressive recommendations
	// and 3 for more cautious recommendations.
	OutlierMultiplier float64

	// UptimeThreshold is the uptime percentage over the channel's observed
	// lifetime beneath which channels will be recommended for close. This
	// value is expressed as a percentage in [0,1], and will default to 0 if
	// it is not set.
	UptimeThreshold float64

	// MinimumMonitored is the minimum amount of time that a channel must have
	// been monitored for before it is considered for closing.
	MinimumMonitored time.Duration
}

// Recommendation provides the value that a close recommendation was
// based on, and a boolean indicating whether we recommend closing the
// channel.
type Recommendation struct {
	Value          float64
	RecommendClose bool
}

// Report contains a set of close recommendations and information about the
// number of channels considered for close.
type Report struct {
	// TotalChannels is the number of channels that we have.
	TotalChannels int

	// ConsideredChannels is the number of channels that have been monitored
	// for long enough to be considered for close.
	ConsideredChannels int

	// OutlierRecommendations is a map of chanel outpoints to a bool which
	// indicates whether we should close the channel based on whether it is
	// an outlier.
	OutlierRecommendations map[string]Recommendation

	// ThresholdRecommendations is a map of chanel outpoints to a bool which
	// indicates whether we should close the channel based on whether it is
	// below a user provided threshold.
	ThresholdRecommendations map[string]Recommendation
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

	report := &Report{
		TotalChannels:      len(channels),
		ConsideredChannels: len(uptime),
	}

	// Get close recommendations based on outliers.
	report.OutlierRecommendations, err = getOutlierRecs(
		uptime, cfg.OutlierMultiplier,
	)
	if err != nil {
		return nil, err
	}

	// Get close recommendations based on threshold.
	report.ThresholdRecommendations = getThresholdRecs(
		uptime, cfg.UptimeThreshold,
	)

	return report, nil
}

// getThresholdRecs returns a map of channel points to values that are below a
// given threshold.
func getThresholdRecs(uptime dataset.Dataset,
	threshold float64) map[string]Recommendation {

	// Get a map of channel labels to a boolean indicating whether
	// they are beneath the threshold.
	thresholdValues := uptime.GetThreshold(threshold, true)

	recommendations := make(
		map[string]Recommendation, len(thresholdValues),
	)

	for chanPoint, belowThrehsold := range thresholdValues {
		recommendations[chanPoint] = Recommendation{
			Value:          uptime.Value(chanPoint),
			RecommendClose: belowThrehsold,
		}
	}

	return recommendations
}

// getOutlierRecs generates map of channel outpoint strings to booleans indicating
// whether we recommend closing a channel.
func getOutlierRecs(uptime dataset.Dataset,
	outlierMultiplier float64) (map[string]Recommendation, error) {

	recommendations := make(map[string]Recommendation)

	outliers, err := uptime.GetOutliers(outlierMultiplier)
	if err != nil {
		return nil, err
	}

	// Add a recommendation for each channel to our set of recommendations.
	// If the channel is a lower outlier, we recommend it for close.
	for chanPoint, outlier := range outliers {
		recommendations[chanPoint] = Recommendation{
			Value:          uptime.Value(chanPoint),
			RecommendClose: outlier.LowerOutlier,
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

	log.Debugf("considering: %v channels for close out of %v",
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
