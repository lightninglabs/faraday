package chanevents

import (
	"context"
	"testing"
	"time"

	"github.com/btcsuite/btcd/btcutil"
	"github.com/lightningnetwork/lnd/fn/v2"
	"github.com/stretchr/testify/require"
)

// nextEventID is incremented by newEvent so every synthetic event carries a
// unique ID, making assertion failures easier to trace.
var nextEventID int64 = 1

// newEvent returns an update-typed ChannelEvent with the given balances. The
// ID is auto-incremented so failing assertions can identify the offending row.
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

// newStatusEvent returns an Online or Offline event with no balance payload,
// matching the schema contract for non-Update event types.
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
	// in the self-pair row. The merge must yield each element twice.
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

// TestCalculateBothDirectionsUptime verifies that the bidirectional uptime
// walk correctly attributes effective uptime given varying liveness, balance
// changes, and forwarding amounts. Each table row pins one boundary condition
// of the (A→B) direction. Full bidirectional invariants are covered by
// dedicated tests.
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
			// The forward in successAmts updates both channels:
			// the in-channel's remoteBalance drops by the amount
			// (peer A spent it) and the out-channel's localBalance
			// drops by the same (forwarded out to B). Both Update
			// events fire at the forward's timestamp. The threshold
			// straddles pre and post liquidity so the boundary at
			// the forward time is what carves the uptime window.
			// The later tests in here aren't causally consistent
			// with the successAmts, which in general is the source
			// of truth for forwards.
			name: "Forward drives the balance timeline",
			inStates: map[int64]*channelState{
				chanInID: {
					online:        true,
					remoteBalance: 1500,
				},
			},
			outStates: map[int64]*channelState{
				chanOutID: {
					online:       true,
					localBalance: 1000,
				},
			},
			inEvents: []*ChannelEvent{
				// Forward of 200 sats at t=150 on the in side:
				// local 0 → 200, remote 1500 → 1300.
				newEvent(
					chanInID, 150, EventTypeUpdate, 200,
					1300,
				),
			},
			outEvents: []*ChannelEvent{
				// Same forward on the out side: local 1000 →
				// 800, remote 0 → 200.
				newEvent(
					chanOutID, 150, EventTypeUpdate, 800,
					200,
				),
			},
			successAmts: []btcutil.Amount{
				200,
			},
			thresholdAmount: 900,
			// t=100..150 (50s): liq = min(1500, 1000) = 1000
			//                   > 900 → qualifies.
			// t=150..200 (50s): liq = min(1300, 800)  = 800
			//                   < 900 → drops out.
			// Total uptime = 50s, total amount = 200 sats.
			expected: &ForwardingAbility{
				Velocity:       4, // 200 sats / 50s
				UptimeFraction: 0.5,
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
			// The total effective uptime is 100s, because the
			// liquidity threshold is low.
			expected: &ForwardingAbility{
				Velocity:       1, // 100 sats / 100s
				UptimeFraction: 1,
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
			// We don't have initial balance states, so we can't
			// determine liquidity until we see an event on both
			// channels. At t=140 we know the liquidity is 800, and
			// it's online for the remaining 60s of the 100s total.
			// So uptime fraction is 0.6 for 800.
			expected: &ForwardingAbility{
				// 100 sats / 60s
				Velocity:       1.6666666666666667,
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
			thresholdAmount: 900,
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
			name: "Self route multiple channels",
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
			name: "Zero uptime with forwards is flagged inconsistent",
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
				Velocity:       0,
				UptimeFraction: 0,
				Inconsistent:   true,
			},
		},
	}

	for _, tc := range testCases {
		t.Run(
			tc.name,
			func(t *testing.T) {
				t.Parallel()

				var totalSuccessfulAmount btcutil.Amount
				for _, amt := range tc.successAmts {
					totalSuccessfulAmount += amt
				}

				mergedEvents := mergeEventSlices(
					tc.inEvents, tc.outEvents,
				)

				inputsAB := pairInputs{
					threshold:             tc.thresholdAmount,
					totalSuccessfulAmount: totalSuccessfulAmount,
				}

				// The (B→A) inputs are not asserted by this
				// test. Pass zero so the second ability is
				// well-defined but ignored.
				var inputsBA pairInputs

				// Precompute the starting balances.
				var sumARemote, sumALocal, sumBRemote,
					sumBLocal btcutil.Amount

				for _, s := range tc.inStates {
					if s.online {
						sumARemote += s.remoteBalance
						sumALocal += s.localBalance
					}
				}
				for _, s := range tc.outStates {
					if s.online {
						sumBRemote += s.remoteBalance
						sumBLocal += s.localBalance
					}
				}

				abilityAB, _, err :=
					calculateBothDirectionsUptime(
						context.Background(),
						startTime, endTime,
						inputsAB, inputsBA,
						tc.inStates, tc.outStates,
						sumARemote, sumALocal,
						sumBRemote, sumBLocal,
						mergedEvents,
					)
				require.NoError(t, err)
				require.Equal(t, tc.expected, abilityAB)
			},
		)
	}
}

// TestCalculateBothDirectionsUptimeAsymmetric pins that the two accumulators
// are independent: with liquidity that crosses only the A→B threshold, the B→A
// direction must report zero uptime regardless of how high the A→B side scores.
func TestCalculateBothDirectionsUptimeAsymmetric(t *testing.T) {
	t.Parallel()

	var (
		chanInID  int64 = 1
		chanOutID int64 = 2
		startTime       = time.Unix(100, 0)
		endTime         = time.Unix(200, 0)
	)

	// A holds inbound liquidity (remoteBalance high). B holds outbound
	// liquidity (localBalance high). The situation favours A→B and starves
	// B→A.
	statesA := map[int64]*channelState{
		chanInID: {
			online:        true,
			remoteBalance: 1000,
			localBalance:  100,
		},
	}
	statesB := map[int64]*channelState{
		chanOutID: {
			online:        true,
			localBalance:  1000,
			remoteBalance: 100,
		},
	}

	// Threshold sits between the two directions: A→B has min(1000, 1000)
	// = 1000 ≥ 500 (qualifying); B→A has min(100, 100) = 100 < 500 (not
	// qualifying).
	inputsAB := pairInputs{
		threshold:             500,
		totalSuccessfulAmount: 100,
	}
	inputsBA := pairInputs{
		threshold:             500,
		totalSuccessfulAmount: 50,
	}

	// Precompute the starting balances.
	var sumARemote, sumALocal, sumBRemote, sumBLocal btcutil.Amount
	for _, s := range statesA {
		if s.online {
			sumARemote += s.remoteBalance
			sumALocal += s.localBalance
		}
	}
	for _, s := range statesB {
		if s.online {
			sumBRemote += s.remoteBalance
			sumBLocal += s.localBalance
		}
	}

	abilityAB, abilityBA, err := calculateBothDirectionsUptime(
		context.Background(), startTime, endTime,
		inputsAB, inputsBA, statesA, statesB,
		sumARemote, sumALocal, sumBRemote, sumBLocal,
		mergeEventSlices(nil, nil),
	)
	require.NoError(t, err)

	require.Equal(
		t, &ForwardingAbility{
			Velocity:       1, // 100 sats / 100s
			UptimeFraction: 1.0,
		}, abilityAB,
	)
	require.Equal(
		t, &ForwardingAbility{
			Velocity:       0,
			UptimeFraction: 0,
			// Forwards landed but BA never crossed threshold.
			Inconsistent: true,
		}, abilityBA,
	)
}
