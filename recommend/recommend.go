// Package recommend provides recommendations for closing channels with the
// constraints provided in its close recommendation config. Only open public
// channels that have been monitored for the configurable minimum monitored
// time will be considered for closing.
//
// Channels will be assessed based on the following data points:
// - Uptime ratio
//
// Channels that are outliers within the set of channels that are eligible for
// close recommendation will be recommended for closure.
package recommend

import (
	"errors"
	"time"

	"github.com/lightninglabs/terminator/dataset"
	"github.com/lightninglabs/terminator/insights"
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
	// ChannelInsights is a function which returns a set of channel insights
	// for our current set of channels.
	ChannelInsights func() ([]*insights.ChannelInfo, error)

	// OutlierMultiplier is the number of inter quartile ranges a value
	// should be away from the lower/upper quartile to be considered an
	// outlier. Recommended values are 1.5 for more aggressive
	// recommendations and 3 for more cautious recommendations.
	OutlierMultiplier float64

	// UptimeThreshold is the uptime ratio over the channel's observed
	// lifetime beneath which channels will be recommended for close. This
	// value is expressed as a ratio in [0,1], and will default to 0 if
	// it is not set.
	UptimeThreshold float64

	// MinimumMonitored is the minimum amount of time that a channel must
	// have been monitored for before it is considered for closing.
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
// are considered for close if their uptime ratio is a lower outlier in
// uptime ratio dataset.
func CloseRecommendations(cfg *CloseRecommendationConfig) (*Report, error) {
	// Check that the minimum wait time is non-zero.
	if cfg.MinimumMonitored == 0 {
		return nil, errZeroMinMonitored
	}

	// Get the set of insights for our currently open channels.
	channels, err := cfg.ChannelInsights()
	if err != nil {
		return nil, err
	}

	// Filter out channels that are below the minimum required age.
	filtered := filterChannels(channels, cfg.MinimumMonitored)

	// Produce a dataset containing uptime ratio for channels that have
	// been monitored for longer than the minimum time.
	uptime := getUptimeDataset(filtered)

	report := &Report{
		TotalChannels:      len(channels),
		ConsideredChannels: len(filtered),
	}

	// Get close recommendations based on outliers.
	report.OutlierRecommendations, err = getOutlierRecs(
		uptime, cfg.OutlierMultiplier, false,
	)
	if err != nil {
		return nil, err
	}

	// Get close recommendations based on threshold.
	report.ThresholdRecommendations = getThresholdRecs(
		uptime, cfg.UptimeThreshold, true,
	)

	return report, nil
}

// getThresholdRecs returns a map of channel points to values that are above
// or below a given threshold.
func getThresholdRecs(values dataset.Dataset,
	threshold float64, belowThreshold bool) map[string]Recommendation {

	// Get a map of channel labels to a boolean indicating whether
	// they are beneath the threshold.
	thresholdValues := values.GetThreshold(threshold, belowThreshold)

	recommendations := make(
		map[string]Recommendation, len(thresholdValues),
	)

	for chanPoint, crossesThreshold := range thresholdValues {
		recommendations[chanPoint] = Recommendation{
			Value:          values.Value(chanPoint),
			RecommendClose: crossesThreshold,
		}
	}

	return recommendations
}

// getOutlierRecs generates map of channel outpoint strings to booleans
// indicating whether we recommend closing a channel. It takes a outlier
// multiplier which scales the degree to which we want to calculate outliers,
// and an upper outlier boolean which determines whether we want to identify
// upper or lower outliers.
func getOutlierRecs(values dataset.Dataset,
	outlierMultiplier float64,
	upperOutlier bool) (map[string]Recommendation, error) {

	recommendations := make(map[string]Recommendation)

	outliers, err := values.GetOutliers(outlierMultiplier)
	if err != nil {
		return nil, err
	}

	// Add a recommendation for each channel to our set of recommendations.
	// RecommendClose in the recommendation will be set to true if the
	// channel matches the outlier type we are looking for (upper or
	// lower).
	for chanPoint, outlier := range outliers {
		var recommendClose bool

		// If we want to detect upper outliers, and the channel is a
		// upper outlier, set recommend close to true.
		if upperOutlier && outlier.UpperOutlier {
			recommendClose = true
		}

		// If we want to detect lower outliers, and the channel is a
		// lower outlier, set recommend close to true.
		if !upperOutlier && outlier.LowerOutlier {
			recommendClose = true
		}

		recommendations[chanPoint] = Recommendation{
			Value:          values.Value(chanPoint),
			RecommendClose: recommendClose,
		}
	}

	return recommendations, nil
}

// filterChannels filters out channels that are beneath the minimum age, or
// are private and returns a set of channels that are eligible for close
// recommendations.
func filterChannels(channelInsights []*insights.ChannelInfo,
	minimumAge time.Duration) []*insights.ChannelInfo {

	filteredChannels := make(
		[]*insights.ChannelInfo, 0, len(channelInsights),
	)

	for _, channel := range channelInsights {
		if channel.MonitoredFor < minimumAge {
			log.Tracef("Channel: %v has not been "+
				"monitored for long enough, excluding it "+
				"from consideration", channel.ChannelPoint)

			continue
		}

		if channel.Private {
			log.Tracef("Channel: %v is private, excluding "+
				"it from consideration", channel.ChannelPoint)

			continue
		}

		filteredChannels = append(filteredChannels, channel)
	}

	log.Debugf("considering: %v channels for close out of %v",
		len(filteredChannels), len(channelInsights))

	return filteredChannels
}

// getUptimeDataset takes a set of channels that are eligible for close and
// produces an uptime dataset.
func getUptimeDataset(
	eligibleChannels []*insights.ChannelInfo) dataset.Dataset {

	// Create a map which will hold channel point string label to uptime
	// ratio.
	var channels = make(map[string]float64, len(eligibleChannels))

	for _, channel := range eligibleChannels {
		// Calculate the uptime ratio for the channel and add it
		// to the channel -> uptime map.
		uptimeRatio := float64(channel.Uptime) /
			float64(channel.MonitoredFor)

		channels[channel.ChannelPoint] = uptimeRatio
	}

	// Create a dataset for the uptime values we have collected.
	return dataset.New(channels)
}
