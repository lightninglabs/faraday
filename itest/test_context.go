package itest

import (
	"context"
	"fmt"
	"os/exec"
	"testing"
	"time"

	"github.com/btcsuite/btcd/btcutil"
	"github.com/btcsuite/btcd/chaincfg"
	"github.com/btcsuite/btcd/chaincfg/chainhash"
	"github.com/btcsuite/btcd/rpcclient"
	"github.com/btcsuite/btcd/wire"
	"github.com/lightninglabs/faraday"
	"github.com/lightninglabs/faraday/frdrpc"
	"github.com/lightninglabs/lndclient"
	invoicespkg "github.com/lightningnetwork/lnd/invoices"
	"github.com/lightningnetwork/lnd/lnrpc"
	"github.com/lightningnetwork/lnd/lnrpc/invoicesrpc"
	"github.com/lightningnetwork/lnd/lntypes"
	"github.com/lightningnetwork/lnd/lnwire"
	"github.com/lightningnetwork/lnd/routing/route"
	"github.com/stretchr/testify/require"
)

var (
	waitDuration = 20 * time.Second
	waitTick     = 200 * time.Millisecond

	processKillTimeout = 5 * time.Second

	faradayCmd = "./faraday"

	faradayCertPath     = "/root/.faraday/regtest/tls.cert"
	faradayMacaroonPath = "/root/.faraday/regtest/faraday.macaroon"

	faradayArgs = []string{
		"--rpclisten=localhost:8465",
		"--network=regtest",
		"--lnd.macaroonpath=lnd-alice/faraday-custom.macaroon",
		"--lnd.tlscertpath=lnd-alice/tls.cert",
		"--debuglevel=debug",
		"--connect_bitcoin",
		"--bitcoin.user=devuser",
		"--bitcoin.password=devpass",
		"--bitcoin.host=localhost:18443",
	}
)

// testContext provides a set up test environment for itests to run in.
type testContext struct {
	bitcoindClient *rpcclient.Client

	aliceClient *lndclient.GrpcLndServices
	bobClient   *lndclient.GrpcLndServices

	faradayClient frdrpc.FaradayServerClient

	alicePubkey, bobPubkey route.Vertex

	dummyAddress btcutil.Address

	faradayCmd *exec.Cmd
	faradayErr chan error

	t *testing.T
}

// newTestContext returns a new context instance.
func newTestContext(t *testing.T) *testContext {
	var err error

	ctx := &testContext{
		t: t,
	}

	// Setup rpc client for bitcoind.
	ctx.bitcoindClient, err = getBitcoindClient()
	require.NoError(t, err)

	// Set a dummy mining address.
	ctx.dummyAddress, err = btcutil.DecodeAddress(
		"2N9kBLwWmJjoPxBddwR8G9hwLMrQyHum44K",
		&chaincfg.RegressionNetParams,
	)
	require.NoError(t, err)

	// Setup rpc client for lnd instances.
	ctx.aliceClient, err = lndclient.NewLndServices(
		&lndclient.LndServicesConfig{
			LndAddress:   "localhost:10009",
			Network:      lndclient.NetworkRegtest,
			MacaroonDir:  "lnd-alice/data/chain/bitcoin/regtest",
			TLSPath:      "lnd-alice/tls.cert",
			CheckVersion: faraday.MinLndVersion,
		},
	)
	require.NoError(t, err)

	ctx.bobClient, err = lndclient.NewLndServices(
		&lndclient.LndServicesConfig{
			LndAddress:   "localhost:10002",
			Network:      lndclient.NetworkRegtest,
			MacaroonDir:  "lnd-bob/data/chain/bitcoin/regtest",
			TLSPath:      "lnd-bob/tls.cert",
			CheckVersion: faraday.MinLndVersion,
		},
	)
	require.NoError(t, err)

	// Get lnd instance info.
	aliceInfo, err := ctx.aliceClient.Client.GetInfo(context.Background())
	require.NoError(t, err)

	ctx.alicePubkey, err = route.NewVertexFromBytes(aliceInfo.IdentityPubkey[:])
	require.NoError(t, err)

	bobInfo, err := ctx.bobClient.Client.GetInfo(context.Background())
	require.NoError(t, err)

	ctx.bobPubkey, err = route.NewVertexFromBytes(bobInfo.IdentityPubkey[:])
	require.NoError(t, err)

	// Start faraday.
	ctx.startFaraday()

	return ctx
}

