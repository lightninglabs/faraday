package script

import (
	"testing"

	"github.com/btcsuite/btcd/wire"
	"github.com/lightninglabs/loop/swap"
	"github.com/stretchr/testify/require"
)

var preimage = [preimageLength]byte{1, 1, 1}

// TestMatchTimeout tests detection of htlc timeout sweeps.
func TestMatchTimeout(t *testing.T) {
	validNP2WSH, err := swap.NewHtlc(
		int32(expiry), senderKey, receiverKey, hash, swap.HtlcNP2WSH,
		params,
	)
	require.NoError(t, err)

	tests := []struct {
		name      string
		txin      []*wire.TxIn
		spendType SpendType
	}{
		{
			name: "too many inputs",
			txin: []*wire.TxIn{
				{}, {},
			},
			spendType: SpendTypeUnknown,
		},
		{
			name: "incorrect witness length",
			txin: []*wire.TxIn{
				{
					Witness: [][]byte{
						{}, {},
					},
				},
			},
			spendType: SpendTypeUnknown,
		},
		{
			name: "not unlocked with timeout",
			txin: []*wire.TxIn{
				{
					Witness: [][]byte{
						{},
						{3},
						{},
					},
				},
			},
			spendType: SpendTypeUnknown,
		},
		{
			name: "unlocked with timeout, wrong program",
			txin: []*wire.TxIn{
				{
					Witness: [][]byte{
						{},
						timeoutUnlock,
						append(validNP2WSH.Script, 2),
					},
				},
			},
			spendType: SpendTypeUnknown,
		},
		{
			name: "unlocked with timeout, correct program",
			txin: []*wire.TxIn{
				{
					Witness: [][]byte{
						{},
						timeoutUnlock,
						validNP2WSH.Script,
					},
				},
			},
			spendType: SpendTypeTimeout,
		},
		{
			name: "unlocked with preimage, correct program",
			txin: []*wire.TxIn{
				{
					Witness: [][]byte{
						{},
						preimage[:],
						validNP2WSH.Script,
					},
				},
			},
			spendType: SpendTypeSuccess,
		},
	}

	for _, testCase := range tests {
		testCase := testCase

		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			tx := &wire.MsgTx{
				TxIn: testCase.txin,
			}

			spendType, err := MatchSpend(tx, expiry)
			require.NoError(t, err)
			require.Equal(t, testCase.spendType, spendType)
		})
	}
}
