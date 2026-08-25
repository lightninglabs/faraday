package frdrpcserver

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/btcsuite/btcd/btcutil"
	"github.com/lightninglabs/faraday/chanevents"
	"github.com/lightninglabs/faraday/frdrpc"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type mockForwardingAnalyzer struct {
	effectiveUptimeFunc func(ctx context.Context, startTime, endTime time.Time,
		liquidityFloor btcutil.Amount) (
		map[chanevents.PeerPair]chanevents.ForwardingAbility, error)
}

func (m *mockForwardingAnalyzer) EffectiveUptime(ctx context.Context, startTime,
	endTime time.Time, liquidityFloor btcutil.Amount) (
	map[chanevents.PeerPair]chanevents.ForwardingAbility, error) {

	return m.effectiveUptimeFunc(ctx, startTime, endTime, liquidityFloor)
}

// TestForwardingAbility tests the ForwardingAbility RPC method, covering both
// successful and error cases.
func TestForwardingAbility(t *testing.T) {
	const (
		peerIn  = "02aaaabbbbcccc0000000000000000000000000000000000000000000000000001"
		peerOut = "02aaaabbbbcccc0000000000000000000000000000000000000000000000000002"
	)

	// analyzerResult is the canned analyzer return for a case. A nil
	// analyzerResult means the case leaves ForwardingAnalyzer unconfigured.
	type analyzerResult func() (
		map[chanevents.PeerPair]chanevents.ForwardingAbility, error)

	tests := []struct {
		name     string
		analyzer analyzerResult
		req      *frdrpc.ForwardingAbilityRequest

		// wantCode is the expected gRPC status; codes.OK denotes
		// success.
		wantCode codes.Code

		// check runs on success with the response and the floor the
		// handler resolved and passed to the analyzer.
		check func(t *testing.T, resp *frdrpc.ForwardingAbilityResponse,
			gotFloor btcutil.Amount)
	}{
		{
			name: "encodes analyzer facts",
			analyzer: func() (
				map[chanevents.PeerPair]chanevents.ForwardingAbility,
				error) {

				return map[chanevents.PeerPair]chanevents.ForwardingAbility{
					{
						PeerIn:  peerIn,
						PeerOut: peerOut,
					}: {
						EffectiveUptime: 90 * time.Second,
						ForwardedAmount: 550,
						FeeMsat:         1100,
						Forwards:        2,
					},
				}, nil
			},
			req: &frdrpc.ForwardingAbilityRequest{
				StartTime:         100,
				EndTime:           200,
				LiquidityFloorSat: 1000,
			},
			wantCode: codes.OK,
			check: func(t *testing.T,
				resp *frdrpc.ForwardingAbilityResponse,
				gotFloor btcutil.Amount) {

				// The explicit floor passes straight through.
				require.Equal(t, btcutil.Amount(1000), gotFloor)
				require.Len(t, resp.Peers, 2)
				require.Len(t, resp.Entries, 1)
				require.EqualValues(t, 100, resp.StartTime)
				require.EqualValues(t, 200, resp.EndTime)
				require.EqualValues(
					t, 90, resp.Entries[0].EffectiveUptimeS,
				)
				require.EqualValues(
					t, 550, resp.Entries[0].ForwardedSat,
				)
				require.EqualValues(
					t, 1100, resp.Entries[0].FeeMsat,
				)
				require.EqualValues(
					t, 2, resp.Entries[0].Forwards,
				)

				// An unset threshold echoes the server default,
				// and a forwarded pair leaves the bitmask empty.
				require.Equal(
					t, defaultUptimeThreshold,
					resp.UptimeThreshold,
				)
				require.Empty(t, resp.UpButIdleBitmask)
			},
		},
		{
			name: "unset floor uses server default",
			analyzer: func() (
				map[chanevents.PeerPair]chanevents.ForwardingAbility,
				error) {

				// Return a fully-up pair so the node-down guard
				// passes and the default floor can be observed.
				return map[chanevents.PeerPair]chanevents.ForwardingAbility{
					{PeerIn: peerIn, PeerOut: peerOut}: {
						EffectiveUptime: 100 * time.Second,
					},
				}, nil
			},
			req: &frdrpc.ForwardingAbilityRequest{
				StartTime: 100,
				EndTime:   200,
			},
			wantCode: codes.OK,
			check: func(t *testing.T,
				_ *frdrpc.ForwardingAbilityResponse,
				gotFloor btcutil.Amount) {

				require.Equal(
					t, btcutil.Amount(
						defaultLiquidityFloorSat,
					), gotFloor,
				)
			},
		},
		{
			name: "node down trips guard",
			analyzer: func() (
				map[chanevents.PeerPair]chanevents.ForwardingAbility,
				error) {

				// A single pair, up well below the default 0.9
				// threshold over the 100s window and with no
				// forwards, leaves nothing that clears it.
				return map[chanevents.PeerPair]chanevents.ForwardingAbility{
					{PeerIn: peerIn, PeerOut: peerOut}: {
						EffectiveUptime: 10 * time.Second,
					},
				}, nil
			},
			req: &frdrpc.ForwardingAbilityRequest{
				StartTime: 100,
				EndTime:   200,
			},
			wantCode: codes.FailedPrecondition,
		},
		{
			name: "low-uptime forward does not rescue guard",
			analyzer: func() (
				map[chanevents.PeerPair]chanevents.ForwardingAbility,
				error) {

				// Forwarded volume at sub-threshold uptime must
				// not satisfy the guard.
				return map[chanevents.PeerPair]chanevents.ForwardingAbility{
					{PeerIn: peerIn, PeerOut: peerOut}: {
						EffectiveUptime: 10 * time.Second,
						ForwardedAmount: 999,
					},
				}, nil
			},
			req: &frdrpc.ForwardingAbilityRequest{
				StartTime: 100,
				EndTime:   200,
			},
			wantCode: codes.FailedPrecondition,
		},
		{
			name: "explicit threshold flags up-but-idle pair",
			analyzer: func() (
				map[chanevents.PeerPair]chanevents.ForwardingAbility,
				error) {

				return map[chanevents.PeerPair]chanevents.ForwardingAbility{
					{PeerIn: peerIn, PeerOut: peerOut}: {
						EffectiveUptime: 60 * time.Second,
					},
				}, nil
			},
			req: &frdrpc.ForwardingAbilityRequest{
				StartTime:       100,
				EndTime:         200,
				UptimeThreshold: 0.5,
			},
			wantCode: codes.OK,
			check: func(t *testing.T,
				resp *frdrpc.ForwardingAbilityResponse,
				_ btcutil.Amount) {

				// Up 60s of a 100s window at a 0.5 threshold:
				// idle, so a bit and no entry.
				require.Equal(t, 0.5, resp.UptimeThreshold)
				require.Empty(t, resp.Entries)
				require.NotEmpty(t, resp.UpButIdleBitmask)
			},
		},
		{
			name: "out of range threshold is rejected",
			analyzer: func() (
				map[chanevents.PeerPair]chanevents.ForwardingAbility,
				error) {

				return nil, nil
			},
			req: &frdrpc.ForwardingAbilityRequest{
				StartTime:       100,
				EndTime:         200,
				UptimeThreshold: 1.5,
			},
			wantCode: codes.InvalidArgument,
		},
		{
			name: "start after end is rejected",
			req: &frdrpc.ForwardingAbilityRequest{
				StartTime: 200,
				EndTime:   100,
			},
			wantCode: codes.InvalidArgument,
		},
		{
			name: "missing analyzer is unavailable",
			req: &frdrpc.ForwardingAbilityRequest{
				StartTime:         100,
				EndTime:           200,
				LiquidityFloorSat: 1000,
			},
			wantCode: codes.Unavailable,
		},
		{
			name: "analyzer error is internal",
			analyzer: func() (
				map[chanevents.PeerPair]chanevents.ForwardingAbility,
				error) {

				return nil, errors.New("db lookup failed")
			},
			req: &frdrpc.ForwardingAbilityRequest{
				StartTime:         100,
				EndTime:           200,
				LiquidityFloorSat: 1000,
			},
			wantCode: codes.Internal,
		},
	}

	for _, tc := range tests {
		t.Run(
			tc.name,
			func(t *testing.T) {
				var gotFloor btcutil.Amount

				cfg := &Config{}
				if tc.analyzer != nil {
					cfg.ForwardingAnalyzer = &mockForwardingAnalyzer{
						effectiveUptimeFunc: func(
							_ context.Context, _,
							_ time.Time,
							floor btcutil.Amount) (
							map[chanevents.PeerPair]chanevents.ForwardingAbility,
							error) {

							gotFloor = floor

							return tc.analyzer()
						},
					}
				}

				server := NewRPCServer(cfg)
				resp, err := server.ForwardingAbility(
					t.Context(), tc.req,
				)

				if tc.wantCode != codes.OK {
					st, ok := status.FromError(err)
					require.True(t, ok)
					require.Equal(t, tc.wantCode, st.Code())

					return
				}

				require.NoError(t, err)
				require.NotNil(t, resp)
				if tc.check != nil {
					tc.check(t, resp, gotFloor)
				}
			},
		)
	}
}