// mine signals btcd to mine the given number of blocks.
func (c *testContext) mineBlocks(blocks uint32) {
	_, err := c.bitcoindClient.GenerateToAddress(
		int64(blocks), c.dummyAddress, nil,
	)
	require.NoError(c.t, err)
}

// mine mines a block and returns the number of included txes.
func (c *testContext) mine() int {
	c.t.Helper()

	blockHashes, err := c.bitcoindClient.GenerateToAddress(
		1, c.dummyAddress, nil,
	)
	require.NoError(c.t, err)

	hash := blockHashes[0]
	block, err := c.bitcoindClient.GetBlock(hash)
	require.NoError(c.t, err)

	// Subtract coinbase tx.
	return len(block.Transactions) - 1
}

// mine mines a block and verifies that the expected number of transactions is
// present (excluding the coinbase tx).
func (c *testContext) mineExactly(expectedTxCount int) {
	c.t.Helper()

	txCount := c.mine()
	require.Equal(c.t, expectedTxCount, txCount)
}

// mempoolTxCount returns the number of txes currently in the mempool.
func (c *testContext) mempoolTxCount() int {
	txes, err := c.bitcoindClient.GetRawMempool()
	require.NoError(c.t, err)

	return len(txes)
}

// balances stores the wallet and channel balances for alice and bob.
type balances struct {
	aliceWallet, aliceChannel btcutil.Amount
	bobWallet, bobChannel     btcutil.Amount
}

// String returns human-readable balances
func (b balances) String() string {
	return fmt.Sprintf(
		"alice: wallet=%v,channel=%v, bob: wallet=%v,channel=%v",
		b.aliceWallet, b.aliceChannel,
		b.bobWallet, b.bobChannel,
	)
}

// getBalances returns the balances for the client and server lnd instances.
func (c *testContext) getBalances() balances {
	get := func(lnd lndclient.LightningClient) (btcutil.Amount,
		btcutil.Amount) {

		walletResp, err := lnd.WalletBalance(
			context.Background(),
		)
		require.NoError(c.t, err)

		channelResp, err := lnd.WalletBalance(
			context.Background(),
		)
		require.NoError(c.t, err)

		return walletResp.Confirmed, channelResp.Confirmed
	}

	var b balances
	b.aliceWallet, b.aliceChannel = get(c.aliceClient.Client)
	b.bobWallet, b.bobChannel = get(c.bobClient.Client)

	return b
}

// waitBalance keeps querying the lnd balances until the given condition is met.
func (c *testContext) waitBalance(condition func(balances) bool) {
	c.t.Helper()

	c.eventuallyf(
		func() bool {
			return condition(c.getBalances())
		},
		"timeout waiting for balance",
	)
}

// eventuallyf wraps testify's Eventuallyf method with default time parameters.
func (c *testContext) eventuallyf(condition func() bool, msg string,
	args ...interface{}) { // nolint:unparam

	c.t.Helper()

	require.Eventuallyf(
		c.t, condition, waitDuration, waitTick, msg, args,
	)
}

func (c *testContext) addInvoice(client lndclient.LightningClient,
	amt lnwire.MilliSatoshi) (lntypes.Hash, string) {

	// Add an invoice to the receiving client.
	hash, payreq, err := client.AddInvoice(
		context.Background(), &invoicesrpc.AddInvoiceData{
			Value: amt,
		},
	)
	require.NoError(c.t, err, "could not add invoice")

	return hash, payreq
}

