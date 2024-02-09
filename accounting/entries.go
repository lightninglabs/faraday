package accounting

import (
	"fmt"
	"strings"

	"github.com/btcsuite/btcd/btcutil"
	"github.com/lightninglabs/lndclient"
	"github.com/lightningnetwork/lnd/lntypes"
	"github.com/lightningnetwork/lnd/lnwire"
	"github.com/lightningnetwork/lnd/routing/route"
)

// entryUtils contains the utility functions required to create an entry.
type entryUtils struct {
	// getFee looks up the fees for an on chain transaction, this function
	// may be nil.
	getFee getFeeFunc

	// getFiat provides a fiat price for the btc value provided at its
	// timestamp.
	getFiat fiatPrice

	// customCategories is a set of custom categories which are set for the
	// report.
	customCategories []CustomCategory
}

// FeeReference returns a special unique reference for the fee paid on a
// transaction. We use the reference of the original entry with :-1 to denote
// that this entry is associated with the original entry.
func FeeReference(reference string) string {
	return fmt.Sprintf("%v:-1", reference)
}

// channelOpenNote creates a note for a channel open entry type.
func channelOpenNote(initiator bool, remotePubkey string,
	capacity btcutil.Amount) string {

	if !initiator {
		return fmt.Sprintf("remote peer %v initated channel open "+
			"with capacity: %v sat", remotePubkey,
			capacity)
	}

	return fmt.Sprintf("initiated channel with remote peer: %v "+
		"capacity: %v sats", remotePubkey, capacity)
}

// channelOpenFeeNote creates a note for channel open types.
func channelOpenFeeNote(channelID lnwire.ShortChannelID) string {
	return fmt.Sprintf("fees to open channel: %v", channelID)
}

// channelOpenEntries produces entries for channel opens and their fees.
func channelOpenEntries(channel channelInfo, tx lndclient.Transaction,
	u entryUtils) ([]*HarmonyEntry, error) {

	// If the transaction has a negative amount, we can infer that this
	// transaction was a local channel open, because a remote party opening
	// a channel to us does not affect our balance.
	amtMsat := satsToMsat(tx.Amount)
	initiator := amtMsat < 0

	entryType := EntryTypeLocalChannelOpen
	if !initiator {
		entryType = EntryTypeRemoteChannelOpen
	}

	note := channelOpenNote(
		initiator, channel.pubKeyBytes.String(),
		channel.capacity,
	)

	category := getCategory(tx.Label, u.customCategories)

	openEntry, err := newHarmonyEntry(
		tx.Timestamp, amtMsat, entryType, tx.TxHash,
		channel.channelID.String(), note, category,
		true, u.getFiat,
	)
	if err != nil {
		return nil, err
	}

	// If we did not initiate opening the channel, we can just return the
	// channel open entry and do not need a fee entry.
	if !initiator {
		return []*HarmonyEntry{openEntry}, nil
	}

	// We also need an entry for the fees we paid for the on chain tx.
	// Transactions record fees in absolute amounts in sats, so we need
	// to convert fees to msat and flip it to a negative value so it
	// records as a debit.
	feeMsat := invertedSatsToMsats(tx.Fee)

	note = channelOpenFeeNote(channel.channelID)
	feeEntry, err := newHarmonyEntry(
		tx.Timestamp, feeMsat, EntryTypeChannelOpenFee, tx.TxHash,
		FeeReference(tx.TxHash), note, category, true, u.getFiat,
	)
	if err != nil {
		return nil, err
	}

	return []*HarmonyEntry{openEntry, feeEntry}, nil
}

// channelCloseNote creates a close note for a channel close entry type.
func channelCloseNote(channelID lnwire.ShortChannelID, closeType,
	initiated string) string {

	return fmt.Sprintf("close channel: %v close type: %v closed by: %v",
		channelID, closeType, initiated)
}

