package script

import (
	"bytes"

	"github.com/btcsuite/btcd/wire"
)

const (
	// We expect a single input for our spend transaction, which is the
	// htlc transaction.
	spendInputLength = 1

	// We expect our spend transaction to have 3 elements in the witness:
	// 0: The signature for the sender key.
	// 1: The unlocking condition, which is just an empty byte.
	// 2: The script for the htlc itself.
	spendWitnessLength = 3
)

// timeoutUnlock is the unlocking element that we expect in our witness timeout
// tx.
var timeoutUnlock = []byte{0}

// SpendType indicates whether a transaction is an on chain htlc spend.
type SpendType int

const (
	// SpendTypeUnknown is set when our transaction is not a htlc success
	// or timeout.
	SpendTypeUnknown SpendType = iota

	// SpendTypeSuccess represents a success spend from a htlc with the
	// preimage.
	SpendTypeSuccess

	// SpendTypeTimeout represents a timeout spend from a htlc.
	SpendTypeTimeout
)

// MatchSpend attempts to identify a loop out timeout or success sweep. It does
// so by examining the input to a transaction. We expect the following to be
// true for timeout and success spends:
// - The transaction only has one input, because we do not yet have batching
// - The witness has three elements (signature, unlock condition, htlc script)
// - The final element in the witness is the original htlc script
//
// For timeouts, we expect the second element in the witness to unlock with the
// timeout (a 0 value). For success sweeps, we expect the unlocking element to
// be the preimage.
func MatchSpend(tx *wire.MsgTx, height int64) (SpendType, error) {
	if len(tx.TxIn) != spendInputLength {
		return SpendTypeUnknown, nil
	}

	witness := tx.TxIn[0].Witness
	if len(witness) != spendWitnessLength {
		return SpendTypeUnknown, nil
	}

	// Now that we know we have a transaction with a single input and three
	// elements in its witness, we check whether the witness has the timeout
	// or success unlocking element in the witness. We match the timeout
	// condition exactly, and check our preimage by length only (because we
	// do not know exact preimage values)
	var spendType SpendType
	switch {
	case bytes.Equal(witness[1], timeoutUnlock):
		spendType = SpendTypeTimeout

	case len(witness[1]) == preimageLength:
		spendType = SpendTypeSuccess

	// If we did not match the timeout or success unlocking conditions, this
	// transaction is not a success or timeout.
	default:
		return SpendTypeUnknown, nil
	}

	// Get a htlc matcher for our height hint.
	htlcScript, err := newHtlcMatcher(height)
	if err != nil {
		return SpendTypeUnknown, err
	}

	// If the final element in the the witness is a htlc script, then we
	// know that we have spent from an on chain htlc with the timeout path.
	isHtlc, err := matchScript(witness[2], htlcScript)
	if err != nil {
		return SpendTypeUnknown, err
	}

	if !isHtlc {
		return SpendTypeUnknown, nil
	}

	return spendType, nil
}
