package accounting

import (
	"fmt"
	"time"

	"github.com/lightninglabs/faraday/fiat"
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

	// Category indicates whether the entry is part of a custom category.
	Category string

	// OnChain indicates whether the transaction occurred on or off chain.
	OnChain bool

	// Credit is true if the amount listed is a credit, and false if it is
	// a debit.
	Credit bool

	// BTCPrice is the timestamped bitcoin price we used to get our fiat
	// value.
	BTCPrice *fiat.Price
}

// newHarmonyEntry produces a harmony entry. If provided with a negative amount,
// it will produce a record for a debit with the absolute value set in the
// amount field. Likewise, the fiat price will be obtained from the positive
// value. If passed a positive value, an entry for a credit will be made, and no
// changes to the amount will be made. Zero value entries will be recorded as
// a credit.
func newHarmonyEntry(ts time.Time, amountMsat int64, e EntryType, txid,
	reference, note, category string, onChain bool,
	convert fiatPrice) (*HarmonyEntry,

	error) {

	var (
		absAmt = amountMsat
		credit = true
	)

	if absAmt < 0 {
		absAmt *= -1
		credit = false
	}

	btcPrice, err := convert(ts)
	if err != nil {
		return nil, err
	}
	amtMsat := lnwire.MilliSatoshi(absAmt)

	return &HarmonyEntry{
		Timestamp: ts,
		Amount:    amtMsat,
		FiatValue: fiat.MsatToFiat(btcPrice.Price, amtMsat),
		TxID:      txid,
		Reference: reference,
		Note:      note,
		Type:      e,
		Category:  category,
		OnChain:   onChain,
		Credit:    credit,
		BTCPrice:  btcPrice,
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

	// EntryTypeReceipt indicates that we have received a payment. Off
	// chain, this receipt is an invoice that we were paid via lightning.
	// On chain, this receipt is an on chain transaction paying into our
	// wallet.
	EntryTypeReceipt

	// EntryTypePayment indicates that we have made a payment. Off chain,
	// this payment is a lightning payment to an invoice. On chain, this
	// receipt is an on chain transaction paying from our wallet.
	EntryTypePayment

	// EntryTypeFee represent fees paid for on chain transactions or off
	// chain routing. Note that this entry type excludes fees for channel
	// opens and closes.
	EntryTypeFee

	// EntryTypeCircularReceipt represents an invoice that we paid to
	// ourselves. This occurs when circular payments are used to rebalance
	// channels.
	EntryTypeCircularReceipt

	// EntryTypeForward represents a forward through our node.
	EntryTypeForward

	// EntryTypeForwardFee represents the fees we earned forwarding a
	// payment.
	EntryTypeForwardFee

	// EntryTypeCircularPayment represents an operational payment which
	// we pay to ourselves to rebalance channels.
	EntryTypeCircularPayment

	// EntryTypeCircularPaymentFee represents a the fees paid on an
	// operational payment paid to ourselves to rebalance channels.
	EntryTypeCircularPaymentFee

	// EntryTypeSweep represents an on chain payment which swept funds
	// back to our own wallet.
	EntryTypeSweep

	// EntryTypeSweepFee represents the fees that were paid to sweep funds
	// back to our own wallet.
	EntryTypeSweepFee

	// EntryTypeChannelCloseFee represents fees our node paid to close a
	// channel.
	EntryTypeChannelCloseFee
)

// String returns the string representation of an entry type.
func (e EntryType) String() string {
	switch e {
	case EntryTypeLocalChannelOpen:
		return "local channel open"

	case EntryTypeRemoteChannelOpen:
		return "remote channel open"

	case EntryTypeChannelOpenFee:
		return "channel open fee"

	case EntryTypeChannelClose:
		return "channel close fee"

	case EntryTypeReceipt:
		return "receipt"

	case EntryTypePayment:
		return "payment"

	case EntryTypeFee:
		return "fee"

	case EntryTypeCircularReceipt:
		return "circular payment receipt"

	case EntryTypeForward:
		return "forward"

	case EntryTypeForwardFee:
		return "forward fee"

	case EntryTypeCircularPayment:
		return "circular payment"

	case EntryTypeCircularPaymentFee:
		return "circular payment fee"

	case EntryTypeSweep:
		return "sweep"

	case EntryTypeSweepFee:
		return "sweep fee"

	case EntryTypeChannelCloseFee:
		return "channel close fee"

	default:
		return fmt.Sprintf("unknown: %d", e)
	}
}
