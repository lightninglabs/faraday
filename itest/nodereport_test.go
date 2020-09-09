package itest

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/btcsuite/btcd/chaincfg/chainhash"
	"github.com/btcsuite/btcutil"
	"github.com/lightninglabs/lndclient"
	"github.com/lightningnetwork/lnd/lnrpc"
	"github.com/lightningnetwork/lnd/lnrpc/invoicesrpc"
	"github.com/lightningnetwork/lnd/lntypes"
	"github.com/lightningnetwork/lnd/lnwire"
	"github.com/stretchr/testify/require"

	"github.com/lightninglabs/faraday/accounting"
	"github.com/lightninglabs/faraday/fees"
	"github.com/lightninglabs/faraday/frdrpc"
)

var (
	paymentTimeout = time.Second * 10
)

// TestNodeReport tests our node report rpc endpoint. It includes coverage for
// off chain sends and receives, channel opens and closes and on chain receives.
// This test does not include on chain sends, forwards and circular payments.
func TestNodeReport(t *testing.T) {
	c := newTestContext(t)
	defer c.stop()

	ctx := context.Background()

	expected := make(map[string]expectedReport)

	// Our node has received a single coinbase output on chain to fund it
	// for testing. Excluding this transaction from our test is challenging,
	// because our test, lnd and bitcoind may all have different timestamps.
	// instead, we look this transaction up and add an expected receive to
	// our set of reports
	onChain, err := c.aliceClient.Client.ListTransactions(ctx, 0, 0)
	require.NoError(c.t, err)
	require.Len(c.t, onChain, 1, "on chain receive missing")

	expected[onChain[0].TxHash] = expectedReport{
		eventType: frdrpc.EntryType_RECEIPT,
		amount:    lnwire.MilliSatoshi(onChain[0].Amount * 1000),
		onChain:   true,
	}

	// We want to produce all the different types of transactions that our
	// node report creates. We will start with channel opening, initiating
	// a channel from alice to bob.
	var aliceChannelAmt = btcutil.Amount(50000)

	err = c.aliceClient.Client.Connect(
		ctx, c.bobPubkey, "localhost:10012",
	)
	require.NoError(c.t, err, "could not connect nodes")

	aliceChannel, aliceFee := c.openChannel(
		c.aliceClient.Client, c.bobPubkey, aliceChannelAmt,
	)

	// We need our channel ID (because it is used as a unique reference
	// for our report), so we look it up once we know both nodes are aware
	// of it.
	channels, err := c.aliceClient.Client.ListChannels(ctx)
	require.NoError(c.t, err, "alice list channels failed")

	aliceChan := findChannel(channels, aliceChannel)
	require.NotNil(c.t, aliceChan, "alice channel not found")

	// Add our channel open and fee entries to our set of expected entries.
	channelRef := lnwire.NewShortChanIDFromInt(aliceChan.ChannelID)
	expected[channelRef.String()] = expectedReport{
		eventType: frdrpc.EntryType_LOCAL_CHANNEL_OPEN,
		amount:    lnwire.MilliSatoshi(aliceChannelAmt * 1000),
		onChain:   true,
	}

	feeRef := accounting.FeeReference(aliceChannel.Hash.String())
	expected[feeRef] = expectedReport{
		eventType: frdrpc.EntryType_CHANNEL_OPEN_FEE,
		amount:    lnwire.MilliSatoshi(aliceFee * 1000),
		onChain:   true,
	}

	var (
		// Send 20k sats to bob, we make this amount large so that it
		// will cover his incoming reserve, and we will be able to
		// receive a payment back from him.
		paymentAmount lnwire.MilliSatoshi = 20000000

		// Receive 1 sat from bob, allowing us plenty of remaining
		// channel reserve.
		invoiceAmount lnwire.MilliSatoshi = 1000
	)

	// Make a payment from alice to bob, we need to make this payment first
	// because we do not have any incoming liquidity.
	hash, payreq := c.addInvoice(c.bobClient.Client, paymentAmount)
	aliceBobPreimage := c.makePayment(
		c.aliceClient.LndServices, c.bobClient.LndServices,
		lndclient.SendPaymentRequest{
			Invoice:     payreq,
			PaymentHash: &hash,
			Timeout:     paymentTimeout,
		}, lnrpc.Payment_SUCCEEDED,
	)

	// Add an entry for our payment to our set of expected entries. We do
	// not expect a fee entry because we made a single hop payment. Since
	// this is the first payment we send, we expect it to have a sequence
	// number of 1 in its payment reference.
	// TODO(carla): expose sequence number on lndclient and remove fixed val
	paymentRef := fmt.Sprintf("1:%v", aliceBobPreimage)
	expected[paymentRef] = expectedReport{
		eventType: frdrpc.EntryType_PAYMENT,
		amount:    paymentAmount,
		onChain:   false,
	}

	// Make a payment from bob to alice, we should have enough incoming
	// liquidity because we just made an outgoing payment. Here we don't
	// care about the fee because bob paid it.
	hash, payreq = c.addInvoice(c.aliceClient.Client, invoiceAmount)
	bobAlicePreimage := c.makePayment(
		c.bobClient.LndServices, c.aliceClient.LndServices,
		lndclient.SendPaymentRequest{
			Invoice:     payreq,
			PaymentHash: &hash,
			Timeout:     paymentTimeout,
		}, lnrpc.Payment_SUCCEEDED,
	)

	expected[bobAlicePreimage.String()] = expectedReport{
		eventType: frdrpc.EntryType_RECEIPT,
		amount:    invoiceAmount,
		onChain:   false,
	}

	// Now, we add a hodl invoice to bob's client which we will use to test
	// our on chain resolution of htlcs.
	hodlHash := lntypes.Hash{1, 2}
	hodlInv, err := c.bobClient.Invoices.AddHoldInvoice(
		ctx, &invoicesrpc.AddInvoiceData{
			Hash:  &hodlHash,
			Value: 10000,
		},
	)
	require.NoError(c.t, err, "could not add hodl invoice")

	// Make the payment from alice to bob, and wait for it to be in flight.
	// We do not accept the payment on bob's side, so this htlc is pending.
	c.makePayment(
		c.aliceClient.LndServices, c.bobClient.LndServices,
		lndclient.SendPaymentRequest{
			Invoice:     hodlInv,
			PaymentHash: &hodlHash,
			Timeout:     paymentTimeout,
		}, lnrpc.Payment_IN_FLIGHT,
	)

	// We are now going to force close the channel from alice's side. This
	// will create a force close which needs to be swept. We expect to have
	// and entry for our closed channel, which has a zero amount because
	// our outputs are encumbered behind timelocks.
	closeTx, fee := c.closeChannel(c.aliceClient.Client, aliceChannel, true)
	expected[closeTx.String()] = expectedReport{
		eventType: frdrpc.EntryType_CHANNEL_CLOSE,
		onChain:   true,
	}

	expected[accounting.FeeReference(closeTx.String())] = expectedReport{
		eventType: frdrpc.EntryType_CHANNEL_CLOSE_FEE,
		amount:    lnwire.MilliSatoshi(fee * 1000),
		onChain:   true,
	}

	// Because we force closed our channels, we also expect to have sweep
	// transactions for our commitment. Bob should have claimed our htlc on
	// chain, so we do not expect it to be swept.
	sweeps, err := c.aliceClient.WalletKit.ListSweeps(ctx)
	require.NoError(c.t, err, "could not get sweeps")
	require.Len(c.t, sweeps, 1)

	sweepHash, err := chainhash.NewHashFromStr(sweeps[0])
	require.NoError(c.t, err, "could not get sweeps txid")

	// We need to lookup our sweep tx to get our amount, because we subtract
	// fees off our swept commitment output, so we do not know exactly how
	// much we swept.
	sweepTx, err := c.bitcoindClient.GetRawTransactionVerbose(sweepHash)
	require.NoError(c.t, err, "could not lookup sweep")

	var sweepAmount btcutil.Amount
	for _, txout := range sweepTx.Vout {
		amt, err := btcutil.NewAmount(txout.Value)
		require.NoError(c.t, err, "could not get vout amount")
		sweepAmount += amt
	}

	expected[sweeps[0]] = expectedReport{
		amount:    lnwire.MilliSatoshi(sweepAmount * 1000),
		eventType: frdrpc.EntryType_SWEEP,
		onChain:   true,
	}

	// Get our fee for our sweep tx.
	sweepFee, err := fees.CalculateFee(
		c.bitcoindClient.GetRawTransactionVerbose, sweepHash,
	)
	require.NoError(c.t, err, "could get sweep fee")

	expected[accounting.FeeReference(sweeps[0])] = expectedReport{
		amount:    lnwire.MilliSatoshi(sweepFee * 1000),
		eventType: frdrpc.EntryType_SWEEP_FEE,
		onChain:   true,
	}

	// Bitcoin timestamps may be up to 2 hours in the future. We pad our
	// end time for this report so that we do not risk flakes due to
	// timestamp discrepancies.
	endTime := time.Now().Add(time.Hour * 2)

	// Query faraday for our node report. We disable fiat values so that we
	// do not query our fiat API during itests.
	actual, err := c.faradayClient.NodeReport(
		ctx, &frdrpc.NodeReportRequest{
			StartTime:   0,
			EndTime:     uint64(endTime.Unix()),
			DisableFiat: true,
			Granularity: frdrpc.Granularity_DAY,
		},
	)
	require.NoError(c.t, err)

	// We expect our node report to have the same number of entries as our
	// map of expected entries.
	require.Equal(c.t, len(expected), len(actual.Reports),
		"unexpected number of reports")

	// Run through the report entries we got, check that their reference
	// is in our set of expected entries and check the type and amount.
	for _, report := range actual.Reports {
		expectedEntry, ok := expected[report.Reference]
		require.True(c.t, ok, "unexpected %v %v entry in report, "+
			"txid: %v", report.Amount, report.Type, report.Txid)

		require.Equal(c.t, expectedEntry.eventType, report.Type,
			"wrong event type for %v", report.Reference)

		require.Equal(c.t, expectedEntry.amount,
			lnwire.MilliSatoshi(report.Amount), "wrong amount "+
				"for %v", expectedEntry.eventType)

		require.Equal(c.t, expectedEntry.onChain, report.OnChain,
			"on chain incorrect for %v", expectedEntry.eventType)
	}
}

// expectedReport contains the fields we match in our nodereport itest.
type expectedReport struct {
	amount    lnwire.MilliSatoshi
	eventType frdrpc.EntryType
	onChain   bool
}
