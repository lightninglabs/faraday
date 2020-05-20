package accounting

import (
	"time"

	"github.com/lightningnetwork/lnd/lnwire"
	"github.com/shopspring/decimal"
)

// Report contains a set of entries.
type Report []*HarmonyEntry

// HarmonyEntry represents a single action on our balance.
type HarmonyEntry struct {
	// Timestamp is the time at which the event occurred.
	// On chain events: timestamp will be obtained from the block timestamp.
	// Off chain events: timestamp will be obtained from lnd's records.
	Timestamp time.Time

	// Amount is the balance change incurred by this entry, expressed in
	// msat.
	Amount lnwire.MilliSatoshi

	// FiatValue is the fiat value of this entry's amount. This value is
	// expressed as a decimal so that we do not lose precision.
	FiatValue decimal.Decimal

	// TxID is the transaction ID of this entry.
	TxID string

	// Reference is a unique identifier for this entry, if available.
	Reference string

	// Note is an optional note field.
	Note string

	// Type describes the type of entry.
	Type EntryType

	// OnChain indicates whether the transaction occurred on or off chain.
	OnChain bool

	// Credit is true if the amount listed is a credit, and false if it is
	// a debit.
	Credit bool
}

// EntryType indicates the lightning specific type of an entry.
type EntryType int
