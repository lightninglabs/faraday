package chanevents

import (
	"context"
	"errors"
	"fmt"
	"iter"
	"log/slog"
	"sort"
	"time"

	"github.com/btcsuite/btcd/btcutil"
	"github.com/btcsuite/btclog/v2"
	"github.com/lightninglabs/lndclient"
)

var (
	// errUnexpectedUpdateEvent fires when getInitialChannelState's
	// residual-event walk surfaces an Update at a timestamp newer than the
	// seed update.
	errUnexpectedUpdateEvent = errors.New("unexpected update event in " +
		"initial-state walk")

	// errUnknownEventType fires when the event-replay switch sees an
	// EventType outside {Offline, Online, Update}. Indicates schema drift
	// between the store and the analyzer.
	errUnknownEventType = errors.New("unknown channel event type")
)

// EventsSource abstracts the chanevents store so ForwardingAnalyzer can derive
// uptime metrics without coupling to a specific storage backend.
type EventsSource interface {
	// GetLatestChannelUpdateBefore returns the latest channel event before
	// the given time, or (nil, nil) if no event predates it.
	GetLatestChannelUpdateBefore(ctx context.Context, channelID int64,
		before time.Time) (*ChannelEvent, error)

	// GetChannelEvents fetches up to limit events for a channel with id >
	// afterID and timestamp in [startTime, endTime), ordered by id ASC.
	// Callers page through a range by passing the last returned id as
	// afterID until a short page comes back.
	GetChannelEvents(ctx context.Context, channelID, afterID int64,
		startTime, endTime time.Time,
		limit int32) ([]*ChannelEvent, error)

	// GetChannelByShortChanID resolves an scid to a Channel, returning
	// ErrUnknownChannel when no row matches.
	GetChannelByShortChanID(ctx context.Context,
		shortChannelID uint64) (*Channel, error)

	// ScidToPeerMap returns the historically recorded scid→peer index,
	// including closed channels.
	ScidToPeerMap(ctx context.Context) (map[uint64]string, error)
}

// ForwardingAnalyzer computes forwarding velocity and effective uptime for
// every (peerIn, peerOut) pair.
type ForwardingAnalyzer struct {
	store EventsSource
	lnd   lndclient.LndServices
}

// channelEventSeq is a chronologically ordered stream of channel events
// paired with a propagated error value.
type channelEventSeq = iter.Seq2[*ChannelEvent, error]

// ForwardingAbility holds the raw forwarding facts for one direction of a peer
// pair over the analysis window. It carries no derived rates or categories. The
// consumer derives velocity and uptime fraction from these and the window, and
// reconstructs any categorization (such as forwards observed without qualifying
// uptime) from EffectiveUptime and ForwardedAmount.
type ForwardingAbility struct {
	// EffectiveUptime is the time the pair held at least the liquidity floor
	// of directional forwardable liquidity over the window.
	EffectiveUptime time.Duration

	// ForwardedAmount is the total successfully forwarded amount over the
	// window.
	ForwardedAmount btcutil.Amount
}

// PeerPair identifies a unidirectional routing edge from PeerIn to PeerOut.
// PeerIn names the source-side peer (the incoming channel's far end in lnd's
// forwarding vocabulary) and PeerOut names the sink-side peer.
type PeerPair struct {
	PeerIn  string
	PeerOut string
}

// channelState is the per-channel snapshot the uptime walk carries forward as
// it consumes events: liveness plus the two balances that determine forwarding
// liquidity.
type channelState struct {
	online        bool
	localBalance  btcutil.Amount
	remoteBalance btcutil.Amount
}

// NewForwardingAnalyzer returns a ready-to-use analyzer.
func NewForwardingAnalyzer(store EventsSource,
	lnd lndclient.LndServices) *ForwardingAnalyzer {

	return &ForwardingAnalyzer{
		store: store,
		lnd:   lnd,
	}
}

