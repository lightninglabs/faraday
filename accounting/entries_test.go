package accounting

import (
	"fmt"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/lightningnetwork/lnd/lnrpc"
	"github.com/lightningnetwork/lnd/lnwire"
	"github.com/stretchr/testify/require"
)

var (
	openChannelTx              = "44183bc482d5b7be031739ce39b6c91562edd882ba5a9e3647341262328a2228"
	remotePubkey               = "02f6a7664ca2a2178b422a058af651075de2e5bdfff028ac8e1fcd96153cba636b"
	channelID           uint64 = 124244814004224
	channelCapacitySats int64  = 500000
	channelFeesSats     int64  = 10000
	destAddresses              = []string{
		"bcrt1qmwfx3a3y28dlhap9uh0kx0uc7qwwkhc06ajw6n",
		"bcrt1qkhdqm0g4f73splxrluwegvus7rv9ve88x45ejm",
	}

	openChannel = lnrpc.Channel{
		RemotePubkey: remotePubkey,
		ChannelPoint: fmt.Sprintf("%v:%v", openChannelTx, 1),
		ChanId:       channelID,
		Capacity:     channelCapacitySats,
		Initiator:    false,
	}

	transactionTimestamp int64 = 1588145604

	openChannelTransaction = lnrpc.Transaction{
		TxHash: openChannelTx,
		// Amounts are reported with negative values in getTransactions.
		Amount:           channelCapacitySats * -1,
		NumConfirmations: 2,
		TotalFees:        channelFeesSats,
		DestAddresses:    destAddresses,
		TimeStamp:        transactionTimestamp,
	}

	mockPrice = decimal.NewFromInt(10)

	closeTx = "e730b07d6121b19dd717925de82b8c76dec38517ffd85701e6735a726f5f75c3"

	closeBalanceSat int64 = 50000

	channelClose = lnrpc.ChannelCloseSummary{
		ChannelPoint:   openChannel.ChannelPoint,
		ChanId:         openChannel.ChanId,
		ClosingTxHash:  closeTx,
		RemotePubkey:   remotePubkey,
		SettledBalance: closeBalanceSat,
		CloseInitiator: lnrpc.Initiator_INITIATOR_REMOTE,
	}

	closeTimestamp int64 = 1588159722

	closeDestAddrs = []string{
		"bcrt1qmj4f92gcf08j3640csv72xvwlca33ypv4yw2nc",
		"bcrt1q0yv9p2ap55wsy95xgwrgrkmnt9jna03w06524c",
	}

	channelCloseTx = lnrpc.Transaction{
		TxHash:    closeTx,
		Amount:    closeBalanceSat,
		TimeStamp: closeTimestamp,
		// Total fees for closes will always reflect as 0 because they
		// come from the 2-2 multisig funding output.
		TotalFees:     0,
		DestAddresses: closeDestAddrs,
	}
)

// mockConvert is a mocked price function which returns mockPrice * amount.
func mockConvert(amt, _ int64) (decimal.Decimal, error) {
	amtDecimal := decimal.NewFromInt(amt)
	return mockPrice.Mul(amtDecimal), nil
}

