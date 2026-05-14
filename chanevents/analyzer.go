package chanevents

import (
	"context"
	"errors"
	"fmt"
	"iter"
	"log/slog"
	"math"
	"sort"
	"time"

	"github.com/btcsuite/btcd/btcutil"
	"github.com/btcsuite/btclog/v2"
	"github.com/lightninglabs/lndclient"
)

var (
	// errUnexpectedUpdateEvent fires when getInitialChannelState's
	// residual-event walk surfaces an Update at a timestamp newer than the
	// seed update. The SQL ordering rules out this precondition.
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
	// Callers materialise the whole range by passing a large limit (e.g.
	// math.MaxInt32).
	GetChannelEvents(ctx context.Context, channelID, afterID int64,
		startTime, endTime time.Time,
		limit int32) ([]*ChannelEvent, error)

	// GetChannelByShortChanID resolves an scid to a Channel, returning
	// ErrUnknownChannel when no row matches.
	GetChannelByShortChanID(ctx context.Context,
		shortChannelID uint64) (*Channel, error)

	// ScidToPeerMap returns the historic scid→peer index, including closed
	// channels.
	ScidToPeerMap(ctx context.Context) (map[uint64]string, error)
}

// ForwardingAnalyzer computes forwarding velocity and effective uptime for
// every (peerIn, peerOut) pair, fusing lnd's live channel state with historical
// chanevents to avoid survivorship bias.
type ForwardingAnalyzer struct {
	store EventsSource
	lnd   lndclient.LndServices
}

// channelEventSeq names the chronological channel-event stream the pair walk
// consumes, so callers can wrap the multi-line signature without splitting
// mid-bracket inside the generic argument list.
type channelEventSeq = iter.Seq2[*ChannelEvent, error]

// ForwardingAbility quantifies the historical routing performance of a peer
// pair.
type ForwardingAbility struct {
	// Velocity is the forwarding velocity in sat/s during effective uptime.
	// +Inf indicates forwards were observed without the pair ever crossing
	// the liquidity threshold, a model inconsistency.
	Velocity float64

	// UptimeFraction is the fraction of time the channel was considered
	// effective.
	UptimeFraction float64
}

// peerPair defines a unidirectional routing edge between two peers.
type peerPair struct {
	peerIn  string
	peerOut string
}

// pairInputs encapsulates the routing performance thresholds for a single
// direction.
type pairInputs struct {
	threshold             btcutil.Amount
	totalSuccessfulAmount btcutil.Amount
}

// NewForwardingAnalyzer returns a ForwardingAnalyzer backed by the given
// chanevents store and lnd handle.
func NewForwardingAnalyzer(store EventsSource,
	lnd lndclient.LndServices) *ForwardingAnalyzer {

	return &ForwardingAnalyzer{
		store: store,
		lnd:   lnd,
	}
}

// EffectiveUptime returns a ForwardingAbility for every (peerIn, peerOut) pair
// over [startTime, endTime). Closed channels reported by lnd are folded into
// the considered set so survivorship bias does not skew the uptime denominator.
// The liquidity floor is max(quantile(successAmts, fwdPct/100), threshold).
// When forwards land but the floor is never met, Velocity is +Inf, a model
// inconsistency surfaced rather than masked.
func (a *ForwardingAnalyzer) EffectiveUptime(ctx context.Context, startTime,
	endTime time.Time, fwdPct float64, threshold btcutil.Amount) (
	map[string]map[string]ForwardingAbility, error) {

	log.DebugS(
		ctx, "Calculating effective uptime",
		slog.Time("startTime", startTime),
		slog.Time("endTime", endTime), slog.Float64("fwdPct", fwdPct),
		slog.Int64(
			"threshold", int64(threshold),
		),
	)

	scidToPeer, err := a.store.ScidToPeerMap(ctx)
	if err != nil {
		return nil, err
	}
	log.DebugS(
		ctx, "Found historical channels",
		slog.Int(
			"count", len(scidToPeer),
		),
	)

	successfulForwards, channelPeersConsidered, err := a.getForwardingData(
		ctx, startTime, endTime, scidToPeer,
	)
	if err != nil {
		return nil, err
	}
	log.DebugS(
		ctx, "Found peer pairs with successful forwards",
		slog.Int(
			"count", len(successfulForwards),
		),
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
		slog.Int(
			"count", len(peerChannels),
		),
	)

	return calculateAllPairsUptime(
		ctx, a.store, startTime, endTime, fwdPct, threshold,
		successfulForwards, initialStates, peerChannels,
	)
}

