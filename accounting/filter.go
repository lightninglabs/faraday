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