// EffectiveUptime returns a ForwardingAbility for every (peerIn, peerOut) pair
// over [startTime, endTime). Closed channels are folded into the considered set
// so survivorship bias does not skew the uptime denominator. A single
// liquidityFloor is applied uniformly to every pair, so effective uptime is the
// time each pair held at least that much directional forwardable liquidity.
func (a *ForwardingAnalyzer) EffectiveUptime(ctx context.Context, startTime,
	endTime time.Time, liquidityFloor btcutil.Amount) (
	map[PeerPair]ForwardingAbility, error) {

	log.DebugS(
		ctx, "Calculating effective uptime",
		slog.Time("startTime", startTime),
		slog.Time("endTime", endTime),
		slog.Int64("liquidityFloor", int64(liquidityFloor)),
	)

	scidToPeer, err := a.store.ScidToPeerMap(ctx)
	if err != nil {
		return nil, err
	}
	log.DebugS(
		ctx, "Found historical channels",
		slog.Int("count", len(scidToPeer)),
	)

	successfulForwards, channelPeersConsidered, err := a.getForwardingData(
		ctx, startTime, endTime, scidToPeer,
	)
	if err != nil {
		return nil, err
	}
	log.DebugS(
		ctx, "Found peer pairs with successful forwards",
		slog.Int("count", len(successfulForwards)),
	)

	err = a.addActiveChannels(ctx, channelPeersConsidered)
	if err != nil {
		return nil, err
	}

	peerChannels, initialStates, err := a.getPeerChannelData(
		ctx, startTime, channelPeersConsidered,
	)
	if err != nil {
		return nil, err
	}
	log.DebugS(
		ctx, "Identified channels for peers",
		slog.Int("count", len(peerChannels)),
	)

	return calculateAllPairsUptime(
		ctx, a.store, startTime, endTime, liquidityFloor,
		successfulForwards, initialStates, peerChannels,
	)
}

// getForwardingData queries lnd's forwarding history sequentially in paginated
// batches to retrieve successful forwarding events within the specified time
// range, indexing the results by peer pair.
func (a *ForwardingAnalyzer) getForwardingData(ctx context.Context, startTime,
	endTime time.Time, scidToPeer map[uint64]string) (
	map[PeerPair][]btcutil.Amount, map[uint64]string, error) {

	var events []lndclient.ForwardingEvent
	var offset uint32
	const forwardingPageSize = 1000

	for {
		fwds, err := a.lnd.Client.ForwardingHistory(
			ctx, lndclient.ForwardingHistoryRequest{
				StartTime: startTime,
				EndTime:   endTime,
				Offset:    offset,
				MaxEvents: forwardingPageSize,
			},
		)
		if err != nil {
			return nil, nil, err
		}

		if len(fwds.Events) == 0 {
			break
		}

		events = append(events, fwds.Events...)
		if len(fwds.Events) < forwardingPageSize {
			break
		}

		// Guard against a non-advancing offset: if lnd does not move
		// LastIndexOffset past the cursor we already queried, stop
		// rather than re-fetch the same page forever.
		if fwds.LastIndexOffset <= offset {
			break
		}
		offset = fwds.LastIndexOffset
	}

	log.DebugS(
		ctx, "Found forwarding events",
		slog.Int(
			"count", len(events),
		),
	)

	channelPeersConsidered := make(map[uint64]string)
	successfulForwards := make(map[PeerPair][]btcutil.Amount)
	for _, fwd := range events {
		inPeer, ok := scidToPeer[fwd.ChannelIn]
		if !ok {
			log.WarnS(
				ctx, "Could not find peer for incoming channel",
				nil, slog.Uint64("channelIn", fwd.ChannelIn),
			)
			continue
		}

		outPeer, ok := scidToPeer[fwd.ChannelOut]
		if !ok {
			log.WarnS(
				ctx, "Could not find peer for outgoing channel",
				nil, slog.Uint64("channelOut", fwd.ChannelOut),
			)
			continue
		}

		channelPeersConsidered[fwd.ChannelIn] = inPeer
		channelPeersConsidered[fwd.ChannelOut] = outPeer

		pair := PeerPair{
			PeerIn:  inPeer,
			PeerOut: outPeer,
		}

		amt := fwd.AmountMsatOut.ToSatoshis()
		successfulForwards[pair] = append(successfulForwards[pair], amt)
	}

	return successfulForwards, channelPeersConsidered, nil
}

