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
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
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

// TestForwardingAbility integration test opens a channel, sends payments to
// seed events, and verifies that calling the ForwardingAbility RPC returns
// the peer pair analytics successfully and can be decoded.
func TestForwardingAbility(t *testing.T) {
	c := newTestContext(t)
	defer c.stop()

	ctx := context.Background()

	// Connect nodes and open a channel from alice to bob.
	var aliceChannelAmt = btcutil.Amount(500000)

	err := c.aliceClient.Client.Connect(
		ctx, c.bobPubkey, "localhost:10012", true,
	)
	require.NoError(c.t, err, "could not connect nodes")

	_, _ = c.openChannel(
		c.aliceClient.Client, c.bobPubkey, aliceChannelAmt,
	)

	// Wait until alice can route a payment to bob.
	var paymentAmount lnwire.MilliSatoshi = 20000000
	c.eventuallyf(func() bool {
		return c.channelRoutable(c.bobPubkey, paymentAmount)
	}, "channel did not become routable")

	// Send a payment from alice to bob.
	hash, payreq := c.addInvoice(c.bobClient.Client, paymentAmount)
	c.makePayment(
		c.aliceClient.LndServices, c.bobClient.LndServices,
		lndclient.SendPaymentRequest{
			Invoice:     payreq,
			PaymentHash: &hash,
			Timeout:     paymentTimeout,
		}, lnrpc.Payment_SUCCEEDED,
	)

	// The alice->bob payment moved liquidity onto bob's side of the only
	// channel, so from this point on the bob self-pair holds at least the
	// requested floor. Measuring over a window that starts now keeps the
	// pair's uptime fraction high, which both clears the uptime threshold
	// and keeps the node-down guard satisfied. A future end time extends
	// the window over the still-funded state.
	bobHex := c.bobPubkey.String()
	windowStart := time.Now()

	// The events store ingests channel updates asynchronously, so retry
	// until the bob self-pair surfaces.
	var ability frdrpc.ForwardingAbility
	c.eventuallyf(func() bool {
		endTime := time.Now().Add(2 * time.Second).Unix()
		resp, err := c.faradayClient.ForwardingAbility(
			ctx, &frdrpc.ForwardingAbilityRequest{
				StartTime:         uint64(windowStart.Unix()),
				EndTime:           uint64(endTime),
				LiquidityFloorSat: 1000,
				UptimeThreshold:   0.1,
			},
		)
		if err != nil {
			return false
		}

		decoded, err := frdrpc.DecodeForwardingAbility(resp)
		if err != nil {
			return false
		}

		a, ok := decoded[bobHex][bobHex]
		if !ok {
			return false
		}
		ability = a

		return true
	}, "expected bob self-pair in forwarding ability")

	// The bob self-pair was up but never forwarded through itself, so it
	// surfaces via the up-but-idle bitmask: non-zero effective uptime and
	// no forwards, hence neither volume nor fees.
	require.Greater(c.t, ability.EffectiveUptimeS, uint64(0))
	require.Zero(c.t, ability.Forwards)
	require.Zero(c.t, ability.ForwardedMsat)
	require.Zero(c.t, ability.FeeMsat)
}

