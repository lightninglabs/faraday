package accounting

import (
	"testing"
	"time"

	"github.com/lightninglabs/lndclient"
	"github.com/lightningnetwork/lnd/channeldb"
	"github.com/lightningnetwork/lnd/lnrpc"
	"github.com/stretchr/testify/require"
)

var (
	startTime   int64 = 100000
	inRangeTime int64 = 200000
	endTime     int64 = 300000
)

// TestInRange tests filtering of timestamps by a inclusive start time and
// exclusive end time.
func TestInRange(t *testing.T) {
	tests := []struct {
		name      string
		timestamp int64
		inRange   bool
	}{
		{
			name:      "before start time - not in range",
			timestamp: startTime - 100,
			inRange:   false,
		},
		{
			name:      "equals start time - ok",
			timestamp: startTime,
			inRange:   true,
		},
		{
			name:      "between start and end - ok",
			timestamp: inRangeTime,
			inRange:   true,
		},
		{
			name:      "equals end time - not in range",
			timestamp: endTime,
			inRange:   false,
		},
		{
			name:      "after end time - not in range",
			timestamp: endTime + 10,
			inRange:   false,
		},
	}

	for _, test := range tests {
		test := test

		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			start := time.Unix(startTime, 0)
			end := time.Unix(endTime, 0)
			ts := time.Unix(test.timestamp, 0)

			inRange := inRange(ts, start, end)
			require.Equal(t, test.inRange, inRange)
		})
	}
}

// TestFilterOnChain tests filtering transactions based on timestamp and
// confirmations.
func TestFilterOnChain(t *testing.T) {
	// Create three test transactions, one confirmed but outside of our
	// range, one confirmed and in our range and one in our range but not
	// confirmed.
	confirmedTxOutOfRange := lndclient.Transaction{
		Timestamp:     time.Unix(startTime-10, 0),
		Confirmations: 1,
	}

	confirmedTx := lndclient.Transaction{
		Timestamp:     time.Unix(inRangeTime, 0),
		Confirmations: 1,
	}

	noConfTx := lndclient.Transaction{
		Timestamp:     time.Unix(inRangeTime, 0),
		Confirmations: 0,
	}

	start := time.Unix(startTime, 0)
	end := time.Unix(endTime, 0)

	unfiltered := []lndclient.Transaction{
		confirmedTx, noConfTx, confirmedTxOutOfRange,
	}
	filtered := filterOnChain(start, end, unfiltered)

	// We only expect our confirmed transaction in the time range we
	// specified to be included.
	expected := []lndclient.Transaction{confirmedTx}
	require.Equal(t, expected, filtered)
}

// TestFilterInvoices tests filtering out of invoices that are not settled.
func TestFilterInvoices(t *testing.T) {
	inRange := time.Unix(inRangeTime, 0)

	// Create two invoices within our desired time range, one that is
	// settled and one that was cancelled and an invoice outside of our
	// time range that is settled.
	settledInvoice := lndclient.Invoice{
		SettleDate: inRange,
		State:      channeldb.ContractSettled,
	}

	invoices := []lndclient.Invoice{
		settledInvoice,
		{
			SettleDate: inRange,
			State:      channeldb.ContractCanceled,
		},
		{
			SettleDate: time.Unix(startTime-1, 0),
			State:      channeldb.ContractSettled,
		},
	}

	start := time.Unix(startTime, 0)
	end := time.Unix(endTime, 0)

	filtered := filterInvoices(start, end, invoices)

	// We only expect the settled invoice to be included.
	expected := []lndclient.Invoice{
		settledInvoice,
	}

	require.Equal(t, expected, filtered)
}

// TestFilterPayments tests filtering of payments based on their htlc
// timestamps.
func TestFilterPayments(t *testing.T) {
	// Fix current time for testing.
	now := time.Now()

	startTime := now.Add(time.Hour * -2)
	endTime := now.Add(time.Hour * 2)

	beforeStart := startTime.Add(time.Hour * -1)
	inRange := startTime.Add(time.Hour)
	afterEnd := endTime.Add(time.Hour)

	// succeededAfterPeriod is a payment which had a htlc in our period,
	// but only succeeded afterwards.
	succeededAfterPeriod := lndclient.Payment{
		Status: &lndclient.PaymentStatus{
			State: lnrpc.Payment_SUCCEEDED,
		},
		Htlcs: []*lnrpc.HTLCAttempt{
			{
				Status:        lnrpc.HTLCAttempt_FAILED,
				ResolveTimeNs: inRange.UnixNano(),
			},
			{
				Status:        lnrpc.HTLCAttempt_SUCCEEDED,
				ResolveTimeNs: afterEnd.UnixNano(),
			},
		},
	}

	// succeededInPeriod is a payment that had a failed htlc outside of our
	// period, but was settled in relevant period.
	succeededInPeriod := lndclient.Payment{
		Status: &lndclient.PaymentStatus{
			State: lnrpc.Payment_SUCCEEDED,
		},
		Htlcs: []*lnrpc.HTLCAttempt{
			{
				Status:        lnrpc.HTLCAttempt_FAILED,
				ResolveTimeNs: beforeStart.UnixNano(),
			},
			{
				Status:        lnrpc.HTLCAttempt_SUCCEEDED,
				ResolveTimeNs: inRange.UnixNano(),
			},
		},
	}

	// succeededInAndAfterPeriod is a payment that had successful htlc in
	// our period, but its last htlc was settled after our period.
	succeededInAndAfterPeriod := lndclient.Payment{
		Status: &lndclient.PaymentStatus{
			State: lnrpc.Payment_SUCCEEDED,
		},
		Htlcs: []*lnrpc.HTLCAttempt{
			{
				Status:        lnrpc.HTLCAttempt_SUCCEEDED,
				ResolveTimeNs: inRange.UnixNano(),
			},
			{
				Status:        lnrpc.HTLCAttempt_SUCCEEDED,
				ResolveTimeNs: afterEnd.UnixNano(),
			},
		},
	}

	// inFlight has a htlc in the relevant period but it is not settled yet.
	inFlight := lndclient.Payment{
		Status: &lndclient.PaymentStatus{
			State: lnrpc.Payment_IN_FLIGHT,
		},
		Htlcs: []*lnrpc.HTLCAttempt{
			{
				Status:        lnrpc.HTLCAttempt_SUCCEEDED,
				ResolveTimeNs: inRange.UnixNano(),
			},
		},
	}

	payments := []lndclient.Payment{
		succeededInPeriod,
		succeededAfterPeriod,
		succeededInAndAfterPeriod,
		inFlight,
	}

	filtered := filterPayments(startTime, endTime, payments)

	// We only expect the payment that had its last successful htlc in the
	// relevant period to be included. Some rounding occurs when we go
	// from the rpc payment unix nanoseconds to a golang time struct, so
	// we round our settle time so that the two will be equal.
	expected := []settledPayment{
		{
			Payment:    succeededInPeriod,
			settleTime: time.Unix(0, inRange.UnixNano()),
		},
	}

	require.Equal(t, filtered, expected)
}
