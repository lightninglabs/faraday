package chanevents

import (
	"context"
	"testing"
	"time"

	"github.com/btcsuite/btcd/btcutil"
	"github.com/lightninglabs/lndclient"
	"github.com/lightningnetwork/lnd/clock"
	"github.com/lightningnetwork/lnd/fn/v2"
	"github.com/lightningnetwork/lnd/lnwire"
	"github.com/lightningnetwork/lnd/routing/route"
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

		successAmts    []btcutil.Amount
		liquidityFloor btcutil.Amount

		expectedUptime time.Duration
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
			expectedUptime: 100 * time.Second,
		},
		{
			// Liquidity sits exactly on the floor for the whole
			// window. The "at least" contract counts equality, so
			// the pair accrues the full window (a strict greater-
			// than comparison would wrongly report zero).
			name: "Liquidity exactly at floor qualifies",
			inStates: map[int64]*channelState{
				chanInID: {
					online:        true,
					remoteBalance: 500,
				},
			},
			outStates: map[int64]*channelState{
				chanOutID: {
					online:       true,
					localBalance: 500,
				},
			},
			liquidityFloor: 500,
			expectedUptime: 100 * time.Second,
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
			liquidityFloor: 900,
			// t=100..150 (50s): liq = min(1500, 1000) = 1000
			//                   > 900 → qualifies.
			// t=150..200 (50s): liq = min(1300, 800)  = 800
			//                   < 900 → drops out.
			// Total uptime = 50s, total amount = 200 sats.
			expectedUptime: 50 * time.Second,
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
			liquidityFloor: 1,
			expectedUptime: 50 * time.Second,
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
			liquidityFloor: 1,
			// Balance changes at t=150, so for the first 50s the
			// liquidity is 800, then it's 1000 for the next 50s.
			// The total effective uptime is 100s, because the
			// liquidity threshold is low.
			expectedUptime: 100 * time.Second,
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
			expectedUptime: 50 * time.Second,
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
			liquidityFloor: 1,
			// We don't have initial balance states, so we can't
			// determine liquidity until we see an event on both
			// channels. At t=140 we know the liquidity is 800, and
			// it's online for the remaining 60s of the 100s total.
			// So uptime fraction is 0.6 for 800.
			expectedUptime: 60 * time.Second,
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
			liquidityFloor: 900,
			// We expect the liquidity to be the sum of the
			// available balances of the out channels. t=100-150:
			// min(1000, 800 + 500) = 1000 t=150-200: min(1000, 1200
			// + 500) = 1000
			expectedUptime: 100 * time.Second,
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
			liquidityFloor: 1,
			// For the first 50s, liquidity is min(1000, 0) = 0. For
			// the next 50s, liquidity is min(500, 500) = 500.
			expectedUptime: 50 * time.Second,
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
			liquidityFloor: 1500,
			// Initial fwdLiquidity = min(2000, 2000) = 2000. 2000 >
			// 1500, so first 50s accrue. At t=150, chanOut local
			// drops to 0. outStates total local becomes 1000 (from
			// chanIn). fwdLiquidity = min(2000, 1000) = 1000. 1000
			// is not > 1500, so last 50s do not accrue.
			expectedUptime: 50 * time.Second,
		},
		{
			name: "Zero uptime no forwards",
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
			expectedUptime: 0,
		},
		{
			// Forwards landed but the pair never held qualifying
			// liquidity: zero uptime, yet the forwarded volume is
			// still reported so the consumer keeps the demand
			// signal.
			name: "Zero uptime with forwards retains volume",
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
			expectedUptime: 0,
		},
	}

	for _, tc := range testCases {
		t.Run(
			tc.name,
			func(t *testing.T) {
				t.Parallel()

				forwards := pairForwards{
					forwards: int64(len(tc.successAmts)),
				}
				for _, amt := range tc.successAmts {
					forwards.amountMsat +=
						lnwire.NewMSatFromSatoshis(amt)

					// Two msat of fee per satoshi
					// forwarded, so the fee total is
					// distinct from the volume total and a
					// crossed assignment fails the
					// assertion below.
					forwards.feeMsat += lnwire.MilliSatoshi(
						amt,
					) * 2
				}

				mergedEvents := mergeEventSlices(
					tc.inEvents, tc.outEvents,
				)

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

				// The (B→A) forwarded totals are not asserted
				// by this test; pass the zero value so the
				// second ability is well-defined but ignored.
				abilityAB, _, err :=
					calculateBothDirectionsUptime(
						context.Background(),
						startTime, endTime,
						tc.liquidityFloor,
						tc.inStates, tc.outStates,
						sumARemote, sumALocal,
						sumBRemote, sumBLocal,
						mergedEvents,
						forwards, pairForwards{},
					)
				require.NoError(t, err)
				require.Equal(t, &ForwardingAbility{
					EffectiveUptime: tc.expectedUptime,
					ForwardedMsat:   forwards.amountMsat,
					FeeMsat:         forwards.feeMsat,
					Forwards:        forwards.forwards,
				}, abilityAB)
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

	// The floor sits between the two directions: A→B has min(1000, 1000)
	// = 1000 ≥ 500 (qualifying); B→A has min(100, 100) = 100 < 500 (not
	// qualifying).
	const liquidityFloor btcutil.Amount = 500

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
		liquidityFloor, statesA, statesB,
		sumARemote, sumALocal, sumBRemote, sumBLocal,
		mergeEventSlices(nil, nil),
		pairForwards{amountMsat: 100_000, feeMsat: 200, forwards: 2},
		pairForwards{amountMsat: 50_000, feeMsat: 100, forwards: 1},
	)
	require.NoError(t, err)

	require.Equal(
		t, &ForwardingAbility{
			EffectiveUptime: 100 * time.Second,
			ForwardedMsat:   100_000,
			FeeMsat:         200,
			Forwards:        2,
		}, abilityAB,
	)
	require.Equal(
		t, &ForwardingAbility{
			// Forwards landed but BA never crossed the floor: zero
			// uptime, volume and fees still reported.
			EffectiveUptime: 0,
			ForwardedMsat:   50_000,
			FeeMsat:         100,
			Forwards:        1,
		}, abilityBA,
	)
}

// TestInitialStateSameSecond verifies that when multiple update events share
// the same second-resolution timestamp, getInitialChannelState seeds from the
// most recent one (highest id) rather than aborting or choosing arbitrarily.
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

	// An offline event shares the same second-resolution timestamp with an
	// even higher ID. Replaying this offline event will set the channel
	// state to offline, but the balances from the highest-ID update event
	// must still be retained.
	err = store.AddChannelEvent(
		ctx, &ChannelEvent{
			ChannelID: channelID,
			EventType: EventTypeOffline,
			Timestamp: sameTime,
		},
	)
	require.NoError(t, err)

	// Construct a bare analyzer. getInitialChannelState only touches the
	// store, so the lnd field can stay zero.
	a := &ForwardingAnalyzer{store: store}

	startTime := sameTime.Add(time.Second)
	state, err := a.getInitialChannelState(ctx, startTime, channelID)
	require.NoError(t, err)
	require.NotNil(t, state)
	require.False(t, state.online,
		"replayed offline event sets state offline")
	require.Equal(t, btcutil.Amount(200), state.localBalance)
	require.Equal(t, btcutil.Amount(800), state.remoteBalance)
}

