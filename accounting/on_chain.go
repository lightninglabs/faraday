package accounting

import (
	"context"

	"github.com/btcsuite/btcd/btcutil"
	"github.com/btcsuite/btcd/wire"
	"github.com/lightninglabs/faraday/utils"
	"github.com/lightninglabs/lndclient"
	"github.com/lightningnetwork/lnd/lnwire"
	"github.com/lightningnetwork/lnd/routing/route"
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
		cfg.PriceSourceCfg,
	)
	if err != nil {
		return nil, err
	}

	info, err := getOnChainInfo(cfg, getPrice)
	if err != nil {
		return nil, err
	}

	return onChainReport(info)
}

// onChainInformation contains all the information we require to produce an
// on chain report.
type onChainInformation struct {
	txns []lndclient.Transaction
	entryUtils
	sweeps         map[string]bool
	openedChannels map[string]channelInfo
	closedChannels map[string]closedChannelInfo
}

// channelInfo contains information that is common to open and closed channels.
type channelInfo struct {
	channelPoint *wire.OutPoint
	capacity     btcutil.Amount
	pubKeyBytes  route.Vertex
	initiator    lndclient.Initiator
	channelID    lnwire.ShortChannelID
}

// closedChannelInfo contains channel information which has further close info.
type closedChannelInfo struct {
	channelInfo
	closeType      string
	closeInitiator string
}

func newChannelInfo(id lnwire.ShortChannelID, chanPoint *wire.OutPoint,
	pubkey route.Vertex, capacity btcutil.Amount,
	initiator lndclient.Initiator) channelInfo {

	return channelInfo{
		channelID:    id,
		channelPoint: chanPoint,
		pubKeyBytes:  pubkey,
		capacity:     capacity,
		initiator:    initiator,
	}
}

