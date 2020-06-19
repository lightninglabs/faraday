package insights

import (
	"time"

	"github.com/lightninglabs/faraday/revenue"
	"github.com/lightninglabs/lndclient"
	"github.com/lightningnetwork/lnd/lnwire"
)

// ChannelInfo provides a set of performance metrics for a lightning channel.
type ChannelInfo struct {
	// ChannelPoint is the outpoint of the channel's funding transaction.
	ChannelPoint string

	// MonitoredFor is the amount of time the channel's uptime has been
	// monitored by lnd.
	MonitoredFor time.Duration

	// Uptime is the total amount of time the channel's remote peer has
	// been online for.
	Uptime time.Duration

	// VolumeIncoming is the volume in millisatoshis that the channel has
	// forwarded through the node as the incoming channel.
	VolumeIncoming lnwire.MilliSatoshi

	// VolumeOutgoing is the volume in millisatoshis that the channel has
	// forwarded through the node as the outgoing channel.
	VolumeOutgoing lnwire.MilliSatoshi

	// FeesEarned is the total fees earned by the channel while routing.
	// Note that fees are split evenly between incoming and outgoing
	// channels.
	FeesEarned lnwire.MilliSatoshi

	// Confirmations is the number of confirmations the funding transction
	// has.
	Confirmations uint32

	// Private indicates whether the channel is private.
	Private bool
}

// Config provides insights with everything it needs to obtain channel
// insights.
type Config struct {
	// OpenChannels is a function which returns all of our currently open,
	// public and private channels.
	OpenChannels func() ([]lndclient.ChannelInfo, error)

	// CurrentHeight is a function which returns the current block
	// currentHeight.
	CurrentHeight func() (uint32, error)

	// RevenueReport is a report our channels revenue.
	RevenueReport *revenue.Report
}

// GetChannels returns an array of channel insights.
func GetChannels(cfg *Config) ([]*ChannelInfo, error) {
	// Get the current block height.
	height, err := cfg.CurrentHeight()
	if err != nil {
		return nil, err
	}

	channels, err := cfg.OpenChannels()
	if err != nil {
		return nil, err
	}

	insights := make([]*ChannelInfo, 0, len(channels))
	for _, channel := range channels {
		// Get the short channel ID so we can calculate the number of
		// blocks the channel has been open for.
		shortID := lnwire.NewShortChanIDFromInt(channel.ChannelID)

		// Calculate the number of confirmations the channel has. We
		// do not need to check whether channel height >= current
		// height because we are working with already open channels.
		// If the funding transaction is in the current block, it is
		// considered to have one confirmation, so we add one to the
		// current height to reflect this.
		confirmations := (height + 1) - shortID.BlockHeight

		// Create a channel insight for the channel.
		channelInsight := &ChannelInfo{
			ChannelPoint:  channel.ChannelPoint,
			MonitoredFor:  channel.LifeTime,
			Uptime:        channel.Uptime,
			Confirmations: confirmations,
			Private:       channel.Private,
		}

		// If the channel is not present in the revenue report, it has
		// not generated any revenue over the period so we can add it
		// to our set of insights and proceed to the next channel.
		reports, ok := cfg.RevenueReport.ChannelPairs[channel.ChannelPoint]
		if !ok {
			insights = append(insights, channelInsight)
			continue
		}

		// Accumulate revenue totals for the channel.
		for _, rev := range reports {
			channelInsight.VolumeIncoming += rev.AmountIncoming
			channelInsight.VolumeOutgoing += rev.AmountOutgoing

			// We spilt fees evenly between the channels, so that
			// we do not double count fees.
			channelInsight.FeesEarned +=
				(rev.FeesOutgoing + rev.FeesIncoming) / 2
		}

		insights = append(insights, channelInsight)
	}

	return insights, nil
}
