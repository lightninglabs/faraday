package itest

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestSetup sets up a shared test environment.
func TestSetup(t *testing.T) {
	log.Infof("Running golang test setup")

	c := newTestContext(t)

	// Supply client and server with coins.
	aliceAddr, err := c.aliceClient.WalletKit.NextAddr(
		context.Background(),
	)
	require.NoError(c.t, err)

	_, err = c.bitcoindClient.GenerateToAddress(1, aliceAddr, nil)
	require.NoError(c.t, err)

	bobAddr, err := c.bobClient.WalletKit.NextAddr(
		context.Background(),
	)
	require.NoError(c.t, err)

	_, err = c.bitcoindClient.GenerateToAddress(1, bobAddr, nil)
	require.NoError(c.t, err)

	// Mine 100 blocks to allow spending of the coinbase txes.
	c.mineBlocks(100)

	c.waitBalance(func(b balances) bool {
		return b.aliceWallet > 0 && b.bobWallet > 0
	})

	log.Infof("Test setup complete")
}
