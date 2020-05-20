package accounting

import (
	"encoding/hex"
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

	onChainTxID            = "e75760156b04234535e6170f152697de28b73917c69dda53c60baabdae571457"
	onChainAmtSat    int64 = 10000
	onChainFeeSat    int64 = 1000
	onChainTimestamp int64 = 1588160816
	destAddr               = "bcrt1qz9rufsy0txtljfhk298y946wyy8yq7jzne6xku"

	onChainTx = lnrpc.Transaction{
		TxHash:        onChainTxID,
		Amount:        onChainAmtSat,
		TimeStamp:     onChainTimestamp,
		TotalFees:     onChainFeeSat,
		DestAddresses: []string{destAddr},
	}

	paymentRequest = "lnbcrt10n1p0t6nmypp547evsfyrakg0nmyw59ud9cegkt99yccn5nnp4suq3ac4qyzzgevsdqqcqzpgsp54hvffpajcyddm20k3ptu53930425hpnv8m06nh5jrd6qhq53anrq9qy9qsqphhzyenspf7kfwvm3wyu04fa8cjkmvndyexlnrmh52huwa4tntppjmak703gfln76rvswmsx2cz3utsypzfx40dltesy8nj64ttgemgqtwfnj9"

	invoiceMemo = "memo"

	invoiceAmt = lnwire.MilliSatoshi(300)

	invoiceOverpaidAmt = lnwire.MilliSatoshi(400)

	invoiceSettleTime int64 = 1588159722

	invoicePreimage = "b5f0c5ac0c873a05702d0aa63a518ecdb8f3ba786be2c4f64a5b10581da976ae"
	preimage, _     = hex.DecodeString(invoicePreimage)

	invoiceHash = "afb2c82483ed90f9ec8ea178d2e328b2ca526313a4e61ac3808f715010424659"
	hash, _     = hex.DecodeString(invoiceHash)

	invoice = &lnrpc.Invoice{
		Memo:           invoiceMemo,
		RPreimage:      preimage,
		RHash:          hash,
		ValueMsat:      int64(invoiceAmt),
		CreationDate:   0,
		SettleDate:     invoiceSettleTime,
		PaymentRequest: paymentRequest,
		AmtPaidSat:     0,
		AmtPaidMsat:    int64(invoiceOverpaidAmt),
		Htlcs:          nil,
		IsKeysend:      true,
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

// TestOnChainEntry tests creation of entries for receipts and payments, and the
// generation of a fee entry where applicable.
func TestOnChainEntry(t *testing.T) {
	getOnChainEntry := func(isReceive bool, label string) *HarmonyEntry {
		entryType := EntryTypePayment
		if isReceive {
			entryType = EntryTypeReceipt
		}

		amt := satsToMsat(onChainAmtSat)
		fiat, _ := mockConvert(amt, 0)

		return &HarmonyEntry{
			Timestamp: time.Unix(onChainTimestamp, 0),
			Amount:    lnwire.MilliSatoshi(amt),
			FiatValue: fiat,
			TxID:      onChainTxID,
			Reference: onChainTxID,
			Note:      label,
			Type:      entryType,
			OnChain:   true,
			Credit:    isReceive,
		}
	}

	feeAmt := satsToMsat(onChainFeeSat)
	fiat, _ := mockConvert(feeAmt, 0)

	// Fee entry is the fee entry we expect for this transaction.
	feeEntry := &HarmonyEntry{
		Timestamp: time.Unix(onChainTimestamp, 0),
		Amount:    lnwire.MilliSatoshi(feeAmt),
		FiatValue: fiat,
		TxID:      onChainTxID,
		Reference: feeReference(onChainTxID),
		Note:      "",
		Type:      EntryTypeFee,
		OnChain:   true,
		Credit:    false,
	}

	tests := []struct {
		name string

		// Whether the transaction paid us, or was our payment.
		isReceive bool

		// Whether the transaction has a fee attached.
		hasFee bool

		// txLabel is an optional label on the rpc transaction.
		txLabel string
	}{
		{
			name:      "receive with fee",
			isReceive: true,
			hasFee:    true,
		},
		{
			name:      "receive without fee",
			isReceive: true,
			hasFee:    false,
		},
		{
			name:      "payment without fee",
			isReceive: true,
			hasFee:    false,
		},
		{
			name:      "payment with fee",
			isReceive: true,
			hasFee:    true,
		},
	}

	for _, test := range tests {
		test := test

		t.Run(test.name, func(t *testing.T) {
			// Make a copy so that we can change fields.
			chainTx := onChainTx

			// If we are testing a payment, the amount should be
			// negative.
			if !test.isReceive {
				chainTx.Amount *= -1
			}

			// If we should not have a fee present, remove it,
			// also add the fee entry to our expected set of
			// entries.
			if !test.hasFee {
				chainTx.TotalFees = 0
			}

			// Set the label as per the test.
			chainTx.Label = test.txLabel

			entries, err := onChainEntries(&chainTx, mockConvert)
			require.NoError(t, err)

			// We expect the have an single on chain entry at least.
			onChainEntry := getOnChainEntry(
				test.isReceive, test.txLabel,
			)
			expected := []*HarmonyEntry{onChainEntry}

			if test.hasFee {
				expected = append(expected, feeEntry)
			}

			require.Equal(t, expected, entries)
		})
	}
}

// TestInvoiceEntry tests creation of entries for regular invoices and circular
// receipts.
func TestInvoiceEntry(t *testing.T) {
	getEntry := func(circular bool) *HarmonyEntry {
		note := invoiceNote(
			invoice.Memo, invoice.ValueMsat, invoice.AmtPaidMsat,
			invoice.IsKeysend,
		)

		fiat, _ := mockconvert(int64(invoiceOverpaidAmt), 0)

		expectedEntry := &HarmonyEntry{
			Timestamp: time.Unix(invoiceSettleTime, 0),
			Amount:    invoiceOverpaidAmt,
			FiatValue: fiat,
			TxID:      invoiceHash,
			Reference: invoicePreimage,
			Note:      note,
			Type:      EntryTypeReceipt,
			OnChain:   false,
			Credit:    true,
		}

		if circular {
			expectedEntry.Type = EntryTypeCircularReceipt
		}

		return expectedEntry
	}
	tests := []struct {
		name     string
		circular bool
	}{
		{
			name: "test",
		},
	}

	for _, test := range tests {
		test := test

		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			entry, err := invoiceEntry(
				invoice, test.circular, mockconvert,
			)
			if err != nil {
				t.Fatal(err)
			}

			expectedEntry := getEntry(test.circular)

			require.Equal(t, expectedEntry, entry)
		})
	}
}

func TestPaymentEntry(t *testing.T) {
	t.Fatal("TODO(carla): payment test")
}
