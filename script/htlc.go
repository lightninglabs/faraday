package script

import (
	"errors"

	"github.com/btcsuite/btcd/txscript"
)

const (
	// preimageLength is the number of bytes our preimage takes up in the
	// htlc.
	preimageLength = 32

	// hashLength is the length of the hash we include in the htlc script.
	hashLength = 20

	// keyLength is the length of the sender/receiver keys we include in
	// the htlc script.
	keyLength = 33
)

var errInvalidHeightHint = errors.New("require height hint >= 0")

// newHtlcMatcher creates a matcher for a generic on chain htlc script. This
// matcher can be used to match htlc template on chain, without requiring htlc
// specific information like htlc hash and sender/receiver keys, which are just
// matched by length.
//
// All of the variable elements in our script except for cltv timeout are of
// a known length except the cltv timeout value will be encoded with differing
// lengths depending on our current height. On mainnet we will use 3 bytes for
// this value for the foreseeable future (until block 16777216, which should
// fall in ~ year 2302), but we will have lower heights in regtest, and higher
// heights in testnet. We add a height hint (which is the confirmation height
// of the transaction), to help us match this value on different networks. We do
// not need to worry about the edge case where a htlc confirms at a height that
// requires n bytes, but expires at a height that requires n+1 bytes, because we
// don't set our htlcs to expire in 300 years time.
//
// We match the following script:
// OP_SIZE 32 OP_EQUAL
// OP_IF
//    OP_HASH160 [20 byte hash] OP_EQUALVERIFY
//    [33 byte receiver key]
// OP_ELSE
//    OP_DROP
//    <cltv timeout> OP_CHECKLOCKTIMEVERIFY OP_DROP
//    [33 byte sender key]
// OP_ENDIF
// OP_CHECKSIG
func newHtlcMatcher(heightHint int64) ([]scriptMatcher, error) {
	if heightHint <= 0 {
		return nil, errInvalidHeightHint
	}

	var (
		htlcScript []scriptMatcher
		matcher    scriptMatcher
		err        error
	)

	matcher, err = newOpcodeMatcher(txscript.OP_SIZE)
	if err != nil {
		return nil, err
	}
	htlcScript = append(htlcScript, matcher)

	// We match our preimage length exactly, because we expect the exact
	// value in the script.
	matcher, err = newDataMatcher([]byte{preimageLength})
	if err != nil {
		return nil, err
	}
	htlcScript = append(htlcScript, matcher)

	matcher, err = newOpcodeMatcher(txscript.OP_EQUAL)
	if err != nil {
		return nil, err
	}
	htlcScript = append(htlcScript, matcher)

	matcher, err = newOpcodeMatcher(txscript.OP_IF)
	if err != nil {
		return nil, err
	}
	htlcScript = append(htlcScript, matcher)

	matcher, err = newOpcodeMatcher(txscript.OP_HASH160)
	if err != nil {
		return nil, err
	}
	htlcScript = append(htlcScript, matcher)

	// We do not want to match to a specific hash, so we just match our
	// expected 20 byte length.
	matcher, err = newLengthMatcher(hashLength)
	if err != nil {
		return nil, err
	}
	htlcScript = append(htlcScript, matcher)

	matcher, err = newOpcodeMatcher(txscript.OP_EQUALVERIFY)
	if err != nil {
		return nil, err
	}

	// We do not want to match a specific receiver key, so we just match
	// our expected 33 byte length.
	htlcScript = append(htlcScript, matcher)
	matcher, err = newLengthMatcher(keyLength)
	if err != nil {
		return nil, err
	}

	htlcScript = append(htlcScript, matcher)
	matcher, err = newOpcodeMatcher(txscript.OP_ELSE)
	if err != nil {
		return nil, err
	}

	htlcScript = append(htlcScript, matcher)
	matcher, err = newOpcodeMatcher(txscript.OP_DROP)
	if err != nil {
		return nil, err
	}
	htlcScript = append(htlcScript, matcher)

	// Our timeout height will vary with htlcs, so we just match the
	// expected length of this data field.
	matcher, err = newVariableIntMatcher(heightHint)
	if err != nil {
		return nil, err
	}
	htlcScript = append(htlcScript, matcher)

	matcher, err = newOpcodeMatcher(txscript.OP_CHECKLOCKTIMEVERIFY)
	if err != nil {
		return nil, err
	}
	htlcScript = append(htlcScript, matcher)

	matcher, err = newOpcodeMatcher(txscript.OP_DROP)
	if err != nil {
		return nil, err
	}
	htlcScript = append(htlcScript, matcher)

	// As with our receiver key, we don't want to match exact values for
	// sender key, so we match the expected 33 bytes.
	matcher, err = newLengthMatcher(keyLength)
	if err != nil {
		return nil, err
	}
	htlcScript = append(htlcScript, matcher)

	matcher, err = newOpcodeMatcher(txscript.OP_ENDIF)
	if err != nil {
		return nil, err
	}
	htlcScript = append(htlcScript, matcher)

	matcher, err = newOpcodeMatcher(txscript.OP_CHECKSIG)
	if err != nil {
		return nil, err
	}
	htlcScript = append(htlcScript, matcher)

	return htlcScript, nil
}