// stubLndChannelClient implements the three lndclient.LightningClient methods
// the analyzer exercises. The embedded interface is nil, so any other method
// panics. Callers must not invoke methods outside the overridden set.
type stubLndChannelClient struct {
	lndclient.LightningClient

	openChannels      []lndclient.ChannelInfo
	closedChannels    []lndclient.ClosedChannel
	forwardingHistory *lndclient.ForwardingHistoryResponse
}

func (s *stubLndChannelClient) ListChannels(_ context.Context, _, _ bool,
	_ ...lndclient.ListChannelsOption) ([]lndclient.ChannelInfo, error) {

	return s.openChannels, nil
}

func (s *stubLndChannelClient) ClosedChannels(_ context.Context) (
	[]lndclient.ClosedChannel, error) {

	return s.closedChannels, nil
}

func (s *stubLndChannelClient) ForwardingHistory(_ context.Context,
	req lndclient.ForwardingHistoryRequest) (
	*lndclient.ForwardingHistoryResponse, error) {

	if s.forwardingHistory == nil {
		return &lndclient.ForwardingHistoryResponse{}, nil
	}

	if req.Offset > 0 {
		return &lndclient.ForwardingHistoryResponse{}, nil
	}

	return s.forwardingHistory, nil
}

// validPubKey1 is the open-channel peer. route.NewVertexFromStr requires a
// 33-byte compressed key (66 hex chars). The pre-existing testPubKey is 65
// chars long and is fine for the store layer, but the lnd survivorship path
// goes through route.NewVertexFromStr so we use a valid pair here.
const (
	validPubKey1 = "028d4c6347426f2e3f5e2b8e4a1c3b9f1c" +
		"4e5d6f7a8b9c0d1e2f3a4b5c6d7e8f9a"
	validPubKey2 = "038d4c6347426f2e3f5e2b8e4a1c3b9f1c" +
		"4e5d6f7a8b9c0d1e2f3a4b5c6d7e8f9a"
)

