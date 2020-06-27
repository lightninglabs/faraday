package resolutions

import (
	"testing"

	"github.com/btcsuite/btcd/chaincfg/chainhash"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

// TestTotalFees tests fee calculation for a transaction that we assume has a
// single output, and perhaps a change address.
func TestTotalFees(t *testing.T) {
	tests := []struct {
		name string
		txid *chainhash.Hash
		fee  decimal.Decimal
		err  error
	}{
		{
			name: "can calculate fees",
			txid: txid1,
			fee:  tx1TotalFee,
			err:  nil,
		},
		{
			name: "tx2",
			txid: txid2,
			fee:  tx2TotalFee,
			err:  nil,
		},
		{
			name: "too many outputs",
			txid: txid0,
			fee:  decimal.Zero,
			err:  errBatchedTx,
		},
	}

	for _, test := range tests {
		test := test

		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			fee, err := totalFees(getDetails, test.txid)
			require.Equal(t, test.err, err)
			require.Equal(t, test.fee, fee)
		})
	}
}