// getForwardingData returns successful forwards and channels from lnd's
// forwarding history over [startTime, endTime), indexed by peer pair. Unknown
// channels are skipped.
func (a *ForwardingAnalyzer) getForwardingData(ctx context.Context, startTime,
	endTime time.Time, scidToPeer map[uint64]string) (
	map[peerPair][]btcutil.Amount, map[uint64]string, error) {

	fwds, err := a.lnd.Client.ForwardingHistory(
		ctx, lndclient.ForwardingHistoryRequest{
			StartTime: startTime,
			EndTime:   endTime,
		},
	)
	if err != nil {
		return nil, nil, err
	}
	log.DebugS(
		ctx, "Found forwarding events",
		slog.Int(
			"count", len(fwds.Events),
		),
	)

	channelPeersConsidered := make(map[uint64]string)
	successfulForwards := make(map[peerPair][]btcutil.Amount)
	for _, fwd := range fwds.Events {
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

		pair := peerPair{
			peerIn:  inPeer,
			peerOut: outPeer,
		}

		amt := fwd.AmountMsatOut.ToSatoshis()
		successfulForwards[pair] = append(successfulForwards[pair], amt)
	}

	return successfulForwards, channelPeersConsidered, nil
}

// addActiveChannels ensures the channel set includes both open and closed
// channels, preventing survivorship bias in uptime metrics.
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
// grouped by peer. Channels absent from the store are omitted.
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
			ctx, channel.ID, startTime,
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

