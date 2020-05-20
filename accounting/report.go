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

// newHarmonyEntry produces a harmony entry. If provided with a negative amount,
// it will produce a record for a debit with the absolute value set in the
// amount field. Likewise, the fiat price will be obtained from the positive
// value. If passed a positive value, an entry for a credit will be made, and no
// changes to the amount will be made. Zero value entries will be recorded as
// a credit.
// nolint:unparam
func newHarmonyEntry(ts int64, amountMsat int64, e EntryType, txid, reference,
	note string, onChain bool, convert msatToFiat) (*HarmonyEntry, error) {

	var (
		absAmt = amountMsat
		credit = true
	)

	if absAmt < 0 {
		absAmt *= -1
		credit = false
	}

	fiat, err := convert(absAmt, ts)
	if err != nil {
		return nil, err
	}

	return &HarmonyEntry{
		Timestamp: time.Unix(ts, 0),
		Amount:    lnwire.MilliSatoshi(absAmt),
		FiatValue: fiat,
		TxID:      txid,
		Reference: reference,
		Note:      note,
		Type:      e,
		OnChain:   onChain,
		Credit:    credit,
	}, nil
}

// EntryType indicates the lightning specific type of an entry.
type EntryType int

const (
	_ EntryType = iota

	// EntryTypeLocalChannelOpen represents the funding transaction we
	// created to open a channel to a remote peer.
	EntryTypeLocalChannelOpen

	// EntryTypeRemoteChannelOpen represents the funding transaction that
	// our peer created to open a channel to us.
	EntryTypeRemoteChannelOpen

	// EntryTypeChannelOpenFee records the fees we paid on chain when
	// opening a channel to a remote peer.
	EntryTypeChannelOpenFee

	// EntryTypeChannelClose represents a channel closing transaction. If
	// we were paid out a balance by this transaction, the entry will
	// contain that amount. Note that the on chain resolutions required to
	// resolve a force close are not contained in this category. If we
	// force closed, our own balance will also require further on chain
	// resolution, so it will not be included.
	EntryTypeChannelClose
)