// addActiveChannels ensures the channel set includes both open and closed
// channels so that channels that closed during the analysis period are not
// silently excluded.
func (a *ForwardingAnalyzer) addActiveChannels(ctx context.Context,
	channelPeersConsidered map[uint64]string) error {

	// Currently open channels surface their peer directly.
	openChannels, err := a.lnd.Client.ListChannels(ctx, false, false)
	if err != nil {
		return err
	}

	for _, channel := range openChannels {
		channelPeersConsidered[channel.ChannelID] =
			channel.PubKeyBytes.String()
	}

	// Historically closed channels are added so survivorship bias does not
	// skew the denominator.
	closedChannels, err := a.lnd.Client.ClosedChannels(ctx)
	if err != nil {
		return err
	}

	for _, channel := range closedChannels {
		// Channels that did not confirm onchain will not have a
		// ChannelID.
		if channel.ChannelID == 0 {
			continue
		}

		channelPeersConsidered[channel.ChannelID] =
			channel.PubKeyBytes.String()
	}

	return nil
}

// getPeerChannelData returns channels and their initial state at startTime,
// grouped by peer, including only those present in the store.
func (a *ForwardingAnalyzer) getPeerChannelData(ctx context.Context,
	startTime time.Time, channelPeersConsidered map[uint64]string) (
	map[string][]int64, map[string]map[int64]*channelState, error) {

	peerChannels := make(map[string][]int64)
	initialStates := make(map[string]map[int64]*channelState)
	for scid, peerPubKey := range channelPeersConsidered {
		channel, err := a.store.GetChannelByShortChanID(ctx, scid)
		if errors.Is(err, ErrUnknownChannel) {
			// Channels obtained from lnd but not present in the
			// store. This can happen if the channel was very
			// recently opened or closed and the store hasn't
			// ingested the event yet.
			log.DebugS(
				ctx, "Skipping channel not in events store",
				slog.Uint64("scid", scid),
			)

			continue
		}
		if err != nil {
			return nil, nil, err
		}

		state, err := a.getInitialChannelState(
			ctx, startTime, channel.ID,
		)
		if err != nil {
			return nil, nil, err
		}

		if _, ok := initialStates[peerPubKey]; !ok {
			initialStates[peerPubKey] = make(
				map[int64]*channelState,
			)
		}
		initialStates[peerPubKey][channel.ID] = state

		peerChannels[peerPubKey] = append(
			peerChannels[peerPubKey], channel.ID,
		)
	}

	return peerChannels, initialStates, nil
}

