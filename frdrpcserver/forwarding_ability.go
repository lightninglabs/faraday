package frdrpcserver

import (
	"context"
	"log/slog"
	"math"
	"time"

	"github.com/btcsuite/btcd/btcutil"
	"github.com/lightninglabs/faraday/frdrpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// defaultLiquidityFloorSat is the liquidity floor applied when the request
// leaves liquidity_floor_sat unset. It approximates the smallest amount a
// rebalancer would still move, below which a pair is not economically
// forwardable.
const defaultLiquidityFloorSat = 50_000

// defaultUptimeThreshold is the uptime fraction applied when the request leaves
// uptime_threshold unset. A pair that was up at least this fraction of the
// window but did not forward is reported as a single bit rather than a full
// entry. It is high so that only reliably available pairs are flagged, keeping
// the response sparse and the node-down guard meaningful.
const defaultUptimeThreshold = 0.9

// ForwardingAbility returns the raw effective-uptime and forwarded-volume facts
// for each peer pair over the requested window. An unset end_time defaults to
// the current time and an unset liquidity_floor_sat to
// defaultLiquidityFloorSat.
func (s *RPCServer) ForwardingAbility(ctx context.Context,
	req *frdrpc.ForwardingAbilityRequest) (
	*frdrpc.ForwardingAbilityResponse, error) {

	log.DebugS(
		ctx, "Handling ForwardingAbility request",
		slog.Uint64("start_time", req.StartTime),
		slog.Uint64("end_time", req.EndTime),
		slog.Uint64("liquidity_floor_sat", req.LiquidityFloorSat),
	)

	// time.Unix takes an int64, so reject any request value that would
	// overflow when its uint64 seconds are narrowed below.
	if req.StartTime > math.MaxInt64 {
		return nil, status.Error(
			codes.InvalidArgument,
			"start_time exceeds maximum allowed value",
		)
	}
	if req.EndTime > math.MaxInt64 {
		return nil, status.Error(
			codes.InvalidArgument,
			"end_time exceeds maximum allowed value",
		)
	}

	startTime := time.Unix(int64(req.StartTime), 0)
	endTime := time.Now()
	if req.EndTime != 0 {
		endTime = time.Unix(int64(req.EndTime), 0)
	}

	if startTime.After(endTime) {
		return nil, status.Error(
			codes.InvalidArgument,
			"start_time must be less than or equal to end_time",
		)
	}

	if s.cfg.ForwardingAnalyzer == nil {
		return nil, status.Error(
			codes.Unavailable,
			"forwarding analyzer is not configured",
		)
	}

	liquidityFloor := req.LiquidityFloorSat
	if liquidityFloor == 0 {
		liquidityFloor = defaultLiquidityFloorSat
	}

	uptimeThreshold := req.UptimeThreshold
	if uptimeThreshold == 0 {
		uptimeThreshold = defaultUptimeThreshold
	}

	// Reject NaN explicitly: NaN comparisons are always false, so a bare
	// range check would let it slip through.
	if math.IsNaN(uptimeThreshold) || uptimeThreshold < 0 ||
		uptimeThreshold > 1 {

		return nil, status.Error(
			codes.InvalidArgument,
			"uptime_threshold must be in [0, 1]",
		)
	}

	abilities, err := s.cfg.ForwardingAnalyzer.EffectiveUptime(
		ctx, startTime, endTime, btcutil.Amount(liquidityFloor),
	)
	if err != nil {
		log.ErrorS(
			ctx, "EffectiveUptime failed", err,
			slog.Time("start_time", startTime),
			slog.Time("end_time", endTime),
			slog.Uint64("liquidity_floor_sat", liquidityFloor),
		)

		return nil, status.Errorf(codes.Internal, "failed to "+
			"calculate effective uptime: %v", err)
	}

	// Convert the flat map into the nested map the codec expects, carrying
	// the raw facts through unchanged. EffectiveUptime is truncated to
	// whole seconds to match the second-granularity wire field. A pair with
	// only sub-second qualifying uptime therefore reports zero uptime while
	// still carrying its forwarded volume.
	nested := make(map[string]map[string]frdrpc.ForwardingAbility)
	for pair, ability := range abilities {
		if _, ok := nested[pair.PeerIn]; !ok {
			nested[pair.PeerIn] =
				make(map[string]frdrpc.ForwardingAbility)
		}

		nested[pair.PeerIn][pair.PeerOut] = frdrpc.ForwardingAbility{
			EffectiveUptimeS: int64(
				ability.EffectiveUptime.Seconds(),
			),
			ForwardedSat: int64(ability.ForwardedAmount),
			FeeMsat:      int64(ability.FeeMsat),
			Forwards:     ability.Forwards,
		}
	}

	// Guard against returning data when the node itself was down for the
	// window. If no pair held at least the threshold fraction of uptime,
	// the response carries no signal and lowering the threshold to surface
	// something would only inflate it, so fail loudly instead.
	minUptimeS := frdrpc.MinQualifyingUptime(
		uptimeThreshold, endTime.Unix()-startTime.Unix(),
	)
	var qualifying int
	for _, outMap := range nested {
		for _, ability := range outMap {
			if ability.EffectiveUptimeS >= minUptimeS {
				qualifying++
			}
		}
	}
	if qualifying == 0 {
		return nil, status.Error(codes.FailedPrecondition, "no peer "+
			"pair met the uptime threshold over the window; the "+
			"node may have been offline")
	}

	resp, err := frdrpc.EncodeForwardingAbility(
		nested, startTime.Unix(), endTime.Unix(), uptimeThreshold,
	)
	if err != nil {
		log.ErrorS(
			ctx, "EncodeForwardingAbility failed", err,
			slog.Int("pairs", len(abilities)),
		)

		return nil, status.Errorf(codes.Internal, "failed to encode "+
			"forwarding ability: %v", err)
	}

	return resp, nil
}
