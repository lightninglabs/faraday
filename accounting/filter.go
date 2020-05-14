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

// filterForwardingEvents filters out forwarding events that are not in our
// desired time range.
func filterForwardingEvents(startTime, endTime time.Time,
	events []*lnrpc.ForwardingEvent) []*lnrpc.ForwardingEvent {

	var filtered []*lnrpc.ForwardingEvent

	for _, fwd := range events {
		ts := time.Unix(int64(fwd.Timestamp), 0)

		if !inRange(ts, startTime, endTime) {
			continue
		}

		filtered = append(filtered, fwd)
	}

	return filtered
}
