package frdrpcserver

import (
	"context"
	"math"
	"testing"
	"time"

	"github.com/lightninglabs/faraday/chanevents"
	"github.com/lightninglabs/faraday/frdrpc"
	"github.com/lightningnetwork/lnd/clock"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// TestForwardingAbilityValidation pins the input-validation branches of the
// ForwardingAbility handler to their expected gRPC status code and message.
// Every branch short-circuits before the analyzer is constructed, so the test
// needs only the cfg to reach the validator under test, not a working lnd or a
// populated chanevents store.
func TestForwardingAbilityValidation(t *testing.T) {
	t.Parallel()

	// An empty real store lets every case past the first guard reach the
	// validator under test. The nil-store case exercises that guard itself.
	clk := clock.NewTestClock(time.Unix(1, 0))
	store := chanevents.NewTestDB(t, clk)

	tests := []struct {
		name     string
		store    *chanevents.Store
		req      *frdrpc.ForwardingAbilityRequest
		wantCode codes.Code
		wantMsg  string
	}{
		{
			name:  "store not configured",
			store: nil,
			req: &frdrpc.ForwardingAbilityRequest{
				StartTime: 1, EndTime: 2,
			},
			wantCode: codes.FailedPrecondition,
			wantMsg:  "channel events store not configured",
		},
		{
			name:  "start_time over MaxInt64",
			store: store,
			req: &frdrpc.ForwardingAbilityRequest{
				StartTime: math.MaxInt64 + 1,
				EndTime:   math.MaxInt64,
			},
			wantCode: codes.InvalidArgument,
			wantMsg:  "start_time and end_time must be <= MaxInt64",
		},
		{
			name:  "end_time over MaxInt64",
			store: store,
			req: &frdrpc.ForwardingAbilityRequest{
				StartTime: 1,
				EndTime:   math.MaxInt64 + 1,
			},
			wantCode: codes.InvalidArgument,
			wantMsg:  "start_time and end_time must be <= MaxInt64",
		},
		{
			name:  "percentile below zero",
			store: store,
			req: &frdrpc.ForwardingAbilityRequest{
				StartTime: 1, EndTime: 2,
				ForwardPercentile: -0.1,
			},
			wantCode: codes.InvalidArgument,
			wantMsg:  "forward_percentile must be in [0, 100]",
		},
		{
			name:  "percentile above hundred",
			store: store,
			req: &frdrpc.ForwardingAbilityRequest{
				StartTime: 1, EndTime: 2,
				ForwardPercentile: 100.1,
			},
			wantCode: codes.InvalidArgument,
			wantMsg:  "forward_percentile must be in [0, 100]",
		},
	}

	for _, tc := range tests {
		t.Run(
			tc.name,
			func(t *testing.T) {
				t.Parallel()

				s := &RPCServer{cfg: &Config{ChanEvents: tc.store}}

				resp, err := s.ForwardingAbility(
					context.Background(), tc.req,
				)
				require.Nil(t, resp)
				require.Error(t, err)

				st, ok := status.FromError(err)
				require.True(
					t, ok, "expected gRPC status error",
				)
				require.Equal(t, tc.wantCode, st.Code())
				require.Contains(t, st.Message(), tc.wantMsg)
			},
		)
	}
}
