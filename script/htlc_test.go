package script

import (
	"testing"

	"github.com/btcsuite/btcd/chaincfg"
	"github.com/lightninglabs/loop/swap"
	"github.com/lightninglabs/loop/test"
	"github.com/lightningnetwork/lnd/lntypes"
	"github.com/stretchr/testify/require"
)

var (
	senderKey, receiverKey [33]byte

	expiry int64 = 636251

	lowExpiry = 100

	hash = lntypes.Hash{
		1, 1, 1, 1, 1, 1, 1, 1,
		1, 1, 1, 1, 1, 1, 1, 1,
		1, 1, 1, 1, 1, 1, 1, 1,
		1, 1, 1, 1, 1, 1, 1, 1,
	}

	params = &chaincfg.TestNet3Params
)

func init() {
	_, receiverPubKey := test.CreateKey(1)
	copy(receiverKey[:], receiverPubKey.SerializeCompressed())

	_, senderPubKey := test.CreateKey(2)
	copy(senderKey[:], senderPubKey.SerializeCompressed())
}

// TestMatchHtlcScript tests identification of htlc scripts using generic
// matching.
func TestMatchHtlcScript(t *testing.T) {
	// Create two valid swaps, one nested and one native segwit. The output
	// type should not matter for script matching, but we include both for
	// completeness.
	validNP2WSH, err := swap.NewHtlc(
		int32(expiry), senderKey, receiverKey, hash, swap.HtlcNP2WSH,
		params,
	)
	require.NoError(t, err)

	validP2WSH, err := swap.NewHtlc(
		int32(expiry), senderKey, receiverKey, hash, swap.HtlcP2WSH,
		params,
	)
	require.NoError(t, err)

	// Create a htlc which has a low expiry value to test our height hint.
	smallExp, err := swap.NewHtlc(
		int32(lowExpiry), senderKey, receiverKey, hash, swap.HtlcP2WSH,
		params,
	)
	require.NoError(t, err)

	// Create a random script that is the same length as our htlc script,
	// and matches the first few elements but does not equal it.
	randomScript := [106]byte{
		130, 1, 32, 135, 99,
	}

	tests := []struct {
		name   string
		expiry int64
		script []byte
		ok     bool
	}{
		{
			name:   "nested segwit",
			expiry: expiry,
			script: validNP2WSH.Script,
			ok:     true,
		},
		{
			name:   "native segwit",
			expiry: expiry,
			script: validP2WSH.Script,
			ok:     true,
		},
		{
			name:   "low expiry value",
			expiry: int64(lowExpiry),
			script: smallExp.Script,
			ok:     true,
		},
		{
			name:   "script too long",
			expiry: expiry,
			script: append(validP2WSH.Script, 0),
			ok:     false,
		},
		{
			name:   "script too short",
			expiry: expiry,
			script: validP2WSH.Script[:len(validP2WSH.Script)-2],
			ok:     false,
		},
		{
			name:   "random script, same length",
			expiry: expiry,
			script: randomScript[:],
			ok:     false,
		},
	}

	for _, testCase := range tests {
		testCase := testCase

		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			// Get a htlc matcher which will match our script.
			matcher, err := newHtlcMatcher(testCase.expiry)
			require.NoError(t, err)

			ok, err := matchScript(testCase.script, matcher)
			require.NoError(t, err)
			require.Equal(t, testCase.ok, ok)
		})
	}
}
