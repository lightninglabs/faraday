package itest

import (
	"context"
	"testing"
	"time"

	"github.com/btcsuite/btcd/btcutil"
	"github.com/lightninglabs/faraday/frdrpc"
	"github.com/lightninglabs/lndclient"
	"github.com/lightningnetwork/lnd/lnrpc"
	"github.com/lightningnetwork/lnd/lnwire"
	"github.com/stretchr/testify/require"
)

// TestGetChannelEvents tests the GetChannelEvents rpc endpoint.
func TestGetChannelEvents(t *testing.T) {
	c := newTestContext(t)
	defer c.stop()

	ctx := context.Background()

	// We will start by opening a channel from alice to bob.
	var aliceChannelAmt = btcutil.Amount(500000)

	err := c.aliceClient.Client.Connect(
		ctx, c.bobPubkey, "localhost:10012", true,
	)
	require.NoError(c.t, err, "could not connect nodes")

	aliceChannel, _ := c.openChannel(
		c.aliceClient.Client, c.bobPubkey, aliceChannelAmt,
	)

	// Wait until alice can route a payment to bob through the new channel.
	var paymentAmount lnwire.MilliSatoshi = 20000000
	c.eventuallyf(func() bool {
		return c.channelRoutable(c.bobPubkey, paymentAmount)
	}, "channel did not become routable")

	// Now we'll send a payment from alice to bob to generate a balance
	// update event.
	hash, payreq := c.addInvoice(c.bobClient.Client, paymentAmount)
	c.makePayment(
		c.aliceClient.LndServices, c.bobClient.LndServices,
		lndclient.SendPaymentRequest{
			Invoice:     payreq,
			PaymentHash: &hash,
			Timeout:     paymentTimeout,
		}, lnrpc.Payment_SUCCEEDED,
	)

	// We now close the channel to generate an offline event.
	c.closeChannel(c.aliceClient.Client, aliceChannel, true)

	// We'll query for all events and then check that we have at least
	// these three. It's possible that there are more events due to lnd's
	// internal workings, so we won't assert the exact count.
	events, err := c.faradayClient.GetChannelEvents(
		ctx, &frdrpc.ChannelEventsRequest{
			ChanPoint: aliceChannel.String(),
			EndTime:   uint64(time.Now().Unix()) + 1,
		},
	)
	require.NoError(c.t, err, "could not get channel events")

	// Check that we have the expected event types.
	var (
		onlineEvents  int
		updateEvents  int
		offlineEvents int
	)

	for _, event := range events.Events {
		switch event.EventType {
		case frdrpc.ChannelEventType_CHAN_EVENT_ONLINE:
			onlineEvents++

		case frdrpc.ChannelEventType_CHAN_EVENT_UPDATE:
			updateEvents++

		case frdrpc.ChannelEventType_CHAN_EVENT_OFFLINE:
			offlineEvents++
		}
	}

	// We expect to see these events for this channel:
	// 1. Channel Open: online, update (initial balance)
	// 2. Channel Active: online
	// 3. Payment sent: two updates (update_add, update_fulfill)
	// 4. Channel Offline: offline
	// 5. Channel Close: offline
	//
	require.Len(t, events.Events, 7)
	require.Equal(t, 2, onlineEvents)
	require.Equal(t, 3, updateEvents)
	require.Equal(t, 2, offlineEvents)
}
