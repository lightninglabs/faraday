package insights

import (
	"time"

	"github.com/lightninglabs/terminator/revenue"
	"github.com/lightningnetwork/lnd/lnrpc"
	"github.com/lightningnetwork/lnd/lnwire"
)

// Channel provides a set of performance metrics for a lightning channel.
type Channel struct {
	// ChannelPoint is the outpoint of the channel's funding transaction.
	ChannelPoint string

	// MonitoredFor is the amount of time the channel's uptime has been
	// monitored by lnd.
	MonitoredFor time.Duration

	// UptimePercentage is the percentage of its monitored time that a
	// channel's remote peer has been online for.
	UptimePercentage float64

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

	// RevenuePerBlock is the average amount of fees that the channel has
	// earned per block that it has been open for.
	RevenuePerBlock lnwire.MilliSatoshi

	// BlocksOpen is the number of blocks that the channel has been open
	// for.
	BlocksOpen uint32

	// Private indicates whether the channel is private.
	Private bool
}

// Config provides insights with everything it needs to obtain channel
// insights.
type Config struct {
	// OpenChannels is the set of currently open channels.
	OpenChannels []*lnrpc.Channel

	// CurrentHeight is a function which returns the current block
	// currentHeight.
	CurrentHeight func() (uint32, error)

	// RevenueReport is a report our channels revenue.
	RevenueReport *revenue.Report
}

// GetChannels returns an array of channel insights.
func GetChannels(cfg *Config) ([]*Channel, error) {
	// Get the current block height.
	height, err := cfg.CurrentHeight()
	if err != nil {
		return nil, err
	}

	insights := make([]*Channel, 0, len(cfg.OpenChannels))
	for _, channel := range cfg.OpenChannels {
		// Get the short channel ID so we can calculate the number of
		// blocks the channel has been open for.
		shortID := lnwire.NewShortChanIDFromInt(channel.ChanId)

		monitored := time.Second * time.Duration(channel.Lifetime)

		// Create a channel insight for the channel.
		channelInsight := &Channel{
			ChannelPoint: channel.ChannelPoint,
			MonitoredFor: monitored,
			BlocksOpen:   height - shortID.BlockHeight,
			Private:      channel.Private,
		}

		// Calculate uptime for non-zero lifetime channels and set the
		// value on the channel insight.
		if channel.Lifetime > 0 {
			channelInsight.UptimePercentage =
				float64(channel.Uptime) / float64(channel.Lifetime)
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

		// Set revenue earned per active block.
		if channelInsight.BlocksOpen > 0 {
			feesPerBlock := float64(channelInsight.FeesEarned) /
				float64(channelInsight.BlocksOpen)

			channelInsight.RevenuePerBlock = lnwire.MilliSatoshi(
				feesPerBlock,
			)
		}

		insights = append(insights, channelInsight)
	}

	return insights, nil
}
