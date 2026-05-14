package chanevents

import (
	"context"
	"math"
	"testing"
	"time"

	"github.com/btcsuite/btcd/btcutil"
	"github.com/lightningnetwork/lnd/clock"
	"github.com/lightningnetwork/lnd/fn/v2"
	"github.com/stretchr/testify/require"
)

// nextEventID is a simple counter to assign unique IDs to events created by the
// test helpers for ease of debugging.
var nextEventID int64 = 1

// newEvent constructs an update-style event with explicit local and remote
// balances and an auto-incremented ID so test failures can point at the
// offending row.
func newEvent(chanID int64, ts int64, eventType EventType, local,
	remote btcutil.Amount) *ChannelEvent {

	id := nextEventID
	nextEventID++

	return &ChannelEvent{
		ID:            id,
		ChannelID:     chanID,
		Timestamp:     time.Unix(ts, 0),
		EventType:     eventType,
		LocalBalance:  fn.Some(local),
		RemoteBalance: fn.Some(remote),
	}
}

// newStatusEvent constructs an event omitting balance information for status
// changes.
func newStatusEvent(chanID int64, ts int64, eventType EventType) *ChannelEvent {
	return &ChannelEvent{
		ChannelID:     chanID,
		Timestamp:     time.Unix(ts, 0),
		EventType:     eventType,
		LocalBalance:  fn.None[btcutil.Amount](),
		RemoteBalance: fn.None[btcutil.Amount](),
	}
}

// TestMergeEventSlices verifies that interleaving distinct or identical streams
// preserves strict chronological order and resolves timestamp collisions
// deterministically.
func TestMergeEventSlices(t *testing.T) {
	t.Parallel()

	const (
		fromA int64 = 1
		fromB int64 = 2
	)

	// selfPair is the same backing slice passed as both sliceA and sliceB
	// in the self-pair row; the merge must yield each element twice.
	selfPair := []*ChannelEvent{
		newStatusEvent(fromA, 100, EventTypeOnline),
		newStatusEvent(fromA, 200, EventTypeOffline),
	}

	testCases := []struct {
		name     string
		sliceA   []*ChannelEvent
		sliceB   []*ChannelEvent
		expected []*ChannelEvent
	}{
		{
			name: "Both empty",
		},
		{
			name: "Only A",
			sliceA: []*ChannelEvent{
				newStatusEvent(fromA, 100, EventTypeOnline),
				newStatusEvent(fromA, 200, EventTypeOffline),
			},
			expected: []*ChannelEvent{
				newStatusEvent(fromA, 100, EventTypeOnline),
				newStatusEvent(fromA, 200, EventTypeOffline),
			},
		},
		{
			name: "Only B",
			sliceB: []*ChannelEvent{
				newStatusEvent(fromB, 100, EventTypeOnline),
				newStatusEvent(fromB, 200, EventTypeOffline),
			},
			expected: []*ChannelEvent{
				newStatusEvent(fromB, 100, EventTypeOnline),
				newStatusEvent(fromB, 200, EventTypeOffline),
			},
		},
		{
			name: "Disjoint A before B",
			sliceA: []*ChannelEvent{
				newStatusEvent(fromA, 100, EventTypeOnline),
				newStatusEvent(fromA, 150, EventTypeOffline),
			},
			sliceB: []*ChannelEvent{
				newStatusEvent(fromB, 200, EventTypeOnline),
				newStatusEvent(fromB, 250, EventTypeOffline),
			},
			expected: []*ChannelEvent{
				newStatusEvent(fromA, 100, EventTypeOnline),
				newStatusEvent(fromA, 150, EventTypeOffline),
				newStatusEvent(fromB, 200, EventTypeOnline),
				newStatusEvent(fromB, 250, EventTypeOffline),
			},
		},
		{
			name: "Interleaved",
			sliceA: []*ChannelEvent{
				newStatusEvent(fromA, 100, EventTypeOnline),
				newStatusEvent(fromA, 300, EventTypeOffline),
			},
			sliceB: []*ChannelEvent{
				newStatusEvent(fromB, 200, EventTypeOnline),
				newStatusEvent(fromB, 400, EventTypeOffline),
			},
			expected: []*ChannelEvent{
				newStatusEvent(fromA, 100, EventTypeOnline),
				newStatusEvent(fromB, 200, EventTypeOnline),
				newStatusEvent(fromA, 300, EventTypeOffline),
				newStatusEvent(fromB, 400, EventTypeOffline),
			},
		},
		{
			name: "Equal timestamps yield A first",
			sliceA: []*ChannelEvent{
				newStatusEvent(fromA, 100, EventTypeOnline),
				newStatusEvent(fromA, 200, EventTypeOffline),
			},
			sliceB: []*ChannelEvent{
				newStatusEvent(fromB, 100, EventTypeOnline),
				newStatusEvent(fromB, 200, EventTypeOffline),
			},
			expected: []*ChannelEvent{
				newStatusEvent(fromA, 100, EventTypeOnline),
				newStatusEvent(fromB, 100, EventTypeOnline),
				newStatusEvent(fromA, 200, EventTypeOffline),
				newStatusEvent(fromB, 200, EventTypeOffline),
			},
		},
		{
			name:   "Self-pair duplicates each event",
			sliceA: selfPair,
			sliceB: selfPair,
			expected: []*ChannelEvent{
				selfPair[0], selfPair[0],
				selfPair[1], selfPair[1],
			},
		},
	}

	for _, tc := range testCases {
		t.Run(
			tc.name,
			func(t *testing.T) {
				t.Parallel()

				var got []*ChannelEvent
				for event, err := range mergeEventSlices(
					tc.sliceA, tc.sliceB,
				) {
					require.NoError(t, err)
					got = append(got, event)
				}
				require.Equal(t, tc.expected, got)
			},
		)
	}
}