// closedChannelEntries produces the entries associated with a channel close.
// Note that this entry only reflects the balance we were directly paid out
// in the close transaction. It *does not* include any on chain resolutions, so
// it is excluding htlcs that are resolved on chain, and will not reflect our
// balance when we force close (because it is behind a timelock).
func closedChannelEntries(channel closedChannelInfo, tx lndclient.Transaction,
	u entryUtils) ([]*HarmonyEntry, error) {

	amtMsat := satsToMsat(tx.Amount)
	note := channelCloseNote(
		channel.channelID, channel.closeType, channel.closeInitiator,
	)

	category := getCategory(tx.Label, u.customCategories)

	closeEntry, err := newHarmonyEntry(
		tx.Timestamp, amtMsat, EntryTypeChannelClose, tx.TxHash,
		tx.TxHash, note, category, true, u.getFiat,
	)
	if err != nil {
		return nil, err
	}

	switch channel.initiator {
	// If the remote party opened the channel, we can just return the
	// channel close as is, because we did not pay fees for it.
	case lndclient.InitiatorRemote:
		return []*HarmonyEntry{closeEntry}, nil

	// If we originally opened the channel, we continue to create a fee
	// entry.
	case lndclient.InitiatorLocal:

	// If we do not know who opened the channel, we log a warning and
	// return. This is only expected to happen for channels closed by
	// lnd<0.9.
	default:
		log.Warnf("channel: %v initiator unknown, fee entry may be "+
			"missing", channel.channelPoint)

		return []*HarmonyEntry{closeEntry}, nil
	}

	// At this stage, we know that we have a channel close transaction where
	// we paid the fees (because we initiated the channel). If we do not
	// have a fee lookup function, we cannot get fees for this channel so
	// we log a warning and return without a fee entry.
	if u.getFee == nil {
		log.Warnf("no bitcoin backend provided to lookup fees, "+
			"channel close fee entry for: %v omitted",
			channel.channelPoint)

		return []*HarmonyEntry{closeEntry}, nil
	}

	fees, err := u.getFee(tx.Tx.TxHash())
	if err != nil {
		return nil, err
	}

	// Our fees are provided as a positive amount in sats. Convert this to
	// a negative msat value.
	feeAmt := invertedSatsToMsats(fees)

	feeEntry, err := newHarmonyEntry(
		tx.Timestamp, feeAmt, EntryTypeChannelCloseFee,
		tx.TxHash, FeeReference(tx.TxHash), "", category,
		true, u.getFiat,
	)
	if err != nil {
		return nil, err
	}

	return []*HarmonyEntry{closeEntry, feeEntry}, nil
}

// sweepEntries creates a sweep entry and looks up its fee to create a fee
// entry.
func sweepEntries(tx lndclient.Transaction, u entryUtils) ([]*HarmonyEntry, error) {
	category := getCategory(tx.Label, u.customCategories)

	txEntry, err := newHarmonyEntry(
		tx.Timestamp, satsToMsat(tx.Amount), EntryTypeSweep, tx.TxHash,
		tx.TxHash, tx.Label, category, true, u.getFiat,
	)
	if err != nil {
		return nil, err
	}

	// If we do not have a fee lookup function set, we log a warning that
	// we cannot record fees for the sweep transaction and return without
	// adding a fee entry.
	if u.getFee == nil {
		log.Warnf("no bitcoin backend provided to lookup fees, "+
			"sweep fee entry for: %v omitted", tx.TxHash)

		return []*HarmonyEntry{txEntry}, nil
	}

	fee, err := u.getFee(tx.Tx.TxHash())
	if err != nil {
		return nil, err
	}

	feeEntry, err := newHarmonyEntry(
		tx.Timestamp, invertedSatsToMsats(fee), EntryTypeSweepFee,
		tx.TxHash, FeeReference(tx.TxHash), "", category, true,
		u.getFiat,
	)
	if err != nil {
		return nil, err
	}

	return []*HarmonyEntry{txEntry, feeEntry}, nil
}

// isUtxoManagementTx checks whether a transaction is restructuring our utxos.
func isUtxoManagementTx(txn lndclient.Transaction) bool {
	// Check all inputs.
	for _, input := range txn.PreviousOutpoints {
		if !input.IsOurOutput {
			return false
		}
	}

	// Check all outputs.
	for _, output := range txn.OutputDetails {
		if !output.IsOurAddress {
			return false
		}
	}

	// If all inputs and outputs belong to our wallet, it's utxo management.
	return true
}

