package frdrpcserver

import (
	"context"
	"log/slog"
	"math"

	"github.com/btcsuite/btcd/btcutil"
	"github.com/lightninglabs/faraday/chanevents"
	"github.com/lightninglabs/faraday/frdrpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// ForwardingAbility returns per-peer-pair velocity and uptime fraction over the
// requested window. Invalid inputs produce typed gRPC error codes
// (InvalidArgument, FailedPrecondition).
func (s *RPCServer) ForwardingAbility(ctx context.Context,
	req *frdrpc.ForwardingAbilityRequest) (
	*frdrpc.ForwardingAbilityResponse, error) {

	log.DebugS(
		ctx, "Received ForwardingAbility request",
		slog.Uint64("startTime", req.StartTime),
		slog.Uint64("endTime", req.EndTime),
		slog.Float64(
			"forwardPercentile", float64(req.ForwardPercentile),
		), slog.Uint64("thresholdAmtSat", req.ThresholdAmtSat),
	)

	if s.cfg.ChanEvents == nil {
		return nil, status.Error(
			codes.FailedPrecondition,
			"channel events store not configured",
		)
	}

	// uint64 RPC inputs can carry values up to 2^64-1, but the downstream
	// time.Unix(int64(...)) in validateTimes truncates the high bit. Reject
	// values above math.MaxInt64 (= 2^63-1) so the truncation never
	// silently flips sign.
	if req.StartTime > math.MaxInt64 || req.EndTime > math.MaxInt64 {
		return nil, status.Error(
			codes.InvalidArgument,
			"start_time and end_time must be <= MaxInt64",
		)
	}

	startTime, endTime, err := validateTimes(req.StartTime, req.EndTime)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}

	if req.ForwardPercentile < 0 || req.ForwardPercentile > 100 {
		return nil, status.Error(
			codes.InvalidArgument,
			"forward_percentile must be in [0, 100]",
		)
	}

	forwardingAnalyzer := chanevents.NewForwardingAnalyzer(
		s.cfg.ChanEvents, s.cfg.Lnd,
	)

	threshold := btcutil.Amount(req.ThresholdAmtSat)
	pairs, err := forwardingAnalyzer.EffectiveUptime(
		ctx, startTime, endTime, float64(req.ForwardPercentile),
		threshold,
	)
	if err != nil {
		return nil, err
	}

	var rpcPairs []*frdrpc.ForwardingAbilityPair
	for peerIn, outMap := range pairs {
		for peerOut, ability := range outMap {
			rpcPairs = append(
				rpcPairs, &frdrpc.ForwardingAbilityPair{
					PeerIn:  peerIn,
					PeerOut: peerOut,
					Ability: &frdrpc.ForwardingAbility{
						Velocity:       ability.Velocity,
						UptimeFraction: ability.UptimeFraction,
					},
				},
			)
		}
	}

	return &frdrpc.ForwardingAbilityResponse{
		Pairs: rpcPairs,
	}, nil
}
