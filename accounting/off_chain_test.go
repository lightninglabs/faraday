package accounting

import (
	"testing"

	"github.com/lightningnetwork/lnd/lnrpc"
	"github.com/stretchr/testify/require"
)

var (
	ourPubKey   = "03abfbad2e4387e73175949ba8b8d42e1101f4a21a73567da12b730a05db8a4f15"
	otherPubkey = "0349f7019b9c48bc456f011d17538a242f763bbc5759362f200854154113318727"

	paymentHash1 = "673507764b0ad03443d07e7446b884d6d908aa783ee5e2704fbabc09ada79a79"
)

// TestGetCircularPayments tests detection of payments that are made to
// ourselves based on the destination pubkey in the payment's htlc attempts.
func TestGetCircularPayments(t *testing.T) {
	hopToUs := &lnrpc.Hop{
		PubKey: ourPubKey,
	}

	hopToOther := &lnrpc.Hop{
		PubKey: otherPubkey,
	}

	routeToUs := &lnrpc.Route{
		Hops: []*lnrpc.Hop{
			hopToOther,
			hopToUs,
		},
	}

	routeToOther := &lnrpc.Route{
		Hops: []*lnrpc.Hop{
			hopToUs,
			hopToOther,
		},
	}

	tests := []struct {
		name string

		// Payments is the set of payments that we examine for circular
		// payments.
		payments []*lnrpc.Payment

		// circular is the set of circular payments we expect to be
		// returned.
		circular map[string]bool

		// err is the error we expect the function to return.
		err error
	}{
		{
			// This test case is added to cover a race where we
			// have just initiated a payment in lnd and do not
			// have any htlcs in flight. This payment cannot have
			// succeeded yet, so it is not relevant to our
			// accounting period.
			name: "Payment has no htlcs",
			payments: []*lnrpc.Payment{
				{
					PaymentHash: paymentHash1,
				},
			},
			circular: make(map[string]bool),
			err:      nil,
		},
		{
			name: "Route has no hops",
			payments: []*lnrpc.Payment{
				{
					PaymentHash: paymentHash1,
					Htlcs: []*lnrpc.HTLCAttempt{
						{
							Route: &lnrpc.Route{},
						},
					},
				},
			},
			circular: nil,
			err:      errNoHops,
		},
		{
			name: "Last Hop to Us",
			payments: []*lnrpc.Payment{
				{
					PaymentHash: paymentHash1,
					Htlcs: []*lnrpc.HTLCAttempt{
						{
							Route: routeToUs,
						},
					},
				},
			},
			circular: map[string]bool{
				paymentHash1: true,
			},
			err: nil,
		},
		{
			name: "Last Hop not to Us",
			payments: []*lnrpc.Payment{
				{
					PaymentHash: paymentHash1,
					Htlcs: []*lnrpc.HTLCAttempt{
						{
							Route: routeToOther,
						},
					},
				},
			},
			circular: make(map[string]bool),
			err:      nil,
		},
		{
			name: "Duplicates both to us",
			payments: []*lnrpc.Payment{
				{
					PaymentHash: paymentHash1,
					Htlcs: []*lnrpc.HTLCAttempt{
						{
							Route: routeToUs,
						},
					},
				},
				{
					PaymentHash: paymentHash1,
					Htlcs: []*lnrpc.HTLCAttempt{
						{
							Route: routeToUs,
						},
					},
				},
			},
			circular: map[string]bool{
				paymentHash1: true,
			},
			err: nil,
		},
		{
			name: "Duplicates not both to us",
			payments: []*lnrpc.Payment{
				{
					PaymentHash: paymentHash1,
					Htlcs: []*lnrpc.HTLCAttempt{
						{
							Route: routeToUs,
						},
					},
				},
				{
					PaymentHash: paymentHash1,
					Htlcs: []*lnrpc.HTLCAttempt{
						{
							Route: routeToOther,
						},
					},
				},
			},
			circular: nil,
			err:      errDifferentDuplicates,
		},
	}

	for _, test := range tests {
		test := test

		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			circular, err := getCircularPayments(
				ourPubKey, test.payments,
			)
			require.Equal(t, test.err, err)
			require.Equal(t, test.circular, circular)
		})
	}
}