// TestEffectiveUptimeIncludesClosedChannels exercises the survivorship-bias
// guarantee of EffectiveUptime: a peer whose only channel was closed before the
// analysis window must still appear in the result map. Without merging lnd's
// ClosedChannels into the considered set, the closed-channel peer would be
// invisible to the walk and the fleet's reported uptime would over-state
// reality.
func TestEffectiveUptimeIncludesClosedChannels(t *testing.T) {
	t.Parallel()

	clk := clock.NewTestClock(testTime)
	store := NewTestDB(t, clk)
	ctx := context.Background()

	// Two peers: the open-channel peer and the closed-channel peer.
	openPeerID, err := store.AddPeer(ctx, validPubKey1)
	require.NoError(t, err)
	closedPeerID, err := store.AddPeer(ctx, validPubKey2)
	require.NoError(t, err)

	// Both channels live in the chanevents store. lnd will report the first
	// via ListChannels and the second only via ClosedChannels.
	openScid := testShortChanID1
	closedScid := testShortChanID2
	openChanID, err := store.AddChannel(
		ctx, testChanPoint1, openScid, openPeerID,
	)
	require.NoError(t, err)
	closedChanID, err := store.AddChannel(
		ctx, testChanPoint2, closedScid, closedPeerID,
	)
	require.NoError(t, err)

	// Seed an Update event before startTime for each channel so the
	// initial-state walk has a non-zero baseline. Without a baseline, the
	// closed channel's online state would be false and its presence in the
	// result map would not prove the survivorship code path drove it.
	seedTime := testTime
	for _, chanID := range []int64{openChanID, closedChanID} {
		err = store.AddChannelEvent(
			ctx, &ChannelEvent{
				ChannelID:     chanID,
				EventType:     EventTypeUpdate,
				Timestamp:     seedTime,
				LocalBalance:  fn.Some(btcutil.Amount(1000)),
				RemoteBalance: fn.Some(btcutil.Amount(1000)),
			},
		)
		require.NoError(t, err)
	}

	openVertex, err := route.NewVertexFromStr(validPubKey1)
	require.NoError(t, err)
	closedVertex, err := route.NewVertexFromStr(validPubKey2)
	require.NoError(t, err)

	// lnd reports only the open channel via ListChannels. The closed
	// channel surfaces solely through ClosedChannels.
	stub := &stubLndChannelClient{
		openChannels: []lndclient.ChannelInfo{
			{
				ChannelID:   openScid,
				PubKeyBytes: openVertex,
			},
		},
		closedChannels: []lndclient.ClosedChannel{
			{
				ChannelID:   closedScid,
				PubKeyBytes: closedVertex,
			},
		},
	}

	a := NewForwardingAnalyzer(store, lndclient.LndServices{Client: stub})

	startTime := seedTime.Add(time.Second)
	endTime := startTime.Add(time.Minute)

	abilities, err := a.EffectiveUptime(ctx, startTime, endTime, 0)
	require.NoError(t, err)

	// Cross-pair entries in both directions are the cleanest assertion
	// that the closed-channel peer participates in the walk, not just
	// indexes into it. Their presence is the survivorship guarantee
	// under test: without merging lnd's ClosedChannels into the
	// considered set, neither cross would appear.
	require.Contains(
		t, abilities, PeerPair{PeerIn: validPubKey1, PeerOut: validPubKey2},
		"closed-channel peer absent: survivorship handling skipped",
	)
	require.Contains(
		t, abilities, PeerPair{PeerIn: validPubKey2, PeerOut: validPubKey1},
		"closed-channel peer absent: survivorship handling skipped",
	)
}