// getInitialChannelState reconstructs a channel's state at startTime by seeding
// from the latest pre-window update and replaying any residual same-second
// siblings the SQL keyset may have surfaced. A channel with no prior update is
// treated as offline with zero balance.
func (a *ForwardingAnalyzer) getInitialChannelState(ctx context.Context,
	startTime time.Time, channelID int64) (*channelState, error) {

	lastUpdate, err := a.store.GetLatestChannelUpdateBefore(
		ctx, channelID, startTime,
	)
	if err != nil {
		return nil, err
	}

	if lastUpdate == nil {
		log.TraceS(
			ctx, "No update event for channel",
			slog.Int64("channelID", channelID),
			slog.Time("startTime", startTime),
		)

		return &channelState{online: false}, nil
	}

	// An update event always implies the channel is online.
	state := &channelState{
		online: true,
	}
	lastUpdate.LocalBalance.WhenSome(
		func(amt btcutil.Amount) {
			state.localBalance = amt
		},
	)
	lastUpdate.RemoteBalance.WhenSome(
		func(amt btcutil.Amount) {
			state.remoteBalance = amt
		},
	)

	// Fetch any residual events between the last update and the start time.
	// The range is bounded (typically a handful of same-second siblings or
	// status events) so materialising in one call is fine. Replay below
	// assumes id-ASC matches chronological order, true while writers leave
	// Timestamp zero so the store stamps clock.Now(). Overflow at the cap
	// signals pathological volume the analyzer cannot safely seed from.
	const residualEventLimit = 1024

	residual, err := a.store.GetChannelEvents(
		ctx, channelID, lastUpdate.ID, lastUpdate.Timestamp, startTime,
		residualEventLimit,
	)
	if err != nil {
		return nil, err
	}

	if len(residual) == residualEventLimit {
		return nil, fmt.Errorf("residual events overflow (>=%d) for "+
			"chanID=%d", residualEventLimit, channelID)
	}

	// Replay the residual events to arrive at the channel state on the
	// window's open.
	for _, event := range residual {
		switch event.EventType {
		case EventTypeOffline:
			state.online = false

		case EventTypeOnline:
			state.online = true

		case EventTypeUpdate:
			// Defensively check that the seed update is indeed the
			// latest before startTime.
			if !event.Timestamp.Equal(lastUpdate.Timestamp) {
				return nil, fmt.Errorf("%w: chanID=%d ts=%v",
					errUnexpectedUpdateEvent, channelID,
					event.Timestamp)
			}

		default:
			return nil, fmt.Errorf("%w: chanID=%d type=%v",
				errUnknownEventType, channelID, event.EventType)
		}
	}

	return state, nil
}

// calculateAllPairsUptime returns forwarding abilities for every peer pair,
// computing both directions (A→B and B→A) in a single pass.
func calculateAllPairsUptime(ctx context.Context, store EventsSource, startTime,
	endTime time.Time, liquidityFloor btcutil.Amount,
	successfulForwards map[PeerPair][]btcutil.Amount,
	initialStates map[string]map[int64]*channelState,
	peerChannels map[string][]int64) (
	map[PeerPair]ForwardingAbility, error) {

	results := make(map[PeerPair]ForwardingAbility)
	recordResult := func(peerIn, peerOut string, a ForwardingAbility) {
		results[PeerPair{PeerIn: peerIn, PeerOut: peerOut}] = a
	}

	// Lazy per-peer event cache: each peer's events are fetched once and
	// replayed across every pair walk that consumes them.
	peerEvents := make(map[string][]*ChannelEvent, len(initialStates))
	loadPeer := func(peer string) ([]*ChannelEvent, error) {
		if cached, ok := peerEvents[peer]; ok {
			return cached, nil
		}

		events, err := loadPeerEvents(
			ctx, store, startTime, endTime, peerChannels[peer],
		)
		if err != nil {
			return nil, err
		}

		peerEvents[peer] = events

		return events, nil
	}

	peers := make([]string, 0, len(initialStates))
	for peer := range initialStates {
		peers = append(peers, peer)
	}

	type peerInitialSums struct {
		remote btcutil.Amount
		local  btcutil.Amount
	}

	// We gather the initial balance sums for each peer upfront so the pair
	// walk can be more efficient and doesn't have to recalculate.
	initialSums := make(map[string]peerInitialSums, len(initialStates))
	for peer, states := range initialStates {
		var remoteSum, localSum btcutil.Amount
		for _, s := range states {
			if s.online {
				remoteSum += s.remoteBalance
				localSum += s.localBalance
			}
		}
		initialSums[peer] = peerInitialSums{
			remote: remoteSum,
			local:  localSum,
		}
	}

	for i, peerA := range peers {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		statesA := initialStates[peerA]
		sumsA := initialSums[peerA]

		sliceA, err := loadPeer(peerA)
		if err != nil {
			return nil, err
		}

		for j := i; j < len(peers); j++ {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}

			peerB := peers[j]
			statesB := initialStates[peerB]
			sumsB := initialSums[peerB]

			forwardedAB := pairForwardedTotal(
				successfulForwards, peerA, peerB,
			)
			forwardedBA := pairForwardedTotal(
				successfulForwards, peerB, peerA,
			)

			sliceB := sliceA
			if i != j {
				sliceB, err = loadPeer(peerB)
				if err != nil {
					return nil, err
				}
			}

			abilityAB, abilityBA, err :=
				calculateBothDirectionsUptime(
					ctx, startTime, endTime,
					liquidityFloor,
					statesA, statesB,
					sumsA.remote, sumsA.local,
					sumsB.remote, sumsB.local,
					mergeEventSlices(sliceA, sliceB),
					forwardedAB, forwardedBA,
				)
			if err != nil {
				return nil, err
			}

			recordResult(peerA, peerB, *abilityAB)
			if i != j {
				recordResult(peerB, peerA, *abilityBA)
			}
		}
	}

	return results, nil
}

