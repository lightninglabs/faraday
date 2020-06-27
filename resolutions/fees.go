package resolutions

import (
	"errors"

	"github.com/btcsuite/btcd/btcjson"
	"github.com/btcsuite/btcd/chaincfg/chainhash"
	"github.com/btcsuite/btcutil"
	"github.com/shopspring/decimal"
)

// getTxDetails is a function which looks up transactions by hash.
type getTxDetails func(hash *chainhash.Hash) (*btcjson.TxRawResult, error)

// errBatchedTx is returned when we are trying to get the fees for a transaction
// but it is not of the format we expect (max 2 outputs, one change one output).
var errBatchedTx = errors.New("cannot calculate fees for batched " +
	"transaction")

// totalFees returns the total fees for the transaction provided. Note that this
// function assumes that we have a maximum of two outputs (one regular and one
// change address, or two outputs from a cooperative close) and does not
// calculate fees for batched transactions (where we cannot split fees between
// outputs).
func totalFees(details getTxDetails, txid *chainhash.Hash) (decimal.Decimal,
	error) {

	var fees decimal.Decimal

	tx, err := details(txid)
	if err != nil {
		return decimal.Zero, err
	}

	// Do a quick sanity check that our transaction does not have more than
	// two outputs.
	// TODO(carla): identify change address and split fees between outputs.
	if len(tx.Vout) > 2 {
		return decimal.Zero, errBatchedTx
	}

	// First, we minus total outputs from our fees.
	for _, out := range tx.Vout {
		amt, err := btcutil.NewAmount(out.Value)
		if err != nil {
			return decimal.Zero, err
		}

		fees = fees.Sub(decimal.NewFromInt(int64(amt)))
	}

	// Next, we lookup each of our inputs to figure out their values and
	// minus them from our fees
	for _, in := range tx.Vin {
		prevOutHash, err := chainhash.NewHashFromStr(in.Txid)
		if err != nil {
			return decimal.Zero, err
		}

		tx, err := details(prevOutHash)
		if err != nil {
			return decimal.Zero, err
		}

		prevOut := tx.Vout[in.Vout]
		amt, err := btcutil.NewAmount(prevOut.Value)
		if err != nil {
			return decimal.Zero, err
		}

		fees = fees.Add(decimal.NewFromInt(int64(amt)))
	}

	// Our fees are simply the difference between our input and output
	// total.
	return fees, nil
}
