package accounting

import (
	"context"

	"github.com/lightninglabs/faraday/utils"
	"github.com/lightninglabs/lndclient"
)

// OnChainReport produces a report of our on chain activity for a period using
// live price data. Note that this report relies on transactions returned by
// GetTransactions in lnd. If a transaction is not included in this response
// (eg, a remote party opening a channel to us), it will not be included.
func OnChainReport(ctx context.Context, cfg *OnChainConfig) (Report, error) {
	// Retrieve a function which can be used to query individual prices,
	// or a no-op function if we do not want prices.
	getPrice, err := getConversion(
		ctx, cfg.StartTime, cfg.EndTime, cfg.DisableFiat,
	)
	if err != nil {
		return nil, err
	}

	return onChainReportWithPrices(cfg, getPrice)
}

// onChainReportWithPrices produces off chain reports using the getPrice
// function provided. This allows testing of our report creation without calling
// the actual price API.
func onChainReportWithPrices(cfg *OnChainConfig, getPrice msatToFiat) (Report,
	error) {
	onChainTxns, err := cfg.OnChainTransactions()
	if err != nil {
		return nil, err
	}

	// Filter our on chain transactions by start and end time. If we have
	// no on chain transactions over this period, we can return early.
	filtered := filterOnChain(cfg.StartTime, cfg.EndTime, onChainTxns)
	if len(filtered) == 0 {
		return Report{}, nil
	}

	// Get our opened channels and create a map of closing txid to the
	// channel entry. This will be used to separate channel opens out from
	// other on chain transactions.
	openRPCChannels, err := cfg.OpenChannels()
	if err != nil {
		return nil, err
	}

	openChannels := make(map[string]lndclient.ChannelInfo)
	for _, channel := range openRPCChannels {
		outpoint, err := utils.GetOutPointFromString(
			channel.ChannelPoint,
		)
		if err != nil {
			return nil, err
		}

		// Add the channel to our map, keyed by txid.
		openChannels[outpoint.Hash.String()] = channel
	}

	// Get our closed channels and create a map of closing txid to closed
	// channel. This will be used to separate out channel closes from other
	// on chain transactions.
	closedRPCChannels, err := cfg.ClosedChannels()
	if err != nil {
		return nil, err
	}

	// We create two maps here, one keyed by the close transaction ids for
	// our already closed channels, and another keyed by their channel point.
	// We do this so that we can also match the on chain open transaction
	// for channels that are already closed.
	channelCloses := make(map[string]lndclient.ClosedChannel)
	channelOpens := make(map[string]lndclient.ClosedChannel)

	for _, closedChannel := range closedRPCChannels {
		channelCloses[closedChannel.ClosingTxHash] = closedChannel

		outpoint, err := utils.GetOutPointFromString(
			closedChannel.ChannelPoint,
		)
		if err != nil {
			return nil, err
		}
		channelOpens[outpoint.Hash.String()] = closedChannel
	}

	// Finally, get our list of known sweeps from lnd so that we can
	// identify them separately to other on chain transactions.
	sweeps, err := cfg.ListSweeps()
	if err != nil {
		return nil, err
	}

	isSweep := make(map[string]bool, len(sweeps))
	for _, sweep := range sweeps {
		isSweep[sweep] = true
	}

	return onChainReport(
		filtered, getPrice, openChannels, isSweep, channelOpens,
		channelCloses,
	)
}

// onChainReport produces an on chain transaction report.
func onChainReport(txns []lndclient.Transaction, priceFunc msatToFiat,
	currentlyOpenChannels map[string]lndclient.ChannelInfo,
	sweeps map[string]bool, channelOpenTransactions,
	channelCloseTransactions map[string]lndclient.ClosedChannel) (
	Report, error) {

	txMap := make(map[string]lndclient.Transaction, len(txns))
	for _, tx := range txns {
		txMap[tx.TxHash] = tx
	}

	var report Report

	for _, txn := range txns {
		// If the transaction is in our set of currently open channels,
		// we just need an open channel entry for it.
		openChannel, ok := currentlyOpenChannels[txn.TxHash]
		if ok {
			entries, err := channelOpenEntries(
				openChannel, txn, priceFunc,
			)
			if err != nil {
				return nil, err
			}
			report = append(report, entries...)
		}

		// If the transaction is a channel opening transaction for one
		// of our already closed channels, we need to reconstruct a
		// channel open from our close summary.
		channelOpen, ok := channelOpenTransactions[txn.TxHash]
		if ok {
			entries, err := openChannelFromCloseSummary(
				channelOpen, txn, priceFunc,
			)
			if err != nil {
				return nil, err
			}

			report = append(report, entries...)
			continue
		}

		// If the transaction is a channel close, we create channel
		// close records from our close summary.
		channelClose, ok := channelCloseTransactions[txn.TxHash]
		if ok {
			entries, err := closedChannelEntries(
				channelClose, txn, priceFunc,
			)
			if err != nil {
				return nil, err
			}

			report = append(report, entries...)
			continue
		}

		// Finally, if the transaction is unrelated to channel opens or
		// closes, we create a generic on chain entry for it. We check
		// our list of known sweeps for this tx so that we can separate
		// it our from regular chain sends.
		isSweep := sweeps[txn.TxHash]

		entries, err := onChainEntries(txn, isSweep, priceFunc)
		if err != nil {
			return nil, err
		}
		report = append(report, entries...)
	}

	return report, nil
}