// eventPageSize bounds a single channel-event page so the per-channel fetch
// never asks the store for an unbounded result set.
const eventPageSize = 1000

// loadPeerEvents fetches every event in [startTime, endTime) on the given
// channels and returns them merged into a single chronologically sorted slice.
// Each channel is paged through in id-ascending batches so no single store
// query is unbounded. Events sharing a timestamp are ordered by ascending id so
// the result is deterministic.
func loadPeerEvents(ctx context.Context, store EventsSource, startTime,
	endTime time.Time, chanIDs []int64) ([]*ChannelEvent, error) {

	var events []*ChannelEvent
	for _, chanID := range chanIDs {
		var afterID int64
		for {
			page, err := store.GetChannelEvents(
				ctx, chanID, afterID, startTime, endTime,
				eventPageSize,
			)
			if err != nil {
				return nil, err
			}

			events = append(events, page...)
			if len(page) < eventPageSize {
				break
			}

			// Events come back id-ASC, so the last id is the
			// largest; continue the next page after it.
			afterID = page[len(page)-1].ID
		}
	}

	sort.SliceStable(
		events,
		func(i, j int) bool {
			if events[i].Timestamp.Equal(events[j].Timestamp) {
				return events[i].ID < events[j].ID
			}

			return events[i].Timestamp.Before(events[j].Timestamp)
		},
	)

	return events, nil
}

// pairForwardedTotal sums the successfully forwarded amounts for one direction
// of a peer pair over the analysis window.
func pairForwardedTotal(successfulForwards map[PeerPair][]btcutil.Amount,
	peerIn, peerOut string) btcutil.Amount {

	var total btcutil.Amount
	for _, amt := range successfulForwards[PeerPair{
		PeerIn: peerIn, PeerOut: peerOut,
	}] {
		total += amt
	}

	return total
}