// makePayment takes a source and destination node and makes a payment of the
// amount provided from the source to the destination node. Liquidity should be
// sufficiently balanced to make the payment. The function returns the payment
// preimage used and the total fees paid by the sender.
func (c *testContext) makePayment(src, dest lndclient.LndServices,
	payreq lndclient.SendPaymentRequest,
	targetState lnrpc.Payment_PaymentStatus) lntypes.Preimage {

	paymentChan, errChan, err := src.Router.SendPayment(
		context.Background(), payreq,
	)
	require.NoError(c.t, err, "could not send payment")

	// Wait for the payment to report as a success on the payer's side.
	var preimage lntypes.Preimage

	c.eventuallyf(func() bool {
		select {
		case status := <-paymentChan:
			preimage = status.Preimage
			return status.State == targetState

		case <-errChan:
			require.NoError(c.t, err, "error sending payment")
			return false
		}
	}, "payment did not reach: %v", targetState)

	// Wait for the recipient to settle the invoice if our target state was
	// to settle the payment.
	if targetState == lnrpc.Payment_SUCCEEDED {
		c.eventuallyf(func() bool {
			inv, err := dest.Client.LookupInvoice(
				context.Background(), *payreq.PaymentHash,
			)
			if err != nil {
				return false
			}

			return inv.State == invoicespkg.ContractSettled
		}, "payment not received")
	}

	return preimage
}

// closeChannel closes a channel, mines blocks until the channel is completely
// resolved and returns the closed txid.
func (c *testContext) closeChannel(client lndclient.LightningClient,
	channel *wire.OutPoint, force bool) (chainhash.Hash, btcutil.Amount) {

	closeChan, errChan, err := client.CloseChannel(
		context.Background(), channel, force, 0, nil,
	)
	require.NoError(c.t, err, "could not close channel")

	var (
		closeTx  chainhash.Hash
		closeFee btcutil.Amount
	)

	// Wait for us to get an update from our channel indicating that it is
	// pending close.
	c.eventuallyf(func() bool {
		select {
		case update := <-closeChan:
			closeTx = update.CloseTxid()

			switch update.(type) {
			case *lndclient.PendingCloseUpdate:
				// Get our close tx from the mempool to get its fee
				// and add an expected entry because we opened the
				// channel so we pay the fees.
				close, err := c.bitcoindClient.GetMempoolEntry(
					closeTx.String(),
				)
				require.NoError(c.t, err, "could not get mempool")

				closeFee, err = btcutil.NewAmount(close.Fee)
				require.NoError(c.t, err, "could not get fee")

			case *lndclient.ChannelClosedUpdate:
				return true
			}

		case <-errChan:
			c.t.Fatalf("error closing channel: %v, %v", channel,
				err)

		// If we have not received an update yet, mine a block.
		default:
			c.mine()
		}

		return false
	}, "channel did not enter pending close state")

	// Now, we want to wait to for our channel to resolve. This can take
	// much longer than getting a pending close, because our force closed
	// channels need to wait for their outputs to unlock. We use a custom
	// timeout in this case.
	waitForClose := func() bool {
		channels, err := client.ClosedChannels(context.Background())
		require.NoError(c.t, err, "could not get closed channels")

		for _, channel := range channels {
			if channel.ClosingTxHash == closeTx.String() {
				return true
			}
		}

		// If we did not find our channel, mine another block then
		// return.
		c.mine()
		return false
	}
	require.Eventuallyf(
		c.t, waitForClose, time.Minute*2, waitTick,
		"could not resolve force closed channel: %v", channel,
	)

	return closeTx, closeFee
}

// waitForWalletsSynced waits for both nodes to report their wallets as synced
// to the graph.
func (c *testContext) waitForWalletsSynced() {
	c.eventuallyf(func() bool {
		info, err := c.aliceClient.Client.GetInfo(context.Background())
		if err != nil {
			return false
		}

		if !info.SyncedToGraph {
			return false
		}

		info, err = c.bobClient.Client.GetInfo(context.Background())
		if err != nil {
			return false
		}

		return info.SyncedToGraph
	}, "wallets never synced to chain")
}

// openChannel opens a channel and waits for both sides to see it.
func (c *testContext) openChannel(src lndclient.LightningClient,
	destKey route.Vertex, amount btcutil.Amount) (*wire.OutPoint,
	btcutil.Amount) {

	// Wait for both of our nodes to report that their wallets are synced
	// so that we can open a channel between them.
	c.waitForWalletsSynced()

	channel, err := src.OpenChannel(
		context.Background(), destKey, amount, 0, false,
	)
	require.NoError(c.t, err, "could not open channel")

	// Once we have initiated opening node's channel, we get it from the
	// mempool so that we can get our tx fee.
	c.waitForMempoolTxCount(1, "channel not in mempool")

	tx, err := c.bitcoindClient.GetMempoolEntry(channel.Hash.String())
	require.NoError(c.t, err, "could not get mempool")

	fee, err := btcutil.NewAmount(tx.Fee)
	require.NoError(c.t, err, "channel fee failed")

	// Wait for both nodes to see the channel.
	c.waitForChannelOpen(channel)

	return channel, fee
}

