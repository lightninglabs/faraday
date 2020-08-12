package fees

import (
	"fmt"
	"testing"

	"github.com/btcsuite/btcd/btcjson"
	"github.com/btcsuite/btcd/chaincfg/chainhash"
	"github.com/btcsuite/btcutil"
	"github.com/stretchr/testify/require"
)

var (
	hash0 = "286ddb794170fafb73450db66b911f65823567ca3f9b88adc1c67b769951d7c2"
	hash1 = "b3ee48d811b07dd4c6c089e49587ed45d313cf2333d3588468848c4d98e5940d"
	hash2 = "ffa0c0191f491ac5193c6626db13d78e44d8b841530058a9eeb89d8fcea26c0d"

	// Create three transaction ids, which we will setup in the sequence
	// txid2 --spends from--> txid1 --spends from--> txid0.
	txid0, _ = chainhash.NewHashFromStr(hash0)
	txid1, _ = chainhash.NewHashFromStr(hash1)
	txid2, _ = chainhash.NewHashFromStr(hash2)

	tx0VOutValue    = 3.1
	tx0OtherOutputs = 1.1

	// Create our first transaction, we do not need to set inputs here
	// because we just spend from this tx in tests. We give this transaction
	// three outputs so that it checks that we appropriately only use the
	// value of a single output in our calculation, and so that it can be
	// used to trigger error conditions (where we require >2 output).
	tx0 = &btcjson.TxRawResult{
		Hash: hash0,
		Vout: []btcjson.Vout{
			{
				Value: tx0VOutValue,
			},
			{
				Value: tx0OtherOutputs,
			},
			{
				Value: tx0OtherOutputs,
			},
		},
	}

	// Set the amount that our next tx will have as an output. The fee for
	// our first tx is therefore the original tx0VOutValue less this amount.
	tx1VoutValue      float64 = 3
	tx1TotalFeeSat, _         = btcutil.NewAmount(
		tx0VOutValue - tx1VoutValue,
	)

	// Create tx1 which spends all of the outputs from tx0, and will be
	// spent by tx2.
	tx1 = &btcjson.TxRawResult{
		Hash: hash1,
		Vin: []btcjson.Vin{
			{
				Txid: hash0,
				Vout: 0,
			},
		},
		Vout: []btcjson.Vout{
			{
				Value: tx1VoutValue,
			},
		},
	}

	// Set the output amounts that our final tx will create.
	output1Value float64 = 2
	output2Value         = 0.5

	// Our fee for our second transaction is therefore the output of tx1
	// less our two outputs.
	tx2TotalFeeSat, _ = btcutil.NewAmount(
		tx1VoutValue - output1Value - output2Value,
	)

	// tx2 is a transaction that spends from only tx1 (value =3) and creates
	// two new outpoints, with a total value of 2.5
	tx2 = &btcjson.TxRawResult{
		Hash: hash2,
		Vin: []btcjson.Vin{
			{
				Txid: hash1,
				Vout: 0,
			},
		},
		Vout: []btcjson.Vout{
			{
				Value: output1Value,
			},
			{
				Value: output2Value,
			},
		},
	}
)

// TestTotalFees tests fee calculation for a transaction that we assume has a
// single output, and perhaps a change address.
func TestTotalFees(t *testing.T) {
	tests := []struct {
		name string
		txid *chainhash.Hash
		fee  btcutil.Amount
		err  error
	}{
		{
			name: "can calculate fees",
			txid: txid1,
			fee:  tx1TotalFeeSat,
			err:  nil,
		},
		{
			name: "tx2",
			txid: txid2,
			fee:  tx2TotalFeeSat,
			err:  nil,
		},
	}

	for _, test := range tests {
		test := test

		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			fee, err := CalculateFee(getDetails, test.txid)
			require.Equal(t, test.err, err)
			require.Equal(t, test.fee, fee)
		})
	}
}

// getDetails mocks lookup for a node that has knowledge of tx1 and tx2.
func getDetails(txHash *chainhash.Hash) (*btcjson.TxRawResult, error) {
	switch *txHash {
	case *txid0:
		return tx0, nil

	case *txid1:
		return tx1, nil

	case *txid2:
		return tx2, nil

	default:
		return nil, fmt.Errorf("transaction not found")
	}
}
