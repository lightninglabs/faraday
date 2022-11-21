package resolutions

import (
	"errors"
	"fmt"

	"github.com/btcsuite/btcd/btcjson"
	"github.com/btcsuite/btcd/btcutil"
	"github.com/btcsuite/btcd/chaincfg/chainhash"
	"github.com/btcsuite/btcd/wire"
	"github.com/lightninglabs/faraday/utils"
	"github.com/lightninglabs/lndclient"
	"github.com/shopspring/decimal"
)

var (
	// ErrChannelNotClosed is returned when we get a request for a close
	// channel report for a channel that is not present in lnd's list of
	// closed channels.
	ErrChannelNotClosed = errors.New("channel not closed, cannot create " +
		"report")

	// ErrCloseTypeNotSupported is returned when we do not yet support
	// creation of close reports for the channel type provided.
	ErrCloseTypeNotSupported = errors.New("close reports for type not " +
		"supported")

	// errBatchedTx is returned when we are trying to get the fees for a
	// transaction but it is not of the format we expect (max 2 outputs,
	// one change one output).
	errBatchedTx = errors.New("cannot calculate fees for batched " +
		"transaction")
)

// Config provides all the external functions and parameters required to produce
// reports on closed channels.
type Config struct {
	// ClosedChannels returns a list of our currently closed channels.
	ClosedChannels func() ([]lndclient.ClosedChannel, error)

	// WalletTransactions returns a list of transactions that are relevant
	// to our wallet.
	WalletTransactions func() ([]lndclient.Transaction, error)

	// GetTxDetail looks up an on chain transaction and returns the raw
	// tx result which contains a detailed set of information about the
	// transaction.
	GetTxDetail func(txHash *chainhash.Hash) (*btcjson.TxRawResult, error)

	// CalculateFees gets the total on chain fees for a transaction.
	CalculateFees func(*chainhash.Hash) (btcutil.Amount, error)
}

// ChannelCloseReport returns a full report on a closed channel.
func ChannelCloseReport(cfg *Config, chanPoint string) (*CloseReport, error) {
	// First, get our set of closed channels and make sure that the
	closed, err := cfg.ClosedChannels()
	if err != nil {
		return nil, err
	}

	var (
		closedChannel lndclient.ClosedChannel
		found         bool
	)

	for _, channel := range closed {
		if channel.ChannelPoint == chanPoint {
			closedChannel = channel
			found = true
			break
		}
	}

	if !found {
		return nil, ErrChannelNotClosed
	}

	outpoint, err := utils.GetOutPointFromString(closedChannel.ChannelPoint)
	if err != nil {
		return nil, err
	}

	// Lookup our transaction and check that it has at most two outputs,
	// allowing for a funding outpoint and change address. Our current
	// fee calculations do not account for batched transactions.
	tx, err := cfg.GetTxDetail(&outpoint.Hash)
	if err != nil {
		return nil, err
	}

	// TODO(carla): support batched transactions.
	if len(tx.Vout) > 2 {
		return nil, errBatchedTx
	}

	switch closedChannel.CloseType {
	case lndclient.CloseTypeCooperative:
		return coopCloseReport(cfg, outpoint, &closedChannel)

	default:
		return nil, ErrCloseTypeNotSupported
	}
}

// CloseReport represents a closed channel.
type CloseReport struct {
	// ChannelPoint is the outpoint of the funding transaction.
	ChannelPoint *wire.OutPoint

	// ChannelInitiator is true if we opened the channel.
	ChannelInitiator bool

	// CloseType reflects the type of close that occurred.
	CloseType lndclient.CloseType

	// CloseTxid is the transaction ID of the channel close.
	CloseTxid string

	// OpenFee is the amount of fees we paid to open the channel in
	// satoshis. Note that this will be zero for the current protocol where
	// the initiating party pays for the channel to be opened.
	OpenFee decimal.Decimal

	// CloseFee is the amount of fees we paid to close the channel in
	// satoshis. Note that this will be zero for the current protocol where
	// the initiating party pays for the channel to be closed.
	CloseFee decimal.Decimal
}

// coopCloseReport creates a channel report for a cooperatively closed channel
// where we do not need to worry about on chain resolutions.
func coopCloseReport(cfg *Config, chanPoint *wire.OutPoint,
	channel *lndclient.ClosedChannel) (*CloseReport, error) {

	report := &CloseReport{
		ChannelPoint:     chanPoint,
		ChannelInitiator: false,
		CloseType:        channel.CloseType,
		CloseTxid:        channel.ClosingTxHash,
		OpenFee:          decimal.Zero,
		CloseFee:         decimal.Zero,
	}

	// We pay fees based on whether we opened the channel or not, so we
	// switch on our open initiator field (which may be unknown) to decide
	// whether we need to get fee information.
	switch channel.OpenInitiator {
	// If the remote party opened the channel, we do not need to get any
	// further information about the open and close fees, because we know
	// the remote party paid them. We can just return our report as is.
	case lndclient.InitiatorRemote:
		return report, nil

	// If we know we opened the channel, we fallthrough to get our open and
	// close fees.
	case lndclient.InitiatorLocal:
		report.ChannelInitiator = true

	// If we do not know whether we opened the channel or not, we lookup our
	// funding outpoint with our wallet to determine whether is is ours or
	// not. If it isn't ours, we just return the report as is. If it is, we
	// fallthrough to get additional information about the close.
	case lndclient.InitiatorUnrecorded:
		var err error
		report.ChannelInitiator, err = getCloseInitiatorFromWallet(
			cfg, report.ChannelPoint.Hash.String(),
		)
		if err != nil {
			return nil, err
		}

		// If we did not open the channel, we can just return here
		// because we do not need to record any fees (we did not pay
		// them). If we did open the channel, we fallthrough to get
		// our fee information.
		if !report.ChannelInitiator {
			return report, nil
		}

	default:
		return nil, fmt.Errorf("unknown inititor: %v",
			channel.OpenInitiator)
	}

	// At this stage, we know that we opened the channel. We now lookup our
	// open and close transactions to get the fees we paid for them.
	var err error
	openFee, err := cfg.CalculateFees(&chanPoint.Hash)
	if err != nil {
		return nil, err
	}
	report.OpenFee = decimal.NewFromInt(int64(openFee))

	closeHash, err := chainhash.NewHashFromStr(channel.ClosingTxHash)
	if err != nil {
		return nil, err
	}

	// Get the fees for our closing transaction. Since we will have to pay
	// for the full close transaction (regardless of whether we have an
	// output), we get our total fees for this transaction rather than for
	// a specific outpoint.
	closeFee, err := cfg.CalculateFees(closeHash)
	if err != nil {
		return nil, err
	}
	report.CloseFee = decimal.NewFromInt(int64(closeFee))

	return report, nil
}

// getCloseInitiatorFromWallet figures out whether we initiated opening a
// channel by checking whether the opening transaction is in our set of wallet
// relevant transactions. If it is present, we contributed funds or published
// (in the case of psbt) the channel, so we were the opening party.
func getCloseInitiatorFromWallet(cfg *Config, openTx string) (bool,
	error) {

	txns, err := cfg.WalletTransactions()
	if err != nil {
		return false, err
	}

	for _, tx := range txns {
		if tx.TxHash == openTx {
			return true, nil
		}
	}

	return false, nil
}
