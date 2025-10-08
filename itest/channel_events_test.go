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

// TestGetChannelEvents pins the GetChannelEvents RPC contract: a regtest
// channel lifecycle surfaces the expected event-type counts, and a paginated
// walk over the same window matches the unpaginated result event-for-event.
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

	endTime := time.Now().Add(time.Second).Unix()

	events, err := c.faradayClient.GetChannelEvents(
		ctx, &frdrpc.ChannelEventsRequest{
			ChanPoint: aliceChannel.String(),
			EndTime:   endTime,
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

	// We expect exactly these events for this channel:
	// 1. Channel Open: online, update (initial balance)
	// 2. Channel Active: online
	// 3. Payment sent: two updates (update_add, update_fulfill)
	// 4. Channel Offline: offline
	// 5. Channel Close: offline
	require.Len(t, events.Events, 7)
	require.Equal(t, 2, onlineEvents)
	require.Equal(t, 3, updateEvents)
	require.Equal(t, 2, offlineEvents)

	// Walk the same window with a small page size and assert the
	// concatenated pages match the unpaginated result. Catches
	// last_id round-trip, has_more termination, and MaxEvents
	// clamping in one pass.
	const pageSize = 2
	var (
		paged  []*frdrpc.ChannelEvent
		lastID int64
		pages  int
	)
	for {
		page, err := c.faradayClient.GetChannelEvents(
			ctx, &frdrpc.ChannelEventsRequest{
				ChanPoint: aliceChannel.String(),
				EndTime:   endTime,
				MaxEvents: pageSize,
				LastId:    lastID,
			},
		)
		require.NoError(c.t, err, "could not get paginated events")

		// Non-final pages must fill exactly pageSize; the final
		// page (HasMore == false) holds the remainder.
		if page.HasMore {
			require.Len(t, page.Events, pageSize,
				"non-final page %d not full", pages)
		} else {
			require.LessOrEqual(t, len(page.Events), pageSize,
				"final page %d exceeds pageSize", pages)
		}

		paged = append(paged, page.Events...)
		pages++
		if !page.HasMore {
			break
		}

		lastID = page.LastId
	}

	// With 7 events and pageSize 2 we expect ceil(7/2) = 4 pages.
	require.Equal(t, 4, pages, "unexpected page count")

	require.Equal(t, len(events.Events), len(paged),
		"paginated and unpaginated counts differ")
	for i, e := range events.Events {
		require.Equal(t, e.Id, paged[i].Id,
			"event order mismatch at index %d", i)
	}
}
