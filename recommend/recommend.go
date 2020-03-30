// Package recommend provides recommendations for closing channels with the
// constraints provided in its close recommendation config. Only open public
// channels that have been monitored for the configurable minimum monitored
// time will be considered for closing.
//
// Channels will be assessed based on the following data points:
// - Uptime ratio
// - Fee revenue per block capital has been committed for
// - Incoming volume per block capital has been committed for
// - Outgoing volume per block capital has been committed for
// - Total volume per block capital has been committed for
//
// Channels that are outliers within the set of channels that are eligible for
// close recommendation will be recommended for closure.
package recommend

import (
	"errors"
	"time"

	"github.com/lightninglabs/faraday/dataset"
	"github.com/lightninglabs/faraday/insights"
)

var (
	// errZeroMinMonitored is returned when the minimum age provided
	// by the config is zero.
	errZeroMinMonitored = errors.New("must provide a non-zero minimum " +
		"monitor time for channel exclusion")

	// ErrNoMetric is returned when a close recommendations with no chosen
	// metric is provided.
	ErrNoMetric = errors.New("metric required for close " +
		"recommendations")

	// DefaultOutlierMultiplier is the default value used in close
	// recommendations based on outliers when there is no user provided
	// value.
	DefaultOutlierMultiplier float64 = 3
)

// Metric is an enum which indicate what data point our recommendations should
// be based on.
type Metric int

const (
	invalidMetric Metric = iota

	// UptimeMetric bases recommendations on the uptime of the channel's
	// remote peer.
	UptimeMetric

	// RevenueMetric bases recommendations on the revenue that the channel
	// has generated per block that our capital has been committed for.
	RevenueMetric

	// IncomingVolume bases recommendations on the incoming volume that the
	// channel has processed, scaled by funding transaction confirmations.
	IncomingVolume

	// IncomingVolume bases recommendations on the incoming volume that the
	// channel has processed, scaled by funding transaction confirmations.
	OutgoingVolume

	// Volume bases recommendations on the total volume that the
	// channel has processed, scaled by funding transaction confirmations.
	Volume
)

// CloseRecommendationConfig provides the functions and parameters required to
// provide close recommendations. This struct holds fields which are common to
// all recommendation calculation strategies.
type CloseRecommendationConfig struct {
	// ChannelInsights is a function which returns a set of channel insights
	// for our current set of channels.
	ChannelInsights func() ([]*insights.ChannelInfo, error)

	// Metric defines the metric that we will use to provide close
	// recommendations. Calls will fail if no value is provided.
	Metric Metric

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

	// Recommendations is a map of chanel outpoints to a bool which
	// indicates whether we should close the channel.
	Recommendations map[string]Recommendation
}

// OutlierRecommendations returns recommendations based on whether a value is a
// lower outlier within its current dataset. It takes an outlier multiplier value
// which is the number of inter quartile ranges a value should be away from the
// lower/upper quartile to be considered an outlier. Recommended values are 1.5
// for more aggressive recommendations and 3 for more cautious recommendations.
func OutlierRecommendations(cfg *CloseRecommendationConfig,
	outlierMultiplier float64) (*Report, error) {

	getRecs := func(dataset dataset.Dataset) (map[string]Recommendation, error) {
		return getOutlierRecs(dataset, outlierMultiplier, false)
	}

	return closeRecommendations(cfg, getRecs)
}

// ThresholdRecommendations returns a recommendations based on whether a value is
// below a given threshold.
func ThresholdRecommendations(cfg *CloseRecommendationConfig,
	threshold float64) (*Report, error) {

	getRecs := func(dataset dataset.Dataset) (map[string]Recommendation, error) {
		return getThresholdRecs(dataset, threshold, true), nil
	}

	return closeRecommendations(cfg, getRecs)
}

// closeRecommendations returns a report which contains information about the
// channels that were considered and a list of close recommendations. It takes
// a function which can produce the relevant dataset from a set of channel
// insights and a function which can produces recommendations as parameters.
func closeRecommendations(cfg *CloseRecommendationConfig,
	getRecommendations func(data dataset.Dataset) (
		map[string]Recommendation, error)) (*Report, error) {

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

	report := &Report{
		TotalChannels:      len(channels),
		ConsideredChannels: len(filtered),
	}

	var data dataset.Dataset
	switch cfg.Metric {
	case UptimeMetric:
		data = getUptimeDataset(filtered)

	case RevenueMetric:
		data = getConfirmationScaledDataset(revenueValue, filtered)

	case IncomingVolume:
		data = getConfirmationScaledDataset(
			incomingVolumeValue, filtered,
		)

	case OutgoingVolume:
		data = getConfirmationScaledDataset(
			outgoingVolumeValue, filtered,
		)

	case Volume:
		data = getConfirmationScaledDataset(totalVolumeValue, filtered)

	default:
		return nil, ErrNoMetric
	}

	// Get close recommendations based on outliers.
	report.Recommendations, err = getRecommendations(data)
	if err != nil {
		return nil, err
	}

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

// getConfirmationScaledDataset returns a dataset that scales a value by the
// number of confirmations its funding transaction has. It takes a function
// which gets the relevant value from the channel insight as input.
func getConfirmationScaledDataset(getValue perConfirmationValue,
	eligibleChannels []*insights.ChannelInfo) dataset.Dataset {

	// Create a map which will hold channel point string label to revenue
	// per block that we have had revenue committed for.
	var channels = make(map[string]float64, len(eligibleChannels))

	for _, channel := range eligibleChannels {
		// Channels cannot have zero confirmations because we are
		// dealing with open (ie confirmed) channels, so we can
		// get the value and scale it by our confirmation total.
		valuePerConfirmation :=
			getValue(channel) /
				float64(channel.Confirmations)

		channels[channel.ChannelPoint] = valuePerConfirmation
	}

	return channels
}

// perConfirmationValue is a function which gets a value from a channel insight
// that needs to be scaled by its number of confirmations.
type perConfirmationValue func(channel *insights.ChannelInfo) float64

// revenueValue gets total revenue for a channel.
func revenueValue(channel *insights.ChannelInfo) float64 {
	return float64(channel.FeesEarned)
}

// incomingVolumeValue gets total incoming volume for a channel.
func incomingVolumeValue(channel *insights.ChannelInfo) float64 {
	return float64(channel.VolumeIncoming)
}

// outgoingVolumeValue gets total outgoing volume for a channel.
func outgoingVolumeValue(channel *insights.ChannelInfo) float64 {
	return float64(channel.VolumeOutgoing)
}

// totalVolumeValue gets total volume for a channel.
func totalVolumeValue(channel *insights.ChannelInfo) float64 {
	return float64(channel.VolumeIncoming + channel.VolumeOutgoing)
}
