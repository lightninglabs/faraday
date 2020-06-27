package resolutions

import (
	"fmt"
	"testing"

	"github.com/btcsuite/btcd/btcjson"
	"github.com/btcsuite/btcd/chaincfg/chainhash"
	"github.com/btcsuite/btcd/wire"
	"github.com/btcsuite/btcutil"
	"github.com/lightninglabs/lndclient"
	"github.com/shopspring/decimal"
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
	tx1TotalFee = decimal.NewFromInt(int64(tx1TotalFeeSat))

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
	tx2TotalFee = decimal.NewFromInt(int64(tx2TotalFeeSat))

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

	// Finally, we create channel point
	tx1ChanPoint = &wire.OutPoint{
		Hash:  *txid1,
		Index: 0,
	}

	tx2ChanPoint = &wire.OutPoint{
		Hash:  *txid2,
		Index: 0,
	}
)

// TestGetClosedReport tests creation of a a closed channel report.
func TestGetClosedReport(t *testing.T) {
	tests := []struct {
		name           string
		chanPoint      string
		closedChannels []lndclient.ClosedChannel
		error          error
	}{
		{
			name:      "channel found, wrong type",
			chanPoint: tx2ChanPoint.String(),
			closedChannels: []lndclient.ClosedChannel{
				{
					ChannelPoint: tx1ChanPoint.String(),
				},
				{
					ChannelPoint: tx2ChanPoint.String(),
					CloseType:    lndclient.CloseTypeAbandoned,
				},
			},
			error: ErrCloseTypeNotSupported,
		},
		{
			name:      "channel not found",
			chanPoint: tx1ChanPoint.String(),
			closedChannels: []lndclient.ClosedChannel{
				{ChannelPoint: tx2ChanPoint.String()},
			},
			error: ErrChannelNotClosed,
		},
	}

	for _, test := range tests {
		test := test

		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			closedChannels := func() ([]lndclient.ClosedChannel,
				error) {

				return test.closedChannels, nil
			}

			_, err := ChannelCloseReport(
				&Config{ClosedChannels: closedChannels},
				test.chanPoint,
			)
			require.Equal(t, test.error, err)
		})
	}
}

// walletTransactions is a helper function which provides a WalletTransactions
// function that will return the txids provided.
func walletTransactions(txids []*chainhash.Hash) func() (
	[]lndclient.Transaction, error) {

	txns := make([]lndclient.Transaction, len(txids))
	for i, tx := range txids {
		txns[i] = lndclient.Transaction{
			TxHash: tx.String(),
		}
	}

	return func() ([]lndclient.Transaction, error) {
		return txns, nil
	}
}

// TestCoopCloseReport tests creation of a cooperative close report.
func TestCoopCloseReport(t *testing.T) {
	tests := []struct {
		name          string
		openInitiator lndclient.Initiator
		initiator     bool
		chanPoint     *wire.OutPoint
		openFee       decimal.Decimal
		closeFee      decimal.Decimal
		// walletTxns is a list of transactions that belong to our
		// wallet, our channel point should be in this list if it is
		// expected to be locally initiated.
		walletTxns []*chainhash.Hash
		error      error
	}{
		{
			name:          "remote opened",
			openInitiator: lndclient.InitiatorRemote,
			chanPoint:     tx1ChanPoint,
			initiator:     false,
			openFee:       decimal.Zero,
			closeFee:      decimal.Zero,
			error:         nil,
		},
		{
			name:          "local initiator",
			openInitiator: lndclient.InitiatorLocal,
			chanPoint:     tx1ChanPoint,
			initiator:     true,
			openFee:       tx1TotalFee,
			closeFee:      tx2TotalFee,
			error:         nil,
		},
		{
			// Refer to tx0 (which has 3 outputs) as our open tx,
			// this indicates that our close tx was batched, which
			// we do not currently support.
			name:          "local initiator, batched open tx",
			openInitiator: lndclient.InitiatorLocal,
			chanPoint:     wire.NewOutPoint(txid0, 0),
			initiator:     true,
			openFee:       decimal.Zero,
			closeFee:      decimal.Zero,
			error:         errBatchedTx,
		},
		{
			name:          "unknown initiator - lookup is remote",
			openInitiator: lndclient.InitiatorUnrecorded,
			chanPoint:     tx1ChanPoint,
			initiator:     false,
			openFee:       decimal.Zero,
			closeFee:      decimal.Zero,
			walletTxns:    nil,
			error:         nil,
		},
		{
			name:          "unknown initiator - lookup is local",
			openInitiator: lndclient.InitiatorUnrecorded,
			chanPoint:     tx1ChanPoint,
			initiator:     true,
			openFee:       tx1TotalFee,
			closeFee:      tx2TotalFee,
			walletTxns:    []*chainhash.Hash{txid1},
			error:         nil,
		},
	}

	for _, test := range tests {
		test := test

		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			// Create a coop-close with out outpoint and close
			// initiator of choice.
			chanClose := lndclient.ClosedChannel{
				ChannelPoint:  test.chanPoint.String(),
				ClosingTxHash: hash2,
				CloseType:     lndclient.CloseTypeCooperative,
				OpenInitiator: test.openInitiator,
			}

			cfg := &Config{
				WalletTransactions: walletTransactions(
					test.walletTxns,
				),
				GetTxDetail: getDetails,
			}

			report, err := coopCloseReport(cfg, &chanClose)
			require.Equal(t, test.error, err)

			// If we expect an error, we do not proceed to check our
			// report against our expected output.
			if err != nil {
				require.Nil(t, report)
				return
			}

			expected := &CloseReport{
				ChannelPoint: &wire.OutPoint{
					Hash:  *txid1,
					Index: 0,
				},
				ChannelInitiator: test.initiator,
				CloseType:        lndclient.CloseTypeCooperative,
				CloseTxid:        hash2,
				OpenFee:          test.openFee,
				CloseFee:         test.closeFee,
			}

			require.Equal(t, expected, report)
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
