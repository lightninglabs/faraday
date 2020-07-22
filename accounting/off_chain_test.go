package accounting

import (
	"testing"
	"time"

	"github.com/lightninglabs/lndclient"
	"github.com/lightningnetwork/lnd/lnrpc"
	"github.com/lightningnetwork/lnd/lntypes"
	"github.com/lightningnetwork/lnd/routing/route"
	"github.com/stretchr/testify/require"
)

var (
	ourPK        = "03abfbad2e4387e73175949ba8b8d42e1101f4a21a73567da12b730a05db8a4f15"
	ourPubKey, _ = route.NewVertexFromStr(ourPK)

	otherPK        = "0349f7019b9c48bc456f011d17538a242f763bbc5759362f200854154113318727"
	otherPubkey, _ = route.NewVertexFromStr(otherPK)

	paymentHash1 = "673507764b0ad03443d07e7446b884d6d908aa783ee5e2704fbabc09ada79a79"
	hash1, _     = lntypes.MakeHashFromStr(paymentHash1)

	paymentHash2 = "a5530c5930b9eb7ea4284bcff39da52c6bca3103fc790749eb632911edc7143b"
	hash2, _     = lntypes.MakeHashFromStr(paymentHash2)

	hopToUs = &lnrpc.Hop{
		PubKey: ourPK,
	}

	hopToOther = &lnrpc.Hop{
		PubKey: otherPK,
	}

	routeToUs = &lnrpc.Route{
		Hops: []*lnrpc.Hop{
			hopToOther,
			hopToUs,
		},
	}

	routeToOther = &lnrpc.Route{
		Hops: []*lnrpc.Hop{
			hopToUs,
			hopToOther,
		},
	}
)

// TestGetCircularPayments tests detection of payments that are made to
// ourselves based on the destination pubkey in the payment's htlc attempts.
func TestGetCircularPayments(t *testing.T) {
	tests := []struct {
		name string

		// Payments is the set of payments that we examine for circular
		// payments.
		payments []paymentInfo

		// circular is the set of circular payments we expect to be
		// returned.
		circular map[string]bool

		// err is the error we expect the function to return.
		err error
	}{
		{
			// Test the case where we have a legacy payment that
			// we do not know the destination for.
			name: "legacy payment, no destination",
			payments: []paymentInfo{
				{
					destination: nil,
				},
			},
			circular: make(map[string]bool),
			err:      nil,
		},
		{
			// Test the case where we have a settled legacy payment
			// with a payment request to ourselves, which allows us
			// to identify it as circular.
			name: "legacy, has payment request",
			payments: []paymentInfo{
				{
					Payment: lndclient.Payment{
						Hash: hash1,
					},
					destination: &ourPubKey,
				},
			},
			circular: map[string]bool{
				paymentHash1: true,
			},
			err: nil,
		},
		{
			name: "duplicates both to us",
			payments: []paymentInfo{
				{
					Payment: lndclient.Payment{
						Hash: hash1,
					},
					destination: &ourPubKey,
				},
				{
					Payment: lndclient.Payment{
						Hash: hash1,
					},
					destination: &ourPubKey,
				},
			},
			circular: map[string]bool{
				paymentHash1: true,
			},
			err: nil,
		},
		{
			name: "duplicates not both to us",
			payments: []paymentInfo{
				{
					Payment: lndclient.Payment{
						Hash: hash1,
					},
					destination: &ourPubKey,
				},
				{
					Payment: lndclient.Payment{
						Hash: hash1,
					},
					destination: &otherPubkey,
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

// TestOffChainReport tests creation of our off chain report for a given set of
// payments, invoices and forwards. It uses a mocked price function so that the
// test does not make live price API calls.
func TestOffChainReport(t *testing.T) {
	// status is a non-nil success state that is used to prevent payments
	// from panicking on status checks (which are irrelevant for this test).
	status := &lndclient.PaymentStatus{
		State: lnrpc.Payment_SUCCEEDED,
	}

	tests := []struct {
		name string

		// Payments is the set of payments our ListPayments call should
		// return.
		payments []lndclient.Payment

		// err is the error we expect to be returned.
		err error
	}{
		{
			name: "No duplicate payments",
			payments: []lndclient.Payment{
				{
					Hash:   hash1,
					Status: status,
				},
				{
					Hash:   hash2,
					Status: status,
				},
			},
		},
		{
			name: "Duplicate payments both to ourself",
			payments: []lndclient.Payment{
				{
					Hash:   hash1,
					Status: status,
				},
				{
					Hash:   hash1,
					Status: status,
				},
			},
		},
	}

	for _, test := range tests {
		test := test

		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			// Create a config that returns our test set of
			// payments.
			cfg := &OffChainConfig{
				ListInvoices: func() ([]lndclient.Invoice, error) {
					return nil, nil
				},
				ListPayments: func() ([]lndclient.Payment, error) {
					return test.payments, nil
				},
				ListForwards: func() ([]lndclient.ForwardingEvent,
					error) {

					return nil, nil
				},
				CommonConfig: CommonConfig{
					StartTime: time.Unix(startTime, 0),
					EndTime:   time.Unix(endTime, 0),
				},
			}

			_, err := offChainReportWithPrices(cfg, mockPrice)
			require.Equal(t, test.err, err)
		})
	}
}
