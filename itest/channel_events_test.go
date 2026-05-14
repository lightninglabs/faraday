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

// TestForwardingAbility verifies, end-to-end, that a regtest payment surfaces
// over the ForwardingAbility RPC as the expected per-pair velocity and uptime
// fraction.
func TestForwardingAbility(t *testing.T) {
	c := newTestContext(t)
	defer c.stop()

	ctx := context.Background()

	var aliceChannelAmt = btcutil.Amount(500000)

	err := c.aliceClient.Client.Connect(
		ctx, c.bobPubkey, "localhost:10012", true,
	)
	require.NoError(c.t, err, "could not connect nodes")

	// Opening the channel seeds the chanevents pipeline with the online and
	// update events that the assertions below count on.
	aliceChannel, _ := c.openChannel(
		c.aliceClient.Client, c.bobPubkey, aliceChannelAmt,
	)

	// TODO: wait for the channel open to be ready with lntest.
	time.Sleep(3 * time.Second)

	// Wait for channel events to be processed. TODO: replace with sync.Wait
	// once we have lntest.
	assertEvents := func(expected int) {
		var events *frdrpc.ChannelEventsResponse
		var err error
		for range 10 {
			events, err = c.faradayClient.GetChannelEvents(
				ctx, &frdrpc.ChannelEventsRequest{
					ChanPoint: aliceChannel.String(),
					EndTime: time.
						Now().
						Add(time.Second).
						Unix(),
				},
			)
			require.NoError(
				c.t, err, "could not get channel events",
			)

			if len(events.Events) == expected {
				t.Logf("Found events %v", events.Events)

				return
			}
			time.Sleep(500 * time.Millisecond)
		}
		require.Failf(
			c.t, "fail", "expected exactly %d events for channel "+
				"%s, have %d", expected, aliceChannel.String(),
			len(events.Events),
		)
	}

	// Wait for the chanevents pipeline to record the channel-open events
	// (online, update, online) before sampling the forwarding window.
	assertEvents(3)

	// Open the forwarding-ability window before shifting balance. The first
	// half has no remote balance and therefore no routable liquidity.
	// Combined with the equal-length wait after the payment below, this
	// yields a ~50% uptime fraction.
	startTime := time.Now()
	time.Sleep(4 * time.Second)

	// A payment from Alice to Bob shifts liquidity so that the channel has
	// remote balance and circular forwarding becomes possible.
	var paymentAmtMsat lnwire.MilliSatoshi = 250000 * 1000
	hash, payreq := c.addInvoice(c.bobClient.Client, paymentAmtMsat)
	c.makePayment(
		c.aliceClient.LndServices, c.bobClient.LndServices,
		lndclient.SendPaymentRequest{
			Invoice:     payreq,
			PaymentHash: &hash,
			Timeout:     paymentTimeout,
		},
		lnrpc.Payment_SUCCEEDED,
	)

	// The payment adds two update events, one for the add and one for the
	// settle, on top of the three open events recorded earlier.
	assertEvents(5)

	// Mirror the pre-payment wait so the unroutable and routable halves of
	// the window are equal-length.
	time.Sleep(4 * time.Second)

	endTime := time.Now()

	abilities, err := c.faradayClient.ForwardingAbility(
		ctx, &frdrpc.ForwardingAbilityRequest{
			StartTime:       uint64(startTime.Unix()),
			EndTime:         uint64(endTime.Unix()),
			ThresholdAmtSat: 1,
		},
	)
	require.NoError(c.t, err, "could not get forwarding ability")

	// We should only have a single pair: Bob -> Bob (circular).
	require.Len(t, abilities.Pairs, 1)

	ability := abilities.Pairs[0]
	require.Equal(t, c.bobPubkey.String(), ability.PeerIn)
	require.Equal(t, c.bobPubkey.String(), ability.PeerOut)

	// We expect a zero velocity since no forwards occurred for this pair,
	// but a non-zero uptime fraction since there was a period where
	// circular forwarding was possible.
	require.Equal(t, 0.0, ability.Ability.Velocity)
	require.InDelta(
		t, 0.5, ability.Ability.UptimeFraction, 0.1,
	)
}