// getOnChainInfo queries lnd for all transactions relevant to our on chain
// transactions, and produces the set of information that we will need to create
// an on chain report.
func getOnChainInfo(cfg *OnChainConfig, getPrice fiatPrice) (*onChainInformation,
	error) {

	// Create an info struct to hold all the elements we need.
	info := &onChainInformation{
		entryUtils: entryUtils{
			getFiat:          getPrice,
			getFee:           cfg.GetFee,
			customCategories: cfg.Categories,
		},
		openedChannels: make(map[string]channelInfo),
		sweeps:         make(map[string]bool),
		closedChannels: make(map[string]closedChannelInfo),
	}

	onChainTxns, err := cfg.OnChainTransactions()
	if err != nil {
		return nil, err
	}

	// Filter our on chain transactions by start and end time. If we have
	// no confirmed on chain transactions over this period, we can return
	// early.
	info.txns, err = filterOnChain(cfg.StartTime, cfg.EndTime, onChainTxns)
	if err != nil {
		return nil, err
	}

	if len(info.txns) == 0 {
		return info, nil
	}

	// Get our pending channels so that we do not miss channel transactions
	// that may have confirmed on chain, and will thus be included in our
	// set of transactions, but are still considered pending by lnd (this
	// is the case for channel opens that require more than one conf, or for
	// closing channels that are awaiting resolution).
	pending, err := cfg.PendingChannels()
	if err != nil {
		return nil, err
	}

	// We add our pending force close channels to opened and closed channels
	// because it is possible that our channel was opened and closed in the
	// relevant period.
	for _, c := range pending.PendingForceClose {
		inf := newChannelInfo(
			lnwire.NewShortChanIDFromInt(0), c.ChannelPoint,
			c.PubKeyBytes, c.Capacity, c.ChannelInitiator,
		)

		info.openedChannels[c.ChannelPoint.Hash.String()] = inf
		info.closedChannels[c.CloseTxid.String()] = closedChannelInfo{
			channelInfo:    inf,
			closeType:      "force close",
			closeInitiator: "unknown for pending channels",
		}
	}

	// Add our channel open and all possible channel closes to our info set.
	// We add all potential close txids in case one of them has confirmed.
	for _, c := range pending.WaitingClose {
		inf := newChannelInfo(
			lnwire.NewShortChanIDFromInt(0), c.ChannelPoint,
			c.PubKeyBytes, c.Capacity, c.ChannelInitiator,
		)

		info.openedChannels[c.ChannelPoint.Hash.String()] = inf

		closed := closedChannelInfo{
			channelInfo:    inf,
			closeType:      lndclient.CloseTypeCooperative.String(),
			closeInitiator: lndclient.InitiatorUnrecorded.String(),
		}
		info.closedChannels[c.LocalTxid.String()] = closed
		info.closedChannels[c.RemoteTxid.String()] = closed
		info.closedChannels[c.RemotePending.String()] = closed
	}

	// Add our pending open channel to our set of open channels so that
	// we can identify pending channels in our report.
	for _, c := range pending.PendingOpen {
		inf := newChannelInfo(
			lnwire.NewShortChanIDFromInt(0), c.ChannelPoint,
			c.PubKeyBytes, c.Capacity, c.ChannelInitiator,
		)

		info.openedChannels[c.ChannelPoint.Hash.String()] = inf
	}

	// Get our opened channels and create a map of closing txid to the
	// channel entry. This will be used to separate channel opens out from
	// other on chain transactions.
	openRPCChannels, err := cfg.OpenChannels()
	if err != nil {
		return nil, err
	}

	for _, channel := range openRPCChannels {
		outpoint, err := utils.GetOutPointFromString(
			channel.ChannelPoint,
		)
		if err != nil {
			return nil, err
		}

		init := lndclient.InitiatorLocal
		if !channel.Initiator {
			init = lndclient.InitiatorRemote
		}

		inf := newChannelInfo(
			lnwire.NewShortChanIDFromInt(channel.ChannelID),
			outpoint, channel.PubKeyBytes, channel.Capacity, init,
		)

		// Add the channel to our map, keyed by txid.
		info.openedChannels[outpoint.Hash.String()] = inf
	}

	// Get our closed channels and create a map of closing txid to closed
	// channel. This will be used to separate out channel closes from other
	// on chain transactions.
	closedRPCChannels, err := cfg.ClosedChannels()
	if err != nil {
		return nil, err
	}

	// Add our already closed channels open and closed transactions to our
	// on chain info so that we will be able to detect channels that were
	// opened and closed within our period.
	for _, closed := range closedRPCChannels {
		outpoint, err := utils.GetOutPointFromString(
			closed.ChannelPoint,
		)
		if err != nil {
			return nil, err
		}

		inf := newChannelInfo(
			lnwire.NewShortChanIDFromInt(closed.ChannelID),
			outpoint, closed.PubKeyBytes, closed.Capacity,
			closed.OpenInitiator,
		)

		info.openedChannels[outpoint.Hash.String()] = inf

		info.closedChannels[closed.ClosingTxHash] = closedChannelInfo{
			channelInfo:    inf,
			closeType:      closed.CloseType.String(),
			closeInitiator: closed.CloseInitiator.String(),
		}
	}

	// Finally, get our list of known sweeps from lnd so that we can
	// identify them separately to other on chain transactions.
	sweeps, err := cfg.ListSweeps()
	if err != nil {
		return nil, err
	}

	for _, sweep := range sweeps {
		info.sweeps[sweep] = true
	}

	return info, nil
}

// onChainReport produces an on chain transaction report.
func onChainReport(info *onChainInformation) (
	Report, error) {

	var report Report

	for _, txn := range info.txns {
		// If the transaction is a channel open. The channel may be one
		// of our currently open channels, or a channel open for a
		// channel that has already been closed.
		openChannel, ok := info.openedChannels[txn.TxHash]
		if ok {
			entries, err := channelOpenEntries(
				openChannel, txn, info.entryUtils,
			)
			if err != nil {
				return nil, err
			}

			report = append(report, entries...)
			continue
		}

		// Check whether the transaction is a channel close.
		channelClose, ok := info.closedChannels[txn.TxHash]
		if ok {
			entries, err := closedChannelEntries(
				channelClose, txn, info.entryUtils,
			)
			if err != nil {
				return nil, err
			}

			report = append(report, entries...)
			continue
		}

		// Next, we check whether our transaction is a sweep, and create
		// sweep entries that include looking up fees so that we do not
		// miss fees that are contributed by the swept input.
		if info.sweeps[txn.TxHash] {
			entries, err := sweepEntries(
				txn, info.entryUtils,
			)
			if err != nil {
				return nil, err
			}

			report = append(report, entries...)
			continue
		}

		// Finally, if the transaction is unrelated to channel opens or
		// closes, we create a generic on chain entry for it.
		entries, err := onChainEntries(txn, info.entryUtils)
		if err != nil {
			return nil, err
		}
		report = append(report, entries...)
	}

	return report, nil
}