// calculateBothDirectionsUptime computes the effective forwarding uptime for
// both directions of a peer pair in a single chronological walk of the merged
// event stream. Only the liquidity-direction roles differ between the two
// accumulators, as both share the same uniform liquidityFloor. forwardedAB and
// forwardedBA carry each direction's total forwarded volume through to the
// returned abilities. For self-pair calls both returned abilities are equal.
func calculateBothDirectionsUptime(ctx context.Context, startTime,
	endTime time.Time, liquidityFloor btcutil.Amount, statesA,
	statesB map[int64]*channelState, sumARemote, sumALocal, sumBRemote,
	sumBLocal btcutil.Amount, mergedEvents channelEventSeq,
	forwardedAB, forwardedBA btcutil.Amount) (
	*ForwardingAbility, *ForwardingAbility, error) {

	traceOn := log.Level() <= btclog.LevelTrace

	if traceOn {
		log.TraceS(ctx, "Calculating bidirectional effective uptime")
		for chanID, state := range statesA {
			log.TraceS(
				ctx, "Initial state A",
				slog.Int64("chanID", chanID),
				slog.Bool("online", state.online),
				slog.Int64(
					"localBalance", int64(
						state.localBalance,
					),
				),
				slog.Int64(
					"remoteBalance", int64(
						state.remoteBalance,
					),
				),
			)
		}
		for chanID, state := range statesB {
			log.TraceS(
				ctx, "Initial state B",
				slog.Int64("chanID", chanID),
				slog.Bool("online", state.online),
				slog.Int64(
					"localBalance", int64(
						state.localBalance,
					),
				),
				slog.Int64(
					"remoteBalance", int64(
						state.remoteBalance,
					),
				),
			)
		}
		log.TraceS(
			ctx, "Using uniform forwarding liquidity floor",
			slog.Int64("liquidityFloor", int64(liquidityFloor)),
		)
	}

	statesA = copyChannelStates(statesA)
	statesB = copyChannelStates(statesB)

	var uptimeAB, uptimeBA time.Duration
	lastTimestamp := startTime

	accumulate := func(intervalDuration time.Duration) {
		if intervalDuration <= 0 {
			return
		}
		// (A→B): A is incoming, B is outgoing. Liquidity bottleneck is
		// min(A's online inbound, B's online outbound).
		liqAB := min(sumARemote, sumBLocal)
		// (B→A): roles flipped.
		liqBA := min(sumBRemote, sumALocal)
		if traceOn {
			log.TraceS(
				ctx, "Forwarding liquidity check",
				slog.Duration("interval", intervalDuration),
				slog.Int64(
					"liqAB", int64(liqAB),
				),
				slog.Int64(
					"liqBA", int64(liqBA),
				),
			)
		}

		// A direction qualifies when its bottleneck liquidity is at
		// least the floor, matching the "at least" contract documented
		// on the ForwardingAbility proto, struct, and CLI flag. The
		// liquidity must also be strictly positive: zero forwardable
		// liquidity can never route a payment, even when the floor is 0.
		if liqAB >= liquidityFloor && liqAB > 0 {
			uptimeAB += intervalDuration
		}
		if liqBA >= liquidityFloor && liqBA > 0 {
			uptimeBA += intervalDuration
		}
	}

	// Walk the merged event stream, applying each event to both peers'
	// states and accumulating uptime for each direction when the respective
	// liquidity conditions are met.
	for event, err := range mergedEvents {
		if err != nil {
			return nil, nil, err
		}
		if traceOn {
			log.TraceS(
				ctx, "Processing event",
				slog.Int64("chanID", event.ChannelID),
				btclog.Fmt("type", "%v", event.EventType),
				slog.Time("time", event.Timestamp),
			)
		}

		// accumulate uptime for the elapsed interval since the last
		// event, based on the state of the channels during that
		// interval. The events are ordered chronologically so the state
		// is consistent with the entire interval.
		accumulate(event.Timestamp.Sub(lastTimestamp))

		// Update the state for each peer if the event affects one of
		// their channels. Before applying the event, we remove the
		// channel's contribution to the sums if it's currently online,
		// because the event may change the channel's online status or
		// balances in a way that affects the sums.
		if state, ok := statesA[event.ChannelID]; ok {
			// We would have inlcuded the channel's balances in the
			// sums if it was online, so we need to remove them
			// before applying the event.
			if state.online {
				sumARemote -= state.remoteBalance
				sumALocal -= state.localBalance
			}

			if err := applyEvent(state, event); err != nil {
				return nil, nil, err
			}

			// If the channel is still online after applying the
			// event, we add its (possibly updated) balances back to
			// the sums.
			if state.online {
				sumARemote += state.remoteBalance
				sumALocal += state.localBalance
			}
		}
		if state, ok := statesB[event.ChannelID]; ok {
			if state.online {
				sumBRemote -= state.remoteBalance
				sumBLocal -= state.localBalance
			}
			if err := applyEvent(state, event); err != nil {
				return nil, nil, err
			}
			if state.online {
				sumBRemote += state.remoteBalance
				sumBLocal += state.localBalance
			}
		}

		lastTimestamp = event.Timestamp
	}

	// Account for the final interval between the last event and the end
	// time.
	accumulate(endTime.Sub(lastTimestamp))

	if traceOn {
		log.TraceS(
			ctx, "Total effective uptime",
			slog.Duration("uptimeAB", uptimeAB),
			slog.Duration("uptimeBA", uptimeBA),
			slog.Duration(
				"totalDuration", endTime.Sub(startTime),
			),
		)
	}

	abilityAB := makeAbility(uptimeAB, forwardedAB)
	abilityBA := makeAbility(uptimeBA, forwardedBA)

	return abilityAB, abilityBA, nil
}

