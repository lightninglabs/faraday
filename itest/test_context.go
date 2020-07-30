package itest

import (
	"context"
	"fmt"
	"os/exec"
	"testing"
	"time"

	"github.com/btcsuite/btcd/chaincfg"
	"github.com/btcsuite/btcd/rpcclient"
	"github.com/btcsuite/btcd/wire"
	"github.com/btcsuite/btcutil"
	"github.com/lightninglabs/faraday/frdrpc"
	"github.com/lightninglabs/lndclient"
	"github.com/lightningnetwork/lnd/routing/route"
	"github.com/stretchr/testify/require"
)

var (
	waitDuration = 20 * time.Second
	waitTick     = 500 * time.Millisecond

	processKillTimeout = 5 * time.Second

	faradayCmd = "./faraday"

	faradayArgs = []string{
		"--rpclisten=localhost:8465",
		"--regtest",
		"--macaroondir=lnd-alice/data/chain/bitcoin/regtest",
		"--tlscertpath=lnd-alice/tls.cert",
		"--debuglevel=debug",
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
			LndAddress:  "localhost:10009",
			Network:     lndclient.NetworkRegtest,
			MacaroonDir: "lnd-alice/data/chain/bitcoin/regtest",
			TLSPath:     "lnd-alice/tls.cert",
		},
	)
	require.NoError(t, err)

	ctx.bobClient, err = lndclient.NewLndServices(
		&lndclient.LndServicesConfig{
			LndAddress:  "localhost:10002",
			Network:     lndclient.NetworkRegtest,
			MacaroonDir: "lnd-bob/data/chain/bitcoin/regtest",
			TLSPath:     "lnd-bob/tls.cert",
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

		walletResp, err := lnd.ConfirmedWalletBalance(
			context.Background(),
		)
		require.NoError(c.t, err)

		channelResp, err := lnd.ConfirmedWalletBalance(
			context.Background(),
		)
		require.NoError(c.t, err)

		return walletResp, channelResp
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

// waitForChannelOpen waits for a channel between alice and bob to become
// active.
func (c *testContext) waitForChannelOpen(targetChannel *wire.OutPoint) {
	c.t.Helper()

	c.eventuallyf(
		func() bool {
			c.mine()

			aliceChans, err := c.aliceClient.Client.ListChannels(
				context.Background(),
			)
			require.NoError(c.t, err)

			// If we did not find our target channel, we fail.
			if findChannel(aliceChans, targetChannel) == nil {
				return false
			}

			bobChans, err := c.bobClient.Client.ListChannels(
				context.Background(),
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
	c.faradayClient, err = getFaradayClient("localhost:8465")
	require.NoError(c.t, err)

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