// createOnchainFeeEntry creates a fee entry for an on chain transaction.
func createOnchainFeeEntry(tx lndclient.Transaction, category string,
	note string, u entryUtils) (*HarmonyEntry, error) {

	// Total fees are expressed as a positive value in sats, we convert to
	// msat here and make the value negative so that it reflects as a
	// debit.
	feeAmt := invertedSatsToMsats(tx.Fee)

	feeEntry, err := newHarmonyEntry(
		tx.Timestamp, feeAmt, EntryTypeFee,
		tx.TxHash, FeeReference(tx.TxHash), note, category, true,
		u.getFiat,
	)

	if err != nil {
		return nil, err
	}

	return feeEntry, nil
}

// utxoManagementFeeNote creates a note for utxo management fee types.
func utxoManagementFeeNote(txid string) string {
	return fmt.Sprintf("fees for utxo management transaction: %v", txid)
}

// onChainEntries produces relevant entries for an on chain transaction.
func onChainEntries(tx lndclient.Transaction,
	u entryUtils) ([]*HarmonyEntry, error) {

	var (
		amtMsat        = satsToMsat(tx.Amount)
		entryType      EntryType
		category       = getCategory(tx.Label, u.customCategories)
		utxoManagement bool
	)

	// Determine the type of entry we are creating. If this is a sweep, we
	// set our fee as well, otherwise we set type based on the amount of the
	// transaction.
	switch {
	case amtMsat < 0:
		entryType = EntryTypePayment

	case amtMsat > 0:
		entryType = EntryTypeReceipt

	case isUtxoManagementTx(tx):
		utxoManagement = true

	// If we have a zero amount on chain transaction, we do not create an
	// entry for it. This may happen when the remote party claims a htlc on
	// our commitment. We do not want to report 0 value transactions that
	// are not relevant to us, so we just exit early.
	default:
		return nil, nil
	}

	// If this is a utxo management transaction, we return a fee entry only.
	if utxoManagement {
		note := utxoManagementFeeNote(tx.TxHash)
		feeEntry, err := createOnchainFeeEntry(tx, category, note, u)
		if err != nil {
			return nil, err
		}

		return []*HarmonyEntry{feeEntry}, nil
	}

	txEntry, err := newHarmonyEntry(
		tx.Timestamp, amtMsat, entryType, tx.TxHash, tx.TxHash,
		tx.Label, category, true, u.getFiat,
	)
	if err != nil {
		return nil, err
	}

	// If we did not pay any fees, we can just return a single entry.
	if tx.Fee == 0 {
		return []*HarmonyEntry{txEntry}, nil
	}

	feeEntry, err := createOnchainFeeEntry(tx, category, "", u)
	if err != nil {
		return nil, err
	}

	return []*HarmonyEntry{txEntry, feeEntry}, nil
}

// invoiceNote creates an optional note for an invoice if it had a memo, was
// overpaid, or both.
func invoiceNote(memo string, amt, amtPaid lnwire.MilliSatoshi,
	keysend bool) string {

	var notes []string

	if memo != "" {
		notes = append(notes, fmt.Sprintf("memo: %v", memo))
	}

	if amt != amtPaid {
		notes = append(notes, fmt.Sprintf("invoice overpaid "+
			"original amount: %v, paid: %v", amt, amtPaid))
	}

	if keysend {
		notes = append(notes, "keysend payment")
	}

	if len(notes) == 0 {
		return ""
	}

	return strings.Join(notes, "/")
}

// invoiceEntry creates an entry for an invoice.
func invoiceEntry(invoice lndclient.Invoice, circularReceipt bool,
	u entryUtils) (*HarmonyEntry, error) {

	category := getCategory(invoice.Memo, u.customCategories)

	eventType := EntryTypeReceipt
	if circularReceipt {
		eventType = EntryTypeCircularReceipt
	}

	note := invoiceNote(
		invoice.Memo, invoice.Amount, invoice.AmountPaid,
		invoice.IsKeysend,
	)

	return newHarmonyEntry(
		invoice.SettleDate, int64(invoice.AmountPaid), eventType,
		invoice.Hash.String(), invoice.Preimage.String(), note,
		category, false, u.getFiat,
	)
}

// paymentReference produces a unique reference for a payment. Since payment
// hash is not guaranteed to be unique, we use the payments unique sequence
// number and its hash.
func paymentReference(sequenceNumber uint64, preimage lntypes.Preimage) string {
	return fmt.Sprintf("%v:%v", sequenceNumber, preimage)
}

