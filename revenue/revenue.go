package revenue

import (
	"github.com/lightninglabs/lndclient"
	"github.com/lightningnetwork/lnd/lnwire"
)

// Config contains all the functions required to calculate revenue.
type Config struct {
	// ListChannels returns all open, public channels.
	ListChannels func() ([]lndclient.ChannelInfo, error)

	// ClosedChannels returns all closed channels.
	ClosedChannels func() ([]lndclient.ClosedChannel, error)

	// ForwardingHistory returns paginated forwarding history results.
	// The period that these results queried over determines the period
	// that the report is generated for.
	ForwardingHistory func() ([]lndclient.ForwardingEvent, error)
}

// GetRevenueReport produces a revenue report over the period specified.
func GetRevenueReport(cfg *Config) (*Report, error) {
	// To provide the user with a revenue report by outpoint, we need to map
	// short channel ids in the forwarding log to outpoints. Lookup all open
	// and closed channels to produce a map of short channel id to outpoint.
	channels, err := cfg.ListChannels()
	if err != nil {
		return nil, err
	}

	closedChannels, err := cfg.ClosedChannels()
	if err != nil {
		return nil, err
	}

	// Add the channels looked up to a map of short channel id to outpoint
	// string.
	channelIDs := make(map[lnwire.ShortChannelID]string)
	for _, channel := range channels {
		id := lnwire.NewShortChanIDFromInt(channel.ChannelID)
		channelIDs[id] = channel.ChannelPoint
	}

	for _, closedChannel := range closedChannels {
		id := lnwire.NewShortChanIDFromInt(closedChannel.ChannelID)
		channelIDs[id] = closedChannel.ChannelPoint
	}

	fwds, err := cfg.ForwardingHistory()
	if err != nil {
		return nil, err
	}

	events := getRevenueEvents(channelIDs, fwds)
	return getReport(events), nil
}

// getRevenueEvents produces a set of revenue events from a set of forwarding
// events that can be used to create a report.
func getRevenueEvents(channelIDs map[lnwire.ShortChannelID]string,
	fwdEvents []lndclient.ForwardingEvent) []revenueEvent {

	// Do not prealloc because we may not add events if we cannot get their
	// channel ID from the channelIDs map.
	// nolint: prealloc
	var events []revenueEvent

	// Get the event's channel outpoints from out known list of maps and
	// create a revenue event. Return an error if the short channel id's
	// outpoint cannot be found, because we expect all known short channel
	// ids to be provided.
	for _, fwd := range fwdEvents {
		shortChanIn := lnwire.NewShortChanIDFromInt(
			fwd.ChannelIn,
		)
		shortChanOut := lnwire.NewShortChanIDFromInt(
			fwd.ChannelOut,
		)

		incoming, ok := channelIDs[shortChanIn]
		if !ok {
			log.Errorf("cannot find channel "+
				"incoming outpoint for forward: %v(%v "+
				"msat) -> %v(%v msat)", shortChanIn,
				fwd.AmountMsatIn, shortChanOut,
				fwd.AmountMsatOut)

			continue
		}

		outgoing, ok := channelIDs[shortChanOut]
		if !ok {
			log.Errorf("cannot find channel "+
				"outgoing outpoint for forward: %v(%v "+
				"msat) -> %v(%v msat)", shortChanIn,
				fwd.AmountMsatIn, shortChanOut,
				fwd.AmountMsatOut)

			continue
		}

		events = append(events, revenueEvent{
			incomingChannel: incoming,
			outgoingChannel: outgoing,
			incomingAmt:     fwd.AmountMsatIn,
			outgoingAmt:     fwd.AmountMsatOut,
		})
	}

	return events
}

// Report provides a pairwise report on channel revenue. It maps a
// target channel to a map of channels that it has forwarded HTLCs with to
// a record of the revenue produced by each channel in the map. These revenue
// records report revenue and volume with direction relative to the target
// channel.
type Report struct {
	// ChannelPairs contains a map of the string representation of a channel's
	// outpoint to a map of pair channels with which it has generated revenue.
	ChannelPairs map[string]map[string]Revenue
}

