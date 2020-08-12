package accounting

import (
	"testing"
	"time"

	"github.com/lightninglabs/lndclient"
	"github.com/lightningnetwork/lnd/channeldb"
	"github.com/lightningnetwork/lnd/lnrpc"
	"github.com/lightningnetwork/lnd/routing/route"
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
	filtered, err := filterOnChain(start, end, unfiltered)
	require.NoError(t, err)

	// We only expect our confirmed transaction in the time range we
	// specified to be included.
	expected := []lndclient.Transaction{confirmedTx}
	require.Equal(t, expected, filtered)

	// Test the case where we have a receive which has a non-zero fee.
	receiveWithFee := []lndclient.Transaction{
		{
			Timestamp:     time.Unix(inRangeTime, 0),
			Amount:        100,
			Fee:           10,
			Confirmations: 1,
		},
	}
	_, err = filterOnChain(start, end, receiveWithFee)
	require.Equal(t, ErrReceiveWithFee, err)

	// Finally, test that we subtract our fee amount off payments, since it
	// is double counted in lnd's api.
	payment := lndclient.Transaction{
		Timestamp:     time.Unix(inRangeTime, 0),
		Amount:        -100,
		Fee:           10,
		Confirmations: 1,
	}

	filtered, err = filterOnChain(
		start, end, []lndclient.Transaction{payment},
	)
	require.NoError(t, err)

	expctedAmount := payment.Amount + payment.Fee

	require.Equal(t, expctedAmount, filtered[0].Amount)

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
	now := time.Now()

	startTime := now.Add(time.Hour * -2)
	endTime := now.Add(time.Hour * 2)

	beforeStart := startTime.Add(time.Hour * -1)
	inRange := startTime.Add(time.Hour)
	afterEnd := endTime.Add(time.Hour)

	settledInRange := paymentInfo{
		Payment: lndclient.Payment{
			Status: &lndclient.PaymentStatus{
				State: lnrpc.Payment_SUCCEEDED,
			},
		},
		settleTime: inRange,
	}

	payments := []paymentInfo{
		settledInRange,
		{
			Payment: lndclient.Payment{
				Status: &lndclient.PaymentStatus{
					State: lnrpc.Payment_SUCCEEDED,
				},
			},
			settleTime: beforeStart,
		},
		{
			Payment: lndclient.Payment{
				Status: &lndclient.PaymentStatus{
					State: lnrpc.Payment_SUCCEEDED,
				},
			},
			settleTime: afterEnd,
		},
		{
			Payment: lndclient.Payment{
				Status: &lndclient.PaymentStatus{
					State: lnrpc.Payment_IN_FLIGHT,
				},
			},
			settleTime: beforeStart,
		},
		{
			Payment: lndclient.Payment{
				Status: &lndclient.PaymentStatus{
					State: lnrpc.Payment_FAILED,
				},
			},
			settleTime: inRange,
		},
		{
			Payment: lndclient.Payment{
				Status: &lndclient.PaymentStatus{
					State: lnrpc.Payment_FAILED,
				},
			},
			settleTime: afterEnd,
		},
	}
	expected := []paymentInfo{settledInRange}

	filtered := filterPayments(startTime, endTime, payments)
	require.Equal(t, expected, filtered)
}

// TestPreProcessPayments tests getting of destinations and settle timestamps
// for payments.
func TestPreProcessPayments(t *testing.T) {
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
				Route:         routeToUs,
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
				Route:         routeToUs,
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
				Route:         routeToOther,
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
				Route:         routeToUs,
			},
		},
	}

	payments := []lndclient.Payment{
		succeededInPeriod,
		succeededAfterPeriod,
		succeededInAndAfterPeriod,
		inFlight,
	}

	processed, err := preProcessPayments(payments, decode(true))
	require.NoError(t, err)

	// We only expect the payment that had its last successful htlc in the
	// relevant period to be included. Some rounding occurs when we go
	// from the rpc payment unix nanoseconds to a golang time struct, so
	// we round our settle time so that the two will be equal.
	expected := []paymentInfo{
		{
			Payment:     succeededInPeriod,
			destination: &ourPubKey,
			settleTime:  time.Unix(0, inRange.UnixNano()),
		},
		{
			Payment:     succeededAfterPeriod,
			destination: &ourPubKey,
			settleTime:  time.Unix(0, afterEnd.UnixNano()),
		},
		{
			Payment:     succeededInAndAfterPeriod,
			destination: &otherPubkey,
			settleTime:  time.Unix(0, afterEnd.UnixNano()),
		},
		{
			Payment:     inFlight,
			destination: &ourPubKey,
			settleTime:  time.Time{},
		},
	}

	require.Equal(t, processed, expected)
}

// Decode is a helper function which returns a decode function which will
// provide our own pubkey if toSelf is true, and another pubkey otherwise.
func decode(toSelf bool) func(_ string) (*lndclient.PaymentRequest,
	error) {

	return func(_ string) (*lndclient.PaymentRequest, error) {
		pubkey := ourPubKey
		if !toSelf {
			pubkey = otherPubkey
		}

		return &lndclient.PaymentRequest{
			Destination: pubkey,
		}, nil
	}
}

// TestPaymentHtlcDestination tests getting our payment destination from the
// payment's set of htlcs.
func TestPaymentHtlcDestination(t *testing.T) {
	tests := []struct {
		name  string
		dest  *route.Vertex
		htlcs []*lnrpc.HTLCAttempt
		err   error
	}{
		{
			name:  "no htlcs",
			htlcs: nil,
			dest:  nil,
			err:   errNoHtlcs,
		},
		{
			name: "route to us",
			htlcs: []*lnrpc.HTLCAttempt{
				{
					Route: routeToUs,
				},
			},
			dest: &ourPubKey,
			err:  nil,
		},
		{
			name: "route not to us",
			htlcs: []*lnrpc.HTLCAttempt{
				{
					Route: routeToOther,
				},
			},
			dest: &otherPubkey,
			err:  nil,
		},
	}

	for _, test := range tests {
		test := test

		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			// Create a payment with our test state and htlcs.
			payment := lndclient.Payment{
				Htlcs: test.htlcs,
			}

			dest, err := paymentHtlcDestination(payment)
			require.Equal(t, test.err, err)
			require.Equal(t, test.dest, dest)
		})
	}
}

// TestPaymentRequestDestination tests getting of payment destinations from our
// payment request.
func TestPaymentRequestDestination(t *testing.T) {
	tests := []struct {
		name           string
		paymentRequest string
		decode         decodePaymentRequest
		dest           *route.Vertex
		err            error
	}{
		{
			name:           "no payment request",
			decode:         decode(true),
			paymentRequest: "",
			dest:           nil,
			err:            errNoPaymentRequest,
		},
		{
			name:           "to self",
			decode:         decode(true),
			paymentRequest: paymentRequest,
			dest:           &ourPubKey,
			err:            nil,
		},
		{
			name:           "not to self",
			decode:         decode(false),
			paymentRequest: paymentRequest,
			dest:           &otherPubkey,
			err:            nil,
		},
	}

	for _, test := range tests {
		test := test

		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			dest, err := paymentRequestDestination(
				test.paymentRequest, test.decode,
			)
			require.Equal(t, test.err, err)
			require.Equal(t, test.dest, dest)
		})
	}
}