// paymentNote creates a note for payments from our node.
// nolint: interfacer
func paymentNote(dest *route.Vertex, memo *string) string {
	var notes []string

	if memo != nil && *memo != "" {
		notes = append(notes, fmt.Sprintf("memo: %v", *memo))
	}

	if dest != nil {
		notes = append(notes, fmt.Sprintf("destination: %v", dest))
	}

	return strings.Join(notes, "/")
}

// paymentEntry creates an entry for an off chain payment, including fee entries
// where required.
func paymentEntry(payment paymentInfo, paidToSelf bool,
	u entryUtils) ([]*HarmonyEntry, error) {

	// It is possible to make a payment to ourselves as part of a circular
	// rebalance which is operationally used to shift funds between
	// channels. For these payment types, we lose balance from fees, but do
	// not change our balance from the actual payment because it is paid
	// back to ourselves.
	var (
		paymentType = EntryTypePayment
		feeType     = EntryTypeFee
	)

	// If we made the payment to ourselves, we set special entry types,
	// since the payment amount did not actually affect our balance.
	if paidToSelf {
		paymentType = EntryTypeCircularPayment
		feeType = EntryTypeCircularPaymentFee
	}

	// Create a note for our payment. Since we have already checked that our
	// payment is settled, we will not have a nil preimage.
	note := paymentNote(payment.destination, payment.description)
	ref := paymentReference(payment.SequenceNumber, *payment.Preimage)

	// Payment values are expressed as positive values over rpc, but they
	// decrease our balance so we flip our value to a negative one.
	amt := invertMsat(int64(payment.Amount))

	paymentEntry, err := newHarmonyEntry(
		payment.settleTime, amt, paymentType, payment.Hash.String(),
		ref, note, "", false, u.getFiat,
	)
	if err != nil {
		return nil, err
	}

	// If we paid no fees (possible for payments to our direct peer), then
	// we just return the payment entry.
	if payment.Fee == 0 {
		return []*HarmonyEntry{paymentEntry}, nil
	}

	feeRef := FeeReference(ref)
	feeAmt := invertMsat(int64(payment.Fee))

	feeEntry, err := newHarmonyEntry(
		payment.settleTime, feeAmt, feeType, payment.Hash.String(),
		feeRef, note, "", false, u.getFiat,
	)
	if err != nil {
		return nil, err
	}
	return []*HarmonyEntry{paymentEntry, feeEntry}, nil
}

// forwardTxid provides a best effort txid using incoming and outgoing channel
// ID paired with timestamp in an effort to make txid unique per htlc forwarded.
// This is not used as a reference because we could theoretically have duplicate
// timestamps.
func forwardTxid(forward lndclient.ForwardingEvent) string {
	return fmt.Sprintf("%v:%v:%v", forward.Timestamp, forward.ChannelIn,
		forward.ChannelOut)
}

// forwardNote creates a note that indicates the amounts that were forwarded in
// and out of our node.
func forwardNote(amtIn, amtOut lnwire.MilliSatoshi) string {
	return fmt.Sprintf("incoming: %v outgoing: %v", amtIn, amtOut)
}

// forwardingEntry produces a forwarding entry with a zero amount which reflects
// shifting of funds in our channels, and fees entry which reflects the fees we
// earned form the forward.
func forwardingEntry(forward lndclient.ForwardingEvent,
	u entryUtils) ([]*HarmonyEntry, error) {

	txid := forwardTxid(forward)
	note := forwardNote(forward.AmountMsatIn, forward.AmountMsatOut)

	fwdEntry, err := newHarmonyEntry(
		forward.Timestamp, 0, EntryTypeForward, txid, "", note, "",
		false, u.getFiat,
	)
	if err != nil {
		return nil, err
	}

	// If we did not earn any fees, return the forwarding entry.
	if forward.FeeMsat == 0 {
		return []*HarmonyEntry{fwdEntry}, nil
	}

	feeEntry, err := newHarmonyEntry(
		forward.Timestamp, int64(forward.FeeMsat),
		EntryTypeForwardFee, txid, "", "", "", false, u.getFiat,
	)
	if err != nil {
		return nil, err
	}

	return []*HarmonyEntry{fwdEntry, feeEntry}, nil
}