// Revenue describes the volume of forwards that a channel has been a part of
// and fees that a channel has generated as a result forward events. Volume
// and fee are reported by direction, where incoming means that the forward
// arrived at our node via the channel, and outgoing meaning that the forward
// left our node via the channel.
type Revenue struct {
	// AmountOutgoing is the amount in msat that was sent out over the channel
	// as part of forwards with its peer channel.
	AmountOutgoing lnwire.MilliSatoshi

	// AmountIncoming is the amount in msat that arrived on the channel to be
	// forwarded onwards by the peer channel.
	AmountIncoming lnwire.MilliSatoshi

	// FeesOutgoing is the amount in msat of fees that we attribute to the
	// channel for its role as the outgoing channel in forwards.
	FeesOutgoing lnwire.MilliSatoshi

	// FeesIncoming is the amount in msat of fees that we attribute to the
	// channel for its role as the incoming channel in forwards.
	FeesIncoming lnwire.MilliSatoshi
}

// getRevenue gets a revenue record for a given target channel and its
// forwarding pair. If map entries do not exist at any stage, they are created.
func (r Report) getRevenue(targetChan, pairChan string) Revenue {
	// If we do not have an entry in our revenue report for the target channel
	// create one.
	record, ok := r.ChannelPairs[targetChan]
	if !ok {
		record = make(map[string]Revenue)
		r.ChannelPairs[targetChan] = record
	}

	// Get the revenue we have with the pair channel, if there is no revenue
	// record, return an empty one.
	revenue, ok := record[pairChan]
	if !ok {
		return Revenue{}
	}

	return revenue
}

// setRevenue sets the revenue value for a target channel and its forwarding
// pair. This function expects both maps to be initialized.
func (r Report) setRevenue(targetChan, pairChan string,
	revenue Revenue) {

	r.ChannelPairs[targetChan][pairChan] = revenue
}

// addIncoming gets the existing revenue record for the incoming channel with
// the outgoing channel, adds the incoming volume and fees and updates the
// Revenue Report.
func (r Report) addIncoming(incomingChannel,
	outgoingChannel string, amount, fees lnwire.MilliSatoshi) {

	revenue := r.getRevenue(incomingChannel, outgoingChannel)

	// Add the fees and revenue that have been earned to the existing revenue
	// record.
	revenue.AmountIncoming += amount
	revenue.FeesIncoming += fees

	// Set the new revenue record in the revenue report.
	r.setRevenue(incomingChannel, outgoingChannel, revenue)
}

// addOutgoing gets the existing revenue record for the outgoing channel with
// the incoming channel, adds the outgoing volume and fees and updates the
// Revenue Report.
func (r Report) addOutgoing(outgoingChannel,
	incomingChannel string, amount, fees lnwire.MilliSatoshi) {

	revenue := r.getRevenue(outgoingChannel, incomingChannel)

	// Add the fees and revenue that have been earned to the existing revenue
	// record.
	revenue.AmountOutgoing += amount
	revenue.FeesOutgoing += fees

	// Set the new revenue record in the revenue report.
	r.setRevenue(outgoingChannel, incomingChannel, revenue)
}

// revenueEvent provides the information captured by ForwardingEvents with
// channel outpoint strings rather than short channel ids.
type revenueEvent struct {
	incomingChannel string
	outgoingChannel string
	incomingAmt     lnwire.MilliSatoshi
	outgoingAmt     lnwire.MilliSatoshi
}

// getReport creates a revenue report for the set of events provided. It
// takes an attribute incoming float which determines the fee split between
// incoming and outgoing channels.
func getReport(events []revenueEvent) *Report {
	report := &Report{
		ChannelPairs: make(map[string]map[string]Revenue),
	}

	for _, event := range events {
		// Calculate total fees earned for this event.
		fee := event.incomingAmt - event.outgoingAmt

		// Calculate fees earned by the incoming channel in this event.
		fees := lnwire.MilliSatoshi(float64(fee))

		// Update the revenue record for the incoming channel.
		report.addIncoming(event.incomingChannel, event.outgoingChannel,
			event.incomingAmt, fees)

		// Update the revenue record for the downstream channel.
		report.addOutgoing(event.outgoingChannel, event.incomingChannel,
			event.outgoingAmt, fees)
	}

	return report
}
