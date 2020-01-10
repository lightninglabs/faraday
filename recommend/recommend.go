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
	"fmt"
	"time"

	"github.com/lightninglabs/terminator/dataset"
	"github.com/lightninglabs/terminator/insights"
)

// errZeroMinMonitored is returned when the minimum ages provided by the config
// is zero.
var errZeroMinMonitored = errors.New("must provide a non-zero minimum " +
	"monitor time for channel exclusion")

// CloseRecommendationConfig provides the functions and parameters required to
// provide close recommendations.
type CloseRecommendationConfig struct {
	// ChannelInsights is a set of channels with data points relevant to
	// channel close decisions.
	ChannelInsights []*insights.Channel

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

	// UptimeRecommendations is a map of chanel outpoints to a bool which
	// indicates whether we should close the channel because its uptime
	// is a statistical outlier.
	UptimeRecommendations map[string]bool

	// RevenueRecommendations is a map of chanel outpoints to a bool which
	// indicates whether we should close the channel because its revenue
	// per block open is a statistical outlier.
	RevenueRecommendations map[string]bool
}

// reportTemplate is a template for petty printing revenue reports.
var reportTemplate = `Total Channels: %v
Channels Considered: %v
%v
%v`

// String returns a string representation of a rReport.
func (r *Report) String() string {
	var (
		uptimeRecs = fmt.Sprintf("Uptime Based Close "+
			"Recommendations: %v", len(r.UptimeRecommendations))

		revenueRecs = fmt.Sprintf("Revenue Based Close "+
			"Recommendations: %v", len(r.RevenueRecommendations))
	)

	// Accumulate any uptime based recommendations.
	for channel, rec := range r.UptimeRecommendations {
		if rec {
			uptimeRecs = fmt.Sprintf("%v\n%v", uptimeRecs,
				channel)
		}
	}

	// Accumulate any revenue based recommendations.
	for channel, rec := range r.RevenueRecommendations {
		if rec {
			revenueRecs = fmt.Sprintf("%v\n%v", revenueRecs,
				channel)
		}
	}

	// Return report template populated with report details.
	return fmt.Sprintf(reportTemplate, r.TotalChannels,
		r.ConsideredChannels, uptimeRecs, revenueRecs)
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

	// Filter out channels that are below the minimum required age.
	filtered := filterChannels(cfg.ChannelInsights, cfg.MinimumMonitored)

	// Produce a dataset containing uptime percentage for channels that have
	// been monitored for longer than the minimum time.
	uptime, revenue := getDatasets(filtered)

	uptimeRecs, err := getOutlierRecommendations(
		uptime, cfg.StrongOutlier,
	)
	if err != nil {
		return nil, err
	}

	revenueRecs, err := getOutlierRecommendations(
		revenue, cfg.StrongOutlier,
	)
	if err != nil {
		return nil, err
	}

	return &Report{
		TotalChannels:          len(cfg.ChannelInsights),
		ConsideredChannels:     len(filtered),
		UptimeRecommendations:  uptimeRecs,
		RevenueRecommendations: revenueRecs,
	}, nil
}

// getOutlierRecommendations generates map of channel outpoint strings to
// booleans indicating whether we recommend closing a channel because it is
// a statistical lower outlier.
func getOutlierRecommendations(uptime dataset.Dataset,
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

// filterChannels filters out channels that are beneath the minimum age or
// private and produces a set of channels that are eligible for close
// recommendation.
func filterChannels(openChannels []*insights.Channel,
	minimumAge time.Duration) []*insights.Channel {

	channels := make([]*insights.Channel, 0, len(openChannels))

	for _, channel := range openChannels {
		if channel.MonitoredFor < minimumAge {
			log.Tracef("Channel: %v has not been monitored for "+
				"long enough, excluding it from consideration",
				channel.ChannelPoint)
			continue
		}

		if channel.Private {
			log.Tracef("private channel: %v excluded from "+
				"consideration", channel.ChannelPoint)
			continue
		}

		channels = append(channels, channel)
	}

	log.Debugf("considering: % channels for close out of %v",
		len(channels), len(openChannels))

	return channels
}

// getDatasets takes a set of channels that are eligible for close and
// produces relevant datasets.
func getDatasets(eligibleChannels []*insights.Channel) (
	dataset.Dataset, dataset.Dataset) {

	// Create a maps which will hold channel point string label to uptime
	// and revenue values.
	var (
		uptimeData  = make(map[string]float64)
		revenueData = make(map[string]float64)
	)

	for _, channel := range eligibleChannels {
		uptimeData[channel.ChannelPoint] =
			channel.UptimePercentage
		revenueData[channel.ChannelPoint] =
			float64(channel.RevenuePerBlock)
	}

	// Create a dataset for the uptime values we have collected.
	return dataset.New(uptimeData), dataset.New(revenueData)
}