// waitForChannelOpen waits for a channel between alice and bob to become
// active.
func (c *testContext) waitForChannelOpen(targetChannel *wire.OutPoint) {
	c.t.Helper()

	c.eventuallyf(
		func() bool {
			c.mine()

			aliceChans, err := c.aliceClient.Client.ListChannels(
				context.Background(), false, false,
			)
			require.NoError(c.t, err)

			// If we did not find our target channel, we fail.
			if findChannel(aliceChans, targetChannel) == nil {
				return false
			}

			bobChans, err := c.bobClient.Client.ListChannels(
				context.Background(), false, false,
			)
			require.NoError(c.t, err)

			// Succeed if we found our channel in in bob's channels.
			return findChannel(bobChans, targetChannel) != nil
		},
		"channel not open",
	)
}

// findChannel finds a channel in a set of open channels, returning nil if it
// is not found.
// nolint:interfacer
func findChannel(channels []lndclient.ChannelInfo,
	target *wire.OutPoint) *lndclient.ChannelInfo {

	for _, channel := range channels {
		if channel.ChannelPoint == target.String() {
			// Declare a variable in our scope so we don't return
			// a pointer to a range variable.
			foundChannel := channel
			return &foundChannel
		}
	}

	return nil
}

// waitForMempoolTxCount waits until the specified number of txes are present in
// the mempool
func (c *testContext) waitForMempoolTxCount(txCount int, msg string) {
	c.t.Helper()

	c.eventuallyf(
		func() bool {
			return c.mempoolTxCount() == txCount
		},
		msg,
	)
}

// waitForTxesAndMine waits for a specified number of txes to arrive in the
// mempool and then mines a block.
func (c *testContext) waitForTxesAndMine(txCount int, msg string) {
	c.t.Helper()

	c.waitForMempoolTxCount(txCount, msg)
	c.mineExactly(txCount)
}

// mempoolEmpty asserts that the mempool is empty.
func (c *testContext) mempoolEmpty() {
	c.t.Helper()

	require.Equal(c.t, 0, c.mempoolTxCount(), "mempool not empty")
}

// startFaraday starts faraday, connecting to our test context's alice lnd node.
// It returns process start errors and an error channel for errors that occur
// after the start.
func (c *testContext) startFaraday() {
	// Start loop client daemon.
	c.faradayCmd = exec.Command(
		faradayCmd, faradayArgs...,
	)

	attachPrefixStdout(c.faradayCmd, "faraday")

	log.Info("Starting Faraday")
	require.NoError(c.t, c.faradayCmd.Start())

	c.faradayErr = make(chan error, 1)
	go func() {
		c.faradayErr <- c.faradayCmd.Wait()
	}()

	// Setup connection to faraday.
	var err error
	c.eventuallyf(func() bool {
		c.faradayClient, err = getFaradayClient(
			"localhost:8465", faradayCertPath, faradayMacaroonPath,
		)
		return err == nil
	}, "could not connect to faraday process: %v", err)

	// Wait for connectivity.
	c.eventuallyf(func() bool {
		_, err = c.faradayClient.ChannelInsights(
			context.Background(), &frdrpc.ChannelInsightsRequest{},
		)
		return err == nil
	}, "could not connect to faraday process: %v", err)
}

// stopFaraday stops the faraday process.
func (c *testContext) stopFaraday() {
	if c.faradayCmd == nil {
		return
	}

	// Kill the faraday process.
	require.NoError(c.t, c.faradayCmd.Process.Kill())

	select {
	case <-c.faradayErr:
	case <-time.After(processKillTimeout):
		require.FailNow(c.t, "cannot kill faraday process")
	}

	c.faradayCmd = nil
}

// stop stops the faraday process.
func (c *testContext) stop() {
	c.stopFaraday()
}
