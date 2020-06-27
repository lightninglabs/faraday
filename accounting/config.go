package accounting

import (
	"context"
	"time"

	"github.com/lightninglabs/faraday/lndwrap"
	"github.com/lightninglabs/lndclient"
)

// OffChainConfig contains all the functionality required to produce an off
// chain report.
type OffChainConfig struct {
	// ListInvoices lists all our invoices.
	ListInvoices func() ([]lndclient.Invoice, error)

	// ListPayments lists all our payments.
	ListPayments func() ([]lndclient.Payment, error)

	// ListForwards lists all our forwards over out relevant period.
	ListForwards func() ([]lndclient.ForwardingEvent, error)

	// OwnPubKey is our node's public key. We use this value to identify
	// payments that are made to our own node.
	OwnPubKey string

	// StartTime is the time from which the report should be created,
	// inclusive.
	StartTime time.Time

	// EndTime is the time until which the report should be created,
	// exclusive.
	EndTime time.Time

	// DisableFiat is set if we want to produce a report without fiat
	// conversions. This is useful for testing, and for cases where our fiat
	// data api may be down.
	DisableFiat bool
}

// OnChainConfig contains all the functionality required to produce an on chain
// report.
type OnChainConfig struct {
	// OpenChannels provides a list of all currently open channels.
	OpenChannels func() ([]lndclient.ChannelInfo, error)

	// ClosedChannels provides a list of all closed channels.
	ClosedChannels func() ([]lndclient.ClosedChannel, error)

	// OnChainTransactions provides a list of all on chain transactions
	// relevant to our wallet over a block range.
	OnChainTransactions func() ([]lndclient.Transaction, error)

	// ListSweeps returns the transaction ids of the list of sweeps known
	// to lnd.
	ListSweeps func() ([]string, error)

	// CurrentHeight gets our current block height.
	CurrentHeight func() (uint32, error)

	// StartTime is the time from which the report should be created,
	// inclusive.
	StartTime time.Time

	// EndTime is the time until which the report should be created,
	// exclusive.
	EndTime time.Time

	// DisableFiat is set if we want to produce a report without fiat
	// conversions. This is useful for testing, and for cases where our fiat
	// data api may be down.
	DisableFiat bool
}

// NewOnChainConfig returns an on chain config from the lnd services provided.
func NewOnChainConfig(ctx context.Context, lnd lndclient.LndServices, startTime,
	endTime time.Time, disableFiat bool) *OnChainConfig {

	return &OnChainConfig{
		OpenChannels: lndwrap.ListChannels(
			ctx, lnd.Client, false,
		),
		ClosedChannels: func() ([]lndclient.ClosedChannel, error) {
			return lnd.Client.ClosedChannels(ctx)
		},
		OnChainTransactions: func() ([]lndclient.Transaction, error) {
			return lnd.Client.ListTransactions(ctx)
		},
		ListSweeps: func() ([]string, error) {
			return lnd.WalletKit.ListSweeps(ctx)
		},
		CurrentHeight: func() (uint32, error) {
			info, err := lnd.Client.GetInfo(ctx)
			if err != nil {
				return 0, nil
			}

			return info.BlockHeight, nil
		},
		StartTime:   startTime,
		EndTime:     endTime,
		DisableFiat: disableFiat,
	}
}

// NewOffChainConfig creates a config for creating off chain reports. It takes
// max parameters which allow control over the pagination size for queries to
// lnd.
func NewOffChainConfig(ctx context.Context, lnd lndclient.LndServices,
	maxInvoices, maxPayments, maxForwards uint64, ownPubkey string,
	startTime, endTime time.Time, disableFiat bool) *OffChainConfig {

	return &OffChainConfig{
		ListInvoices: func() ([]lndclient.Invoice, error) {
			return lndwrap.ListInvoices(
				ctx, 0, maxInvoices,
				lnd.Client,
			)
		},
		ListPayments: func() ([]lndclient.Payment, error) {
			return lndwrap.ListPayments(
				ctx, 0, maxPayments,
				lnd.Client,
			)
		},
		ListForwards: func() ([]lndclient.ForwardingEvent, error) {
			return lndwrap.ListForwards(
				ctx, maxForwards, startTime, endTime,
				lnd.Client,
			)
		},
		OwnPubKey:   ownPubkey,
		StartTime:   startTime,
		EndTime:     endTime,
		DisableFiat: disableFiat,
	}
}