// mergeEventSlices interleaves two sorted event streams into a single
// chronological iter.Seq2. Equal-timestamp events from sliceA are yielded
// first. Self-pair calls (sliceA == sliceB) yield each event twice. Callers
// must keep their state updates idempotent under same-timestamp duplicates.
func mergeEventSlices(sliceA, sliceB []*ChannelEvent) channelEventSeq {
	return func(yield func(*ChannelEvent, error) bool) {
		i, j := 0, 0

		// Interleave both slices until one is exhausted, ensuring
		// strict chronological order across the combined stream.
		for i < len(sliceA) && j < len(sliceB) {
			if sliceA[i].Timestamp.After(sliceB[j].Timestamp) {
				if !yield(sliceB[j], nil) {
					return
				}
				j++
			} else {
				if !yield(sliceA[i], nil) {
					return
				}
				i++
			}
		}

		// Drain any remaining events from sliceA. This loop only
		// executes if sliceB was exhausted first.
		for ; i < len(sliceA); i++ {
			if !yield(sliceA[i], nil) {
				return
			}
		}

		// Drain any remaining events from sliceB. This loop only
		// executes if sliceA was exhausted first.
		for ; j < len(sliceB); j++ {
			if !yield(sliceB[j], nil) {
				return
			}
		}
	}
}

// copyChannelStates returns a deep copy of the per-channel state map so the
// bidirectional walk cannot mutate the caller's snapshot.
func copyChannelStates(states map[int64]*channelState) map[int64]*channelState {
	statesCopy := make(map[int64]*channelState, len(states))
	for chanID, state := range states {
		statesCopy[chanID] = &channelState{
			online:        state.online,
			localBalance:  state.localBalance,
			remoteBalance: state.remoteBalance,
		}
	}

	return statesCopy
}

// applyEvent advances a channel's snapshot by one event. Update events imply
// online and overwrite whichever balance the event carries. Unknown event
// types return errUnknownEventType to surface store↔analyzer schema drift.
func applyEvent(state *channelState, event *ChannelEvent) error {
	switch event.EventType {
	case EventTypeOffline:
		state.online = false

	case EventTypeOnline:
		state.online = true

	case EventTypeUpdate:
		state.online = true
		event.LocalBalance.WhenSome(
			func(amt btcutil.Amount) {
				state.localBalance = amt
			},
		)
		event.RemoteBalance.WhenSome(
			func(amt btcutil.Amount) {
				state.remoteBalance = amt
			},
		)

	default:
		return fmt.Errorf("%w: chanID=%d type=%v", errUnknownEventType,
			event.ChannelID, event.EventType)
	}

	return nil
}

// makeAbility folds an accumulated uptime and successful-amount total into a
// ForwardingAbility carrying the raw facts. Derived rates and categories are
// left to the consumer.
func makeAbility(totalUptime time.Duration,
	totalAmt btcutil.Amount) *ForwardingAbility {

	return &ForwardingAbility{
		EffectiveUptime: totalUptime,
		ForwardedAmount: totalAmt,
	}
}
