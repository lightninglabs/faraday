package accounting

import (
	"time"

	"github.com/lightningnetwork/lnd/lnrpc"
)

// inRange returns a boolean that indicates whether a timestamp lies in a
// range with an inclusive start time and exclusive end time.
func inRange(timestamp, startTime, endTime time.Time) bool {
	// Our start time is inclusive, skip any transactions that are
	// strictly before our start time.
	if timestamp.Before(startTime) {
		return false
	}

	// Our end time is exclusive, so we skip any transactions that
	// are after or equal to our end time.
	if !timestamp.Before(endTime) {
		return false
	}

	return true
}

// filterOnChain filters a set of on chain transactions to get only those
// which lie within [startTime, endTime). Unconfirmed transactions are also
// excluded from this set.
func filterOnChain(startTime, endTime time.Time,
	txns []*lnrpc.Transaction) []*lnrpc.Transaction {

	// nolint: prealloc
	var filtered []*lnrpc.Transaction

	for _, tx := range txns {
		timestamp := time.Unix(tx.TimeStamp, 0)

		// Unconfirmed transactions are listed with 0 confirmations,
		// they have no timestamp so we skip them.
		if tx.NumConfirmations == 0 {
			continue
		}

		if !inRange(timestamp, startTime, endTime) {
			continue
		}

		filtered = append(filtered, tx)
	}

	return filtered
}

// filterInvoices filters out unsettled invoices and those that are outside of
// our desired time range.
func filterInvoices(startTime, endTime time.Time,
	invoices []*lnrpc.Invoice) []*lnrpc.Invoice {

	// nolint: prealloc
	var filtered []*lnrpc.Invoice

	for _, invoice := range invoices {
		// If the invoice was not settled, we do not need to create an
		// entry for it.
		if invoice.State != lnrpc.Invoice_SETTLED {
			continue
		}

		settleTs := time.Unix(invoice.SettleDate, 0)
		if !inRange(settleTs, startTime, endTime) {
			continue
		}

		filtered = append(filtered, invoice)
	}

	return filtered
}

// settledPayment contains a payment and the timestamp of the latest settled
// htlc. Payments do not have a settle time, so we have to get our settle time
// from examining each htlc.
type settledPayment struct {
	*lnrpc.Payment
	settleTime time.Time
}

// filterPayments filters out unsuccessful payments and those which did not
// occur within the range we specify. Since we now allow multi-path payments,
// a single payment may have multiple htlcs resolved over a period of time.
// We use the most recent settle time for payment to classify whether the
// payment occurred within our desired time range, because payments are not
// considered settled until all the htlcs are resolved.
func filterPayments(startTime, endTime time.Time,
	payments []*lnrpc.Payment) []settledPayment {

	// nolint: prealloc
	var filtered []settledPayment

	for _, payment := range payments {
		// If the payment did not succeed, we can skip it.
		if payment.Status != lnrpc.Payment_SUCCEEDED {
			continue
		}

		// We run through each htlc for this payment and get the latest
		// resolution time for a successful htlc. This is the time we
		// will use to determine whether this payment lies in the period
		// we are looking at.
		var latestTimeNs int64
		for _, htlc := range payment.Htlcs {
			if htlc.Status != lnrpc.HTLCAttempt_SUCCEEDED {
				continue
			}

			if htlc.ResolveTimeNs > latestTimeNs {
				latestTimeNs = htlc.ResolveTimeNs
			}
		}

		// Skip the payment if the oldest settle time is not within the
		// range we are looking at.
		ts := time.Unix(0, latestTimeNs)
		if !inRange(ts, startTime, endTime) {
			continue
		}

		// Add a settled payment to our set of settled payments with its
		// timestamp.
		filtered = append(filtered, settledPayment{
			Payment:    payment,
			settleTime: ts,
		})
	}

	return filtered
}
