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
	hash1 = "b3ee48d811b07dd4c6c089e49587ed45d313cf2333d3588468848c4d98e5940d"
	hash2 = "ffa0c0191f491ac5193c6626db13d78e44d8b841530058a9eeb89d8fcea26c0d"

	// Create transaction ids for our hashes.
	txid1, _ = chainhash.NewHashFromStr(hash1)
	txid2, _ = chainhash.NewHashFromStr(hash2)

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
		tx             *btcjson.TxRawResult
		closedChannels []lndclient.ClosedChannel
		error          error
	}{
		{
			name:      "channel found, wrong type",
			chanPoint: tx2ChanPoint.String(),
			tx:        &btcjson.TxRawResult{},
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
			tx:        &btcjson.TxRawResult{},
			closedChannels: []lndclient.ClosedChannel{
				{ChannelPoint: tx2ChanPoint.String()},
			},
			error: ErrChannelNotClosed,
		},
		{
			name: "batched channel open",
			tx: &btcjson.TxRawResult{
				Vout: []btcjson.Vout{{}, {}, {}},
			},
			chanPoint: tx1ChanPoint.String(),
			closedChannels: []lndclient.ClosedChannel{
				{ChannelPoint: tx1ChanPoint.String()},
			},
			error: errBatchedTx,
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

			getTx := func(txHash *chainhash.Hash) (
				*btcjson.TxRawResult, error) {

				return test.tx, nil
			}

			_, err := ChannelCloseReport(
				&Config{
					ClosedChannels: closedChannels,
					GetTxDetail:    getTx,
				},
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
	var (
		openFee  = btcutil.Amount(1)
		closeFee = btcutil.Amount(2)
	)

	tests := []struct {
		name          string
		openInitiator lndclient.Initiator
		initiator     bool
		chanPoint     *wire.OutPoint
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
			error:         nil,
		},
		{
			name:          "local initiator",
			openInitiator: lndclient.InitiatorLocal,
			chanPoint:     tx1ChanPoint,
			initiator:     true,
			error:         nil,
		},
		{
			name:          "unknown initiator - lookup is remote",
			openInitiator: lndclient.InitiatorUnrecorded,
			chanPoint:     tx1ChanPoint,
			initiator:     false,
			walletTxns:    nil,
			error:         nil,
		},
		{
			name:          "unknown initiator - lookup is local",
			openInitiator: lndclient.InitiatorUnrecorded,
			chanPoint:     tx1ChanPoint,
			initiator:     true,
			walletTxns:    []*chainhash.Hash{txid1},
			error:         nil,
		},
	}

	for _, test := range tests {
		test := test

		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			// Create a coop-close with out outpoint and close
			// initiator of choice, using hash 2 (txid 2) for our
			// close tx.
			chanClose := lndclient.ClosedChannel{
				ChannelPoint:  test.chanPoint.String(),
				ClosingTxHash: hash2,
				CloseType:     lndclient.CloseTypeCooperative,
				OpenInitiator: test.openInitiator,
			}

			// Set a fees function that returns different amounts
			// based on whether the tx being looked up is our open
			// or close tx.
			calculateFees := func(hash *chainhash.Hash) (
				btcutil.Amount, error) {

				switch {
				case hash.IsEqual(&test.chanPoint.Hash):
					return openFee, nil

				case hash.IsEqual(txid2):
					return closeFee, nil

				default:
					return 0, fmt.Errorf("hash: "+
						"%v unknown", hash)
				}
			}

			cfg := &Config{
				WalletTransactions: walletTransactions(
					test.walletTxns,
				),
				CalculateFees: calculateFees,
			}

			report, err := coopCloseReport(
				cfg, test.chanPoint, &chanClose,
			)
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
				OpenFee:          decimal.Zero,
				CloseFee:         decimal.Zero,
			}

			// If we initiated the channel, we expect our fee values
			// to be set.
			if test.initiator {
				expected.OpenFee = decimal.NewFromInt(int64(openFee))
				expected.CloseFee = decimal.NewFromInt(int64(closeFee))
			}

			require.Equal(t, expected, report)
		})
	}
}
