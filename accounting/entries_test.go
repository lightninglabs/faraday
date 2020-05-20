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