// TestChannelOpenEntry tests creation of entries for locally and remotely
// initiated channels. It uses the globally declared open channel and tx to
// as input.
func TestChannelOpenEntry(t *testing.T) {
	// Returns a channel entry for the constant channel.
	getChannelEntry := func(initiator bool) *HarmonyEntry {
		var (
			amt       = satsToMsat(channelCapacitySats)
			credit    = false
			entryType = EntryTypeLocalChannelOpen
		)

		if !initiator {
			amt = 0
			credit = true
			entryType = EntryTypeRemoteChannelOpen
		}

		mockFiat, _ := mockConvert(amt, 0)

		note := channelOpenNote(
			initiator, remotePubkey, channelCapacitySats,
		)

		return &HarmonyEntry{
			Timestamp: time.Unix(transactionTimestamp, 0),
			Amount:    lnwire.MilliSatoshi(amt),
			FiatValue: mockFiat,
			TxID:      openChannelTx,
			Reference: fmt.Sprintf("%v", channelID),
			Note:      note,
			Type:      entryType,
			OnChain:   true,
			Credit:    credit,
		}

	}

	feeAmt := satsToMsat(channelFeesSats)
	mockFee, _ := mockConvert(feeAmt, 0)
	// Fee entry is the expected fee entry for locally initiated channels.
	feeEntry := &HarmonyEntry{
		Timestamp: time.Unix(transactionTimestamp, 0),
		Amount:    lnwire.MilliSatoshi(feeAmt),
		FiatValue: mockFee,
		TxID:      openChannelTx,
		Reference: feeReference(openChannelTx),
		Note:      channelOpenFeeNote(channelID),
		Type:      EntryTypeChannelOpenFee,
		OnChain:   true,
		Credit:    false,
	}

	tests := []struct {
		name string

		// initiator is used to set the initiator bool on the open
		// channel struct we use.
		initiator bool

		expectedErr error
	}{
		{
			name:      "remote initiator",
			initiator: false,
		},
		{
			name:      "local initiator",
			initiator: true,
		},
	}

	for _, test := range tests {
		test := test

		t.Run(test.name, func(t *testing.T) {
			// Make local copies of the global vars so that we can
			// safely change fields.
			channel := openChannel
			tx := openChannelTransaction

			// Set the initiator field according to test
			// requirement.
			channel.Initiator = test.initiator

			// Get our entries.
			entries, err := channelOpenEntries(
				&channel, &tx, mockConvert,
			)
			require.Equal(t, test.expectedErr, err)

			// At a minimum, we expect a channel entry to be present.
			expectedChanEntry := getChannelEntry(test.initiator)

			// If we opened the chanel, we also expect a fee entry
			// to be present.
			expected := []*HarmonyEntry{expectedChanEntry}
			if test.initiator {
				expected = append(expected, feeEntry)
			}

			require.Equal(t, expected, entries)
		})
	}
}

// TestChannelCloseEntry tests creation of channel close entries.
func TestChannelCloseEntry(t *testing.T) {
	// getCloseEntry returns a close entry for the global close var with
	// correct close type and amount.
	getCloseEntry := func(closeType string,
		closeBalance int64) *HarmonyEntry {

		note := channelCloseNote(
			channelID, closeType,
			lnrpc.Initiator_INITIATOR_REMOTE.String(),
		)

		closeAmt := satsToMsat(closeBalance)
		closeFiat, _ := mockConvert(closeAmt, 0)

		return &HarmonyEntry{
			Timestamp: time.Unix(closeTimestamp, 0),
			Amount:    lnwire.MilliSatoshi(closeAmt),
			FiatValue: closeFiat,
			TxID:      closeTx,
			Reference: closeTx,
			Note:      note,
			Type:      EntryTypeChannelClose,
			OnChain:   true,
			Credit:    true,
		}
	}

	tests := []struct {
		name      string
		closeType lnrpc.ChannelCloseSummary_ClosureType
		closeAmt  int64
	}{
		{
			name:      "coop close, has balance",
			closeType: lnrpc.ChannelCloseSummary_COOPERATIVE_CLOSE,
			closeAmt:  closeBalanceSat,
		},
		{
			name:      "force close, has no balance",
			closeType: lnrpc.ChannelCloseSummary_LOCAL_FORCE_CLOSE,
			closeAmt:  0,
		},
	}

	for _, test := range tests {
		test := test

		t.Run(test.name, func(t *testing.T) {
			// Make copies of the global vars so we can change some
			// fields.
			closeChan := channelClose
			closeTx := channelCloseTx

			closeChan.CloseType = test.closeType
			closeTx.Amount = test.closeAmt

			entries, err := closedChannelEntries(
				&closeChan, &closeTx, mockConvert,
			)
			require.NoError(t, err)

			expected := []*HarmonyEntry{getCloseEntry(
				test.closeType.String(), test.closeAmt,
			)}

			require.Equal(t, expected, entries)
		})
	}
}