// getInitialChannelState reconstructs a channel's state at startTime by
// seeding from the latest pre-window update and replaying any residual
// same-second siblings the SQL keyset may have surfaced. A channel with no
// prior update is treated as offline with zero balance — the only safe
// default when nothing was observed.
func (a *ForwardingAnalyzer) getInitialChannelState(ctx context.Context,
	channelID int64, startTime time.Time) (*channelState, error) {

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
	// status events) so materialising in one call is fine.
	residual, err := a.store.GetChannelEvents(
		ctx, channelID, 0, lastUpdate.Timestamp, startTime,
		math.MaxInt32,
	)
	if err != nil {
		return nil, err
	}
	for _, event := range residual {
		// The query above is inclusive of the last update event, so we
		// need to filter it out.
		if event.ID == lastUpdate.ID {
			continue
		}
		switch event.EventType {
		case EventTypeOffline:
			state.online = false

		case EventTypeOnline:
			state.online = true

		case EventTypeUpdate:
			// Same-timestamp updates are older siblings already
			// superseded by lastUpdate; later timestamps cannot
			// appear since the SQL would have selected them.
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
	endTime time.Time, fwdPct float64, threshold btcutil.Amount,
	successfulForwards map[peerPair][]btcutil.Amount,
	initialStates map[string]map[int64]*channelState,
	peerChannels map[string][]int64) (
	map[string]map[string]ForwardingAbility, error) {

	results := make(map[string]map[string]ForwardingAbility)
	recordResult := func(peerIn, peerOut string, a ForwardingAbility) {
		if _, ok := results[peerIn]; !ok {
			results[peerIn] = make(
				map[string]ForwardingAbility,
			)
		}
		results[peerIn][peerOut] = a
	}

	// Lazy per-peer event cache: each peer's events are fetched once and
	// replayed across every pair walk that consumes them. Pre-sorted as a
	// flat slice so the cross-pair merge is a two-pointer slice walk rather
	// than an iter.Pull2 cascade.
	peerEvents := make(map[string][]*ChannelEvent, len(initialStates))
	loadPeer := func(peer string) ([]*ChannelEvent, error) {
		if cached, ok := peerEvents[peer]; ok {
			return cached, nil
		}
		events, err := loadPeerEvents(
			ctx, store, peerChannels[peer], startTime, endTime,
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

	for i, peerA := range peers {
		statesA := initialStates[peerA]
		sliceA, err := loadPeer(peerA)
		if err != nil {
			return nil, err
		}

		for j := i; j < len(peers); j++ {
			peerB := peers[j]
			statesB := initialStates[peerB]

			inputsAB, err := pairThresholdInputs(
				successfulForwards, peerA, peerB, fwdPct,
				threshold,
			)
			if err != nil {
				return nil, err
			}
			inputsBA, err := pairThresholdInputs(
				successfulForwards, peerB, peerA, fwdPct,
				threshold,
			)
			if err != nil {
				return nil, err
			}

			sliceB := sliceA
			if i != j {
				sliceB, err = loadPeer(peerB)
				if err != nil {
					return nil, err
				}
			}

			abilityAB, abilityBA, err := calculateBothDirectionsUptime(
				ctx, startTime, endTime, inputsAB, inputsBA,
				statesA, statesB,
				mergeEventSlices(sliceA, sliceB),
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

// loadPeerEvents fetches every event in [startTime, endTime) on the given
// channels and returns them merged into a single chronologically sorted slice.
// The per-channel fetches issue one GetChannelEvents call each with an
// effectively unbounded limit, and the trailing sort tie-breaks by id so the
// order is deterministic for the pair walk.
func loadPeerEvents(ctx context.Context, store EventsSource, chanIDs []int64,
	startTime, endTime time.Time) ([]*ChannelEvent, error) {

	var events []*ChannelEvent
	for _, chanID := range chanIDs {
		chanEvents, err := store.GetChannelEvents(
			ctx, chanID, 0, startTime, endTime, math.MaxInt32,
		)
		if err != nil {
			return nil, err
		}
		events = append(events, chanEvents...)
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

// pairThresholdInputs computes the per-direction threshold and total amount
// from the successful-forwards map.
func pairThresholdInputs(successfulForwards map[peerPair][]btcutil.Amount,
	peerIn, peerOut string, fwdPct float64,
	threshold btcutil.Amount) (pairInputs, error) {

	successAmts := successfulForwards[peerPair{
		peerIn: peerIn, peerOut: peerOut,
	}]
	t, err := determineThreshold(successAmts, fwdPct, threshold)
	if err != nil {
		return pairInputs{}, err
	}

	var total btcutil.Amount
	for _, amt := range successAmts {
		total += amt
	}

	return pairInputs{threshold: t, totalSuccessfulAmount: total}, nil
}

// determineThreshold establishes the required liquidity floor based on the
// user's manual threshold or the calculated percentile of successful forwards.
func determineThreshold(successAmts []btcutil.Amount, forwardPercentile float64,
	thresholdAmount btcutil.Amount) (btcutil.Amount, error) {

	if len(successAmts) == 0 {
		return thresholdAmount, nil
	}

	q := forwardPercentile / 100
	p, err := Quantile(successAmts, q)
	if err != nil {
		return 0, err
	}

	return max(btcutil.Amount(math.RoundToEven(p)), thresholdAmount), nil
}

// calculateBothDirectionsUptime computes the effective forwarding uptime for
// both directions of a peer pair in a single chronological walk of the merged
// event stream. The same state copies and the same interval ticks feed both
// directional accumulators; only the liquidity-direction roles (which peer's
// remote balance is the inbound bottleneck) and the per-direction thresholds
// differ. For self-pair calls (statesA == statesB, inputsAB == inputsBA) both
// returned abilities are equal — callers record only one.
func calculateBothDirectionsUptime(ctx context.Context, startTime,
	endTime time.Time, inputsAB, inputsBA pairInputs, statesA,
	statesB map[int64]*channelState, mergedEvents channelEventSeq) (
	*ForwardingAbility, *ForwardingAbility, error) {

	log.TraceS(ctx, "Calculating bidirectional effective uptime")
	for chanID, state := range statesA {
		log.TraceS(
			ctx, "Initial state A", slog.Int64("chanID", chanID),
			slog.Bool("online", state.online),
			slog.Int64(
				"localBalance", int64(state.localBalance),
			),
			slog.Int64(
				"remoteBalance", int64(state.remoteBalance),
			),
		)
	}
	for chanID, state := range statesB {
		log.TraceS(
			ctx, "Initial state B", slog.Int64("chanID", chanID),
			slog.Bool("online", state.online),
			slog.Int64(
				"localBalance", int64(state.localBalance),
			),
			slog.Int64(
				"remoteBalance", int64(state.remoteBalance),
			),
		)
	}
	log.TraceS(
		ctx, "Using final forwarding liquidity thresholds",
		slog.Int64(
			"thresholdAB", int64(inputsAB.threshold),
		),
		slog.Int64(
			"thresholdBA", int64(inputsBA.threshold),
		),
	)

	statesA = copyChannelStates(statesA)
	statesB = copyChannelStates(statesB)

	// Trace-level logging is per-event in this loop; the slog.Attr
	// constructors allocate even when the handler will drop the record, so
	// check the level once and short-circuit the per-tick trace calls when
	// trace is disabled. Net effect when trace is silenced (the production
	// default) is no slog allocations in the hot path.
	traceOn := log.Level() <= btclog.LevelTrace

	// Seed running balance sums once; the per-event walk maintains them
	// incrementally so the per-tick liquidity check is O(1) instead of
	// O(channels-per-peer). The forwarding liquidity (A→B) is the
	// bottleneck min(sum of A's online channels' remoteBalance, sum of B's
	// online channels' localBalance); the running sums shadow those two
	// quantities and the min is computed inline in accumulate. (B→A) is the
	// same formula with the roles flipped.
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
		if liqAB > inputsAB.threshold {
			uptimeAB += intervalDuration
		}
		if liqBA > inputsBA.threshold {
			uptimeBA += intervalDuration
		}
	}

	for event, err := range mergedEvents {
		if err != nil {
			return nil, nil, err
		}
		if traceOn {
			log.TraceS(
				ctx, "Processing event",
				slog.Int64("chanID", event.ChannelID),
				slog.Any("type", event.EventType),
				slog.Time("time", event.Timestamp),
			)
		}

		accumulate(event.Timestamp.Sub(lastTimestamp))

		if state, ok := statesA[event.ChannelID]; ok {
			if state.online {
				sumARemote -= state.remoteBalance
				sumALocal -= state.localBalance
			}
			if err := applyEvent(state, event); err != nil {
				return nil, nil, err
			}
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

	accumulate(endTime.Sub(lastTimestamp))

	log.TraceS(
		ctx, "Total effective uptime",
		slog.Duration("uptimeAB", uptimeAB),
		slog.Duration("uptimeBA", uptimeBA),
		slog.Duration(
			"totalDuration", endTime.Sub(startTime),
		),
	)

	abilityAB := makeAbility(
		uptimeAB, inputsAB.totalSuccessfulAmount, startTime, endTime,
	)
	abilityBA := makeAbility(
		uptimeBA, inputsBA.totalSuccessfulAmount, startTime, endTime,
	)

	return abilityAB, abilityBA, nil
}

// mergeEventSlices interleaves two sorted event streams into a single
// chronological iter.Seq2. Equal-timestamp events from sliceA are yielded
// first. Self-pair calls (sliceA == sliceB) yield each event twice — callers
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

// channelState is the per-channel snapshot the uptime walk carries forward as
// it consumes events: liveness plus the two balances that determine forwarding
// liquidity.
type channelState struct {
	online        bool
	localBalance  btcutil.Amount
	remoteBalance btcutil.Amount
}

// copyChannelStates duplicates the snapshot to isolate the bidirectional walk
// from upstream map mutations.
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
// online and overwrite whichever balance the event carries; unknown event types
// return errUnknownEventType to surface store↔analyzer schema drift.
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
// ForwardingAbility. When uptime is zero, velocity is +Inf if any forwards
// landed (a model inconsistency surfaced rather than masked) and 0 otherwise.
func makeAbility(totalUptime time.Duration, totalAmt btcutil.Amount, startTime,
	endTime time.Time) *ForwardingAbility {

	if totalUptime == 0 {
		velocity := 0.0
		if totalAmt > 0 {
			velocity = math.Inf(1)
		}

		return &ForwardingAbility{Velocity: velocity, UptimeFraction: 0}
	}

	totalDuration := endTime.Sub(startTime)

	return &ForwardingAbility{
		Velocity:       float64(totalAmt) / totalUptime.Seconds(),
		UptimeFraction: float64(totalUptime) / float64(totalDuration),
	}
}
