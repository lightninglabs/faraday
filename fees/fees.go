package fees

import (
	"github.com/btcsuite/btcd/btcjson"
	"github.com/btcsuite/btcd/chaincfg/chainhash"
	"github.com/btcsuite/btcutil"
)

// GetDetailsFunc is a function which looks up transactions by hash, returning
// its inputs and outputs.
type GetDetailsFunc func(hash *chainhash.Hash) ([]btcjson.Vin,
	[]btcjson.Vout, error)

// CalculateFee returns the total fees for the transaction provided.
func CalculateFee(details GetDetailsFunc, txid *chainhash.Hash) (btcutil.Amount,
	error) {

	var fees btcutil.Amount

	inputs, outputs, err := details(txid)
	if err != nil {
		return 0, err
	}

	// Lookup each of our inputs and add their value to our fees.
	for _, in := range inputs {
		prevOutHash, err := chainhash.NewHashFromStr(in.Txid)
		if err != nil {
			return 0, err
		}

		_, prevOuts, err := details(prevOutHash)
		if err != nil {
			return 0, err
		}

		prevOut := prevOuts[in.Vout]
		amt, err := btcutil.NewAmount(prevOut.Value)
		if err != nil {
			return 0, err
		}

		fees += amt
	}

	// Next, we minus total outputs from our fees.
	for _, out := range outputs {
		amt, err := btcutil.NewAmount(out.Value)
		if err != nil {
			return 0, err
		}

		fees -= amt
	}

	// Our fees are simply the difference between our input and output
	// total.
	return fees, nil
}