// TestMergeEventSlicesEarlyTermination verifies that the merge sequence safely
// halts mid-stream without exhausting inputs when the consumer aborts.
func TestMergeEventSlicesEarlyTermination(t *testing.T) {
	t.Parallel()

	sliceA := []*ChannelEvent{
		newStatusEvent(1, 100, EventTypeOnline),
		newStatusEvent(1, 300, EventTypeOffline),
	}
	sliceB := []*ChannelEvent{
		newStatusEvent(2, 200, EventTypeOnline),
		newStatusEvent(2, 400, EventTypeOffline),
	}

	var got []*ChannelEvent
	for event, err := range mergeEventSlices(sliceA, sliceB) {
		require.NoError(t, err)
		got = append(got, event)
		if len(got) == 2 {
			break
		}
	}
	require.Len(t, got, 2)
}

// TestCalculateBothDirectionsUptime pins the bidirectional walk's
// windowing+threshold logic via its (A→B) result. Each row encodes one
// invariant of how the walk attributes liquidity, accounts for liveness
// changes, and reports velocity. The (B→A) ability is degenerate for these
// inputs (outStates carry no remoteBalance), so the test asserts only the AB
// direction. Bidirectional-only invariants live in tests that exercise both
// directions explicitly.
func TestCalculateBothDirectionsUptime(t *testing.T) {
	t.Parallel()

	var (
		chanInID  int64 = 1
		chanOutID int64 = 2
		startTime       = time.Unix(100, 0)
		endTime         = time.Unix(200, 0)
	)

	testCases := []struct {
		name string

		inStates  map[int64]*channelState
		outStates map[int64]*channelState
		inEvents  []*ChannelEvent
		outEvents []*ChannelEvent

		successAmts       []btcutil.Amount
		thresholdAmount   btcutil.Amount
		forwardPercentile float64

		expected    *ForwardingAbility
		expectedErr string
	}{
		{
			name: "Basic case always online",
			inStates: map[int64]*channelState{
				chanInID: {
					online:        true,
					remoteBalance: 1000,
				},
			},
			outStates: map[int64]*channelState{
				chanOutID: {
					online:       true,
					localBalance: 800,
				},
			},
			successAmts: []btcutil.Amount{
				100,
			},
			expected: &ForwardingAbility{
				Velocity:       1, // 100 sats / 100s
				UptimeFraction: 1.0,
			},
		},
		{
			name: "Channel goes offline",
			inStates: map[int64]*channelState{
				chanInID: {
					online:        true,
					remoteBalance: 1000,
				},
			},
			outStates: map[int64]*channelState{
				chanOutID: {
					online:       true,
					localBalance: 800,
				},
			},
			inEvents: []*ChannelEvent{
				newStatusEvent(chanInID, 150, EventTypeOffline),
			},
			successAmts: []btcutil.Amount{
				100,
			},
			thresholdAmount: 1,
			expected: &ForwardingAbility{
				Velocity:       2, // 100 sats / 50s
				UptimeFraction: 0.5,
			},
		},
		{
			name: "Balance change",
			inStates: map[int64]*channelState{
				chanInID: {
					online:        true,
					remoteBalance: 1000,
				},
			},
			outStates: map[int64]*channelState{
				chanOutID: {
					online:       true,
					localBalance: 800,
				},
			},
			outEvents: []*ChannelEvent{
				newEvent(
					chanOutID, 150, EventTypeUpdate, 1200,
					0,
				),
			},
			successAmts: []btcutil.Amount{
				100,
			},
			thresholdAmount: 1,
			// Balance changes at t=150, so for the first 50s the
			// liquidity is 800, then it's 1000 for the next 50s.
			// The total effective uptime is 100s.
			expected: &ForwardingAbility{
				Velocity:       1, // 100 sats / 100s
				UptimeFraction: 1.0,
			},
		},
		{
			name: "Duplicate event timestamps",
			inStates: map[int64]*channelState{
				chanInID: {
					online:        true,
					remoteBalance: 1000,
				},
			},
			outStates: map[int64]*channelState{
				chanOutID: {
					online:       true,
					localBalance: 800,
				},
			},
			inEvents: []*ChannelEvent{
				newStatusEvent(chanInID, 150, EventTypeOffline),
			},
			outEvents: []*ChannelEvent{
				newEvent(
					chanOutID, 150, EventTypeUpdate, 1200,
					0,
				),
			},
			successAmts: []btcutil.Amount{
				100,
			},
			thresholdAmount: 1,
			// At t=150, two events happen. From t=100 to t=150
			// (50s), liquidity is min(1000, 800) = 800. After
			// t=150, chanIn is offline, so liquidity is 0 for the
			// remaining 50s.
			expected: &ForwardingAbility{
				Velocity:       2, // 100 sats / 50s
				UptimeFraction: 0.5,
			},
		},
		{
			name: "No initial state",
			inStates: map[int64]*channelState{
				chanInID: {
					online: false,
				},
			},
			outStates: map[int64]*channelState{
				chanOutID: {
					online: false,
				},
			},
			inEvents: []*ChannelEvent{
				newEvent(
					chanInID, 120, EventTypeUpdate, 0, 1000,
				),
			},
			outEvents: []*ChannelEvent{
				newEvent(
					chanOutID, 140, EventTypeUpdate, 800, 0,
				),
			},
			successAmts: []btcutil.Amount{
				100,
			},
			thresholdAmount: 1,
			// We don't have initial state, so we can't determine
			// liquidity until we see an event on both channels. At
			// t=140 we know the liquidity is 800, and it's online
			// for the remaining 60s of the 100s total. So uptime
			// fraction is 0.6 for 800.
			expected: &ForwardingAbility{
				Velocity:       1.6666666666666667, // 100 sats / 60s
				UptimeFraction: 0.6,
			},
		},
		{
			name: "Multiple channels for out peer",
			inStates: map[int64]*channelState{
				chanInID: {
					online:        true,
					remoteBalance: 1000,
				},
			},
			outStates: map[int64]*channelState{
				chanOutID: {
					online:       true,
					localBalance: 800,
				},
				3: {
					online:       true,
					localBalance: 500,
				},
			},
			outEvents: []*ChannelEvent{
				newEvent(
					chanOutID, 150, EventTypeUpdate, 1200,
					0,
				),
			},
			successAmts: []btcutil.Amount{
				100,
			},
			thresholdAmount: 1,
			// We expect the liquidity to be the sum of the
			// available balances of the out channels. t=100-150:
			// min(1000, 800 + 500) = 1000 t=150-200: min(1000, 1200
			// + 500) = 1000
			expected: &ForwardingAbility{
				Velocity:       1, // 100 sats / 100s
				UptimeFraction: 1.0,
			},
		},
		{
			name: "Circular payment ability",
			inStates: map[int64]*channelState{
				chanInID: {
					online:       true,
					localBalance: 1000,
				},
			},
			outStates: map[int64]*channelState{
				chanInID: {
					online:       true,
					localBalance: 1000,
				},
			},
			inEvents: []*ChannelEvent{
				newEvent(
					chanInID, 150, EventTypeUpdate, 500,
					500,
				),
			},
			outEvents: []*ChannelEvent{
				newEvent(
					chanInID, 150, EventTypeUpdate, 500,
					500,
				),
			},
			successAmts: []btcutil.Amount{
				100,
			},
			thresholdAmount: 1,
			// For the first 50s, liquidity is min(1000, 0) = 0. For
			// the next 50s, liquidity is min(500, 500) = 500.
			expected: &ForwardingAbility{
				Velocity:       2, // 100 sats / 50s
				UptimeFraction: 0.5,
			},
		},
		{
			name: "Self route state tracking bug",
			inStates: map[int64]*channelState{
				chanInID: {
					online:        true,
					remoteBalance: 1000,
					localBalance:  1000,
				},
				chanOutID: {
					online:        true,
					remoteBalance: 1000,
					localBalance:  1000,
				},
			},
			outStates: map[int64]*channelState{
				chanInID: {
					online:        true,
					remoteBalance: 1000,
					localBalance:  1000,
				},
				chanOutID: {
					online:        true,
					remoteBalance: 1000,
					localBalance:  1000,
				},
			},
			inEvents: []*ChannelEvent{},
			outEvents: []*ChannelEvent{
				// At 150s (midpoint), out channel balance drops
				// to 0.
				newEvent(
					chanOutID, 150, EventTypeUpdate, 0,
					2000,
				),
			},
			successAmts: []btcutil.Amount{
				100,
			},
			thresholdAmount: 1500,
			// Initial fwdLiquidity = min(2000, 2000) = 2000. 2000 >
			// 1500, so first 50s accrue. At t=150, chanOut local
			// drops to 0. outStates total local becomes 1000 (from
			// chanIn). fwdLiquidity = min(2000, 1000) = 1000. 1000
			// is not > 1500, so last 50s do not accrue.
			expected: &ForwardingAbility{
				Velocity:       2, // 100 sats / 50s
				UptimeFraction: 0.5,
			},
		},
		{
			name: "Zero uptime no forwards yields zero velocity",
			inStates: map[int64]*channelState{
				chanInID: {
					online: false,
				},
			},
			outStates: map[int64]*channelState{
				chanOutID: {
					online: false,
				},
			},
			expected: &ForwardingAbility{
				Velocity:       0,
				UptimeFraction: 0,
			},
		},
		{
			name: "Zero uptime with forwards yields +Inf velocity",
			inStates: map[int64]*channelState{
				chanInID: {
					online: false,
				},
			},
			outStates: map[int64]*channelState{
				chanOutID: {
					online: false,
				},
			},
			successAmts: []btcutil.Amount{
				100,
			},
			expected: &ForwardingAbility{
				Velocity:       math.Inf(1),
				UptimeFraction: 0,
			},
		},
	}

	for _, tc := range testCases {
		t.Run(
			tc.name,
			func(t *testing.T) {
				t.Parallel()

				finalThreshold, err := determineThreshold(
					tc.successAmts, tc.forwardPercentile,
					tc.thresholdAmount,
				)
				if tc.expectedErr != "" {
					require.ErrorContains(
						t, err, tc.expectedErr,
					)

					return
				}
				require.NoError(t, err)

				var totalSuccessfulAmount btcutil.Amount
				for _, amt := range tc.successAmts {
					totalSuccessfulAmount += amt
				}

				mergedEvents := mergeEventSlices(
					tc.inEvents, tc.outEvents,
				)

				inputsAB := pairInputs{
					threshold:             finalThreshold,
					totalSuccessfulAmount: totalSuccessfulAmount,
				}
				// (B→A) inputs are not asserted by this test;
				// pass zero so the second ability is
				// well-defined but ignored.
				var inputsBA pairInputs

				abilityAB, _, err := calculateBothDirectionsUptime(
					context.Background(), startTime,
					endTime, inputsAB, inputsBA,
					tc.inStates, tc.outStates, mergedEvents,
				)
				require.NoError(t, err)
				require.Equal(t, tc.expected, abilityAB)
			},
		)
	}
}