// TestForwardingDowntime exercises the offline/online plumbing end to end. It
// disconnects the only channel peer to take the channel offline, asserts that
// faraday records the resulting offline event, then reconnects the peer and
// asserts the recovering online event lands and the bob self-pair surfaces in
// ForwardingAbility again. This proves downtime and recovery flow through to
// the analyzer; the exact per-second uptime math is covered deterministically
// by the analyzer unit tests.
func TestForwardingDowntime(t *testing.T) {
	c := newTestContext(t)
	defer c.stop()

	ctx := context.Background()

	// Connect nodes and open a channel from alice to bob.
	var aliceChannelAmt = btcutil.Amount(500000)

	err := c.aliceClient.Client.Connect(
		ctx, c.bobPubkey, "localhost:10012", true,
	)
	require.NoError(c.t, err, "could not connect nodes")

	aliceChannel, _ := c.openChannel(
		c.aliceClient.Client, c.bobPubkey, aliceChannelAmt,
	)

	// Wait until alice can route a payment to bob.
	var paymentAmount lnwire.MilliSatoshi = 20000000
	c.eventuallyf(func() bool {
		return c.channelRoutable(c.bobPubkey, paymentAmount)
	}, "channel did not become routable")

	// Move liquidity onto bob's side so the bob self-pair clears the
	// liquidity floor while the channel is up.
	hash, payreq := c.addInvoice(c.bobClient.Client, paymentAmount)
	c.makePayment(
		c.aliceClient.LndServices, c.bobClient.LndServices,
		lndclient.SendPaymentRequest{
			Invoice:     payreq,
			PaymentHash: &hash,
			Timeout:     paymentTimeout,
		}, lnrpc.Payment_SUCCEEDED,
	)

	chanPoint := aliceChannel.String()
	bobHex := c.bobPubkey.String()

	// Snapshot the event counts before the disconnect so we can detect the
	// new offline and online events the disconnect and recovery produce.
	onlineBefore, offlineBefore := c.channelEventCounts(chanPoint)

	// Disconnect bob to take the only channel offline.
	c.disconnectPeer(c.aliceClient, c.bobPubkey)

	// faraday should ingest the resulting offline event: this is the
	// downtime signal that the channel went inactive.
	c.eventuallyf(func() bool {
		_, offline := c.channelEventCounts(chanPoint)
		return offline > offlineBefore
	}, "expected an offline event after disconnect")

	// An explicit DisconnectPeer is sticky: lnd does not auto-reconnect, so
	// the channel stays offline until we reconnect. A window that sits
	// entirely in this offline period leaves no pair clearing the uptime
	// threshold, so the node-down guard rejects the request with
	// FailedPrecondition rather than returning an empty response. The
	// threshold is irrelevant here since the only pair has zero uptime.
	c.eventuallyf(func() bool {
		now := time.Now()
		_, err := c.faradayClient.ForwardingAbility(
			ctx, &frdrpc.ForwardingAbilityRequest{
				StartTime: uint64(now.Unix()),
				EndTime: uint64(
					now.Add(2 * time.Second).Unix(),
				),
				LiquidityFloorSat: 1000,
				UptimeThreshold:   0.9,
			},
		)

		return status.Code(err) == codes.FailedPrecondition
	}, "expected node-down guard while bob is disconnected")

	// An explicit DisconnectPeer drops lnd's persistent connection, so the
	// channel only comes back up once we reconnect. Reconnect bob to bring
	// the channel active again.
	err = c.aliceClient.Client.Connect(
		ctx, c.bobPubkey, "localhost:10012", true,
	)
	require.NoError(c.t, err, "could not reconnect nodes")

	// The channel goes active again on reconnect, which faraday records as
	// an online event.
	c.eventuallyf(func() bool {
		online, _ := c.channelEventCounts(chanPoint)
		return online > onlineBefore
	}, "expected an online event after reconnect")

	// With the channel back up and liquidity still on bob's side, the bob
	// self-pair surfaces in ForwardingAbility again over a fresh window
	// that opens after recovery.
	c.eventuallyf(func() bool {
		now := time.Now()
		resp, err := c.faradayClient.ForwardingAbility(
			ctx, &frdrpc.ForwardingAbilityRequest{
				StartTime: uint64(now.Unix()),
				EndTime: uint64(
					now.Add(2 * time.Second).Unix(),
				),
				LiquidityFloorSat: 1000,
				UptimeThreshold:   0.1,
			},
		)
		if err != nil {
			return false
		}

		decoded, err := frdrpc.DecodeForwardingAbility(resp)
		if err != nil {
			return false
		}

		_, ok := decoded[bobHex][bobHex]

		return ok
	}, "expected bob self-pair after reconnect")
}

// TestChannelEventsPruning verifies that starting Faraday with low size limits
// (e.g. max-events=1 and retention=2s) executes live background pruning
// successfully and bounds the database size correctly.
func TestChannelEventsPruning(t *testing.T) {
	c := newTestContext(
		t, "--chanevents.max-events=1", "--chanevents.retention=2s",
	)
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

	// Use a far-future end time so the query window never excludes a stored
	// event on a slow host. A tight wall-clock window here would make the
	// counts below racy.
	endTime := time.Now().Add(time.Hour).Unix()

	// We deliberately do not assert on the initial event count here: opening
	// a channel records several events, but the 2-second background prune can
	// fire before we observe them on a slow host, so any such pre-prune
	// assertion would be flaky. The eventuallyf checks below verify the
	// pruning behaviour directly instead.

	// Wait for the live background pruning ticker to bound the table to the
	// max-events ceiling. We assert at most one event rather than exactly
	// one: the size limit keeps a single event, but the 2-second retention
	// limit then ages it out since no new events follow the channel open,
	// so the steady state is zero or one.
	var eventsAfter *frdrpc.ChannelEventsResponse
	c.eventuallyf(func() bool {
		var err error
		eventsAfter, err = c.faradayClient.GetChannelEvents(
			ctx, &frdrpc.ChannelEventsRequest{
				ChanPoint: aliceChannel.String(),
				EndTime:   endTime,
			},
		)
		if err != nil {
			return false
		}
		return len(eventsAfter.Events) <= 1
	}, "expected channel events to be pruned down to at most one in the "+
		"background")

	// No further events follow the channel open, so once the remaining
	// event ages past the 2-second retention window the age-based prune
	// removes it too, draining the table to zero.
	c.eventuallyf(func() bool {
		eventsAfter, err := c.faradayClient.GetChannelEvents(
			ctx, &frdrpc.ChannelEventsRequest{
				ChanPoint: aliceChannel.String(),
				EndTime:   endTime,
			},
		)
		if err != nil {
			return false
		}
		return len(eventsAfter.Events) == 0
	}, "expected channel events to be pruned down to zero once all events "+
		"age out of the retention window")
}