// TestEffectiveUptimeArgs exercises the liquidityFloor, startTime, and endTime
// arguments of EffectiveUptime, verifying that the single uniform floor governs
// effective uptime and that forwarded volume is reported regardless of uptime.
func TestEffectiveUptimeArgs(t *testing.T) {
	t.Parallel()

	var (
		clk   = clock.NewTestClock(testTime)
		store = NewTestDB(t, clk)
		ctx   = context.Background()
	)

	peer1ID, err := store.AddPeer(ctx, validPubKey1)
	require.NoError(t, err)
	peer2ID, err := store.AddPeer(ctx, validPubKey2)
	require.NoError(t, err)

	chan1ID, err := store.AddChannel(
		ctx, testChanPoint1, testShortChanID1, peer1ID,
	)
	require.NoError(t, err)
	chan2ID, err := store.AddChannel(
		ctx, testChanPoint2, testShortChanID2, peer2ID,
	)
	require.NoError(t, err)

	seedTime := testTime
	for _, chanID := range []int64{chan1ID, chan2ID} {
		err = store.AddChannelEvent(
			ctx, &ChannelEvent{
				ChannelID: chanID,
				EventType: EventTypeUpdate,
				Timestamp: seedTime,
				LocalBalance: fn.Some(
					btcutil.Amount(1_000_000),
				),
				RemoteBalance: fn.Some(
					btcutil.Amount(1_000_000),
				),
			},
		)
		require.NoError(t, err)
	}

	vertex1, err := route.NewVertexFromStr(validPubKey1)
	require.NoError(t, err)
	vertex2, err := route.NewVertexFromStr(validPubKey2)
	require.NoError(t, err)

	// Stub lnd to return the two channels and successful forwards of 100k
	// and 300k satoshis.
	stub := &stubLndChannelClient{
		openChannels: []lndclient.ChannelInfo{
			{
				ChannelID:   testShortChanID1,
				PubKeyBytes: vertex1,
			},
			{
				ChannelID:   testShortChanID2,
				PubKeyBytes: vertex2,
			},
		},
		forwardingHistory: &lndclient.ForwardingHistoryResponse{
			Events: []lndclient.ForwardingEvent{
				{
					ChannelIn:     testShortChanID1,
					ChannelOut:    testShortChanID2,
					AmountMsatOut: 100_000_000, // 100k sat
					FeeMsat:       100_000,
				},
				{
					ChannelIn:     testShortChanID1,
					ChannelOut:    testShortChanID2,
					AmountMsatOut: 300_000_000, // 300k sat
					FeeMsat:       300_000,
				},
			},
		},
	}

	a := NewForwardingAnalyzer(store, lndclient.LndServices{Client: stub})

	startTime := seedTime.Add(time.Second)
	endTime := startTime.Add(time.Minute)

	// Case 1: liquidityFloor = 50k. Liquidity of 1M exceeds the floor for
	// the whole 60s window, so effective uptime is the full minute and the
	// forwarded volume is the 400M msat total, earning 400k msat.
	abilities, err := a.EffectiveUptime(ctx, startTime, endTime, 50_000)
	require.NoError(t, err)

	pair := PeerPair{PeerIn: validPubKey1, PeerOut: validPubKey2}
	require.Contains(t, abilities, pair)
	require.Equal(t, time.Minute, abilities[pair].EffectiveUptime)
	require.Equal(
		t, lnwire.MilliSatoshi(400_000_000),
		abilities[pair].ForwardedMsat,
	)
	require.Equal(
		t, lnwire.MilliSatoshi(400_000), abilities[pair].FeeMsat,
	)
	require.EqualValues(t, 2, abilities[pair].Forwards)

	// Case 2: liquidityFloor = 1.5M, above the 1M liquidity, so the floor is
	// never crossed and effective uptime is zero. The forwarded volume and
	// fees are still reported so the consumer keeps the signal.
	abilities, err = a.EffectiveUptime(ctx, startTime, endTime, 1_500_000)
	require.NoError(t, err)

	require.Contains(t, abilities, pair)
	require.Zero(t, abilities[pair].EffectiveUptime)
	require.Equal(
		t, lnwire.MilliSatoshi(400_000_000),
		abilities[pair].ForwardedMsat,
	)
	require.Equal(
		t, lnwire.MilliSatoshi(400_000), abilities[pair].FeeMsat,
	)
	require.EqualValues(t, 2, abilities[pair].Forwards)
}