// TestInitialStateSameSecond verifies that two update events sharing a
// second-resolution timestamp before startTime do not abort
// getInitialChannelState. SQL selects the latest by (timestamp DESC, id DESC)
// and the analyzer skips older same-timestamp siblings surfaced by the
// follow-up iterator. The retained state must reflect the highest-id duplicate.
func TestInitialStateSameSecond(t *testing.T) {
	t.Parallel()

	clock := clock.NewTestClock(testTime)
	store := NewTestDB(t, clock)
	ctx := context.Background()

	peerID, err := store.AddPeer(ctx, testPubKey)
	require.NoError(t, err)

	channelID, err := store.AddChannel(
		ctx, testChanPoint1, testShortChanID1, peerID,
	)
	require.NoError(t, err)

	// Two update events share the same second-resolution timestamp. The
	// second insert (higher id) is the one the SQL must pick.
	sameTime := testTime.Add(10 * time.Second)
	err = store.AddChannelEvent(
		ctx, &ChannelEvent{
			ChannelID:     channelID,
			EventType:     EventTypeUpdate,
			Timestamp:     sameTime,
			LocalBalance:  fn.Some(btcutil.Amount(100)),
			RemoteBalance: fn.Some(btcutil.Amount(900)),
		},
	)
	require.NoError(t, err)
	err = store.AddChannelEvent(
		ctx, &ChannelEvent{
			ChannelID:     channelID,
			EventType:     EventTypeUpdate,
			Timestamp:     sameTime,
			LocalBalance:  fn.Some(btcutil.Amount(200)),
			RemoteBalance: fn.Some(btcutil.Amount(800)),
		},
	)
	require.NoError(t, err)

	// Construct a bare analyzer; getInitialChannelState only touches the
	// store, so the lnd field can stay zero.
	a := &ForwardingAnalyzer{store: store}

	startTime := sameTime.Add(time.Second)
	state, err := a.getInitialChannelState(ctx, channelID, startTime)
	require.NoError(t, err)
	require.NotNil(t, state)
	require.True(t, state.online, "an update event implies online")
	require.Equal(t, btcutil.Amount(200), state.localBalance)
	require.Equal(t, btcutil.Amount(800), state.remoteBalance)
}
