package chanevents

import (
	"context"
	"errors"
	"fmt"
	"iter"
	"log/slog"
	"math"
	"time"

	"github.com/btcsuite/btcd/btcutil"
	"github.com/btcsuite/btclog/v2"
)

var (
	// errUnknownEventType fires when the event-replay switch sees an
	// EventType outside {Offline, Online, Update}. Indicates schema drift
	// between the store and the analyzer.
	errUnknownEventType = errors.New("unknown channel event type")
)

// channelEventSeq is a chronologically ordered stream of channel events
// paired with a propagated error value.
type channelEventSeq = iter.Seq2[*ChannelEvent, error]

// ForwardingAbility quantifies the historical routing performance of a peer
// pair. Inconsistent flags the pathological case where forwards were observed
// without the pair ever crossing the liquidity threshold; Velocity is zero in
// that case because the rate is undefined over zero qualifying uptime.
type ForwardingAbility struct {
	// Velocity is the forwarding velocity in sat/s during effective uptime.
	Velocity float64

	// UptimeFraction is the ratio of effective uptime to the full window
	// duration, in [0, 1].
	UptimeFraction float64

	// Inconsistent is set when forwards landed but effective uptime was
	// zero, indicating the input data and the threshold model disagree.
	Inconsistent bool
}

// pairInputs encapsulates the routing performance thresholds for a single
// direction.
type pairInputs struct {
	threshold             btcutil.Amount
	totalSuccessfulAmount btcutil.Amount
}

// channelState is the per-channel snapshot the uptime walk carries forward as
// it consumes events: liveness plus the two balances that determine forwarding
// liquidity.
type channelState struct {
	online        bool
	localBalance  btcutil.Amount
	remoteBalance btcutil.Amount
}

// determineThreshold establishes the required liquidity floor based on the
// user's manual threshold or the calculated percentile of successful forwards.
func determineThreshold(forwardPercentile float64,
	thresholdAmount btcutil.Amount,
	successAmts []btcutil.Amount) (btcutil.Amount, error) {

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
// event stream. Only the liquidity-direction roles and the per-direction
// thresholds differ between the two accumulators. For self-pair calls (statesA
// == statesB, inputsAB == inputsBA) both returned abilities are equal.
func calculateBothDirectionsUptime(ctx context.Context, startTime,
	endTime time.Time, inputsAB, inputsBA pairInputs, statesA,
	statesB map[int64]*channelState, sumARemote, sumALocal, sumBRemote,
	sumBLocal btcutil.Amount, mergedEvents channelEventSeq) (
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
			ctx, "Using final forwarding liquidity thresholds",
			slog.Int64(
				"thresholdAB", int64(inputsAB.threshold),
			),
			slog.Int64(
				"thresholdBA", int64(inputsBA.threshold),
			),
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
		if liqAB > inputsAB.threshold {
			uptimeAB += intervalDuration
		}
		if liqBA > inputsBA.threshold {
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

	abilityAB := makeAbility(
		startTime, endTime, uptimeAB, inputsAB.totalSuccessfulAmount,
	)
	abilityBA := makeAbility(
		startTime, endTime, uptimeBA, inputsBA.totalSuccessfulAmount,
	)

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
// ForwardingAbility. When uptime is zero and forwards landed, the result is
// flagged Inconsistent with zero Velocity.
func makeAbility(startTime, endTime time.Time, totalUptime time.Duration,
	totalAmt btcutil.Amount) *ForwardingAbility {

	if totalUptime == 0 {
		return &ForwardingAbility{Inconsistent: totalAmt > 0}
	}

	totalDuration := endTime.Sub(startTime)

	return &ForwardingAbility{
		Velocity:       float64(totalAmt) / totalUptime.Seconds(),
		UptimeFraction: float64(totalUptime) / float64(totalDuration),
	}
}
