package accounting

import (
	"context"
	"time"

	"github.com/btcsuite/btcd/btcutil"
	"github.com/btcsuite/btcd/chaincfg/chainhash"
	"github.com/lightninglabs/faraday/fees"
	"github.com/lightninglabs/faraday/fiat"
	"github.com/lightninglabs/faraday/lndwrap"
	"github.com/lightninglabs/lndclient"
	"github.com/lightningnetwork/lnd/routing/route"
)

// decodePaymentRequest is a signature for decoding payment requests.
type decodePaymentRequest func(payReq string) (*lndclient.PaymentRequest, error)

// OffChainConfig contains all the functionality required to produce an off
// chain report.
type OffChainConfig struct {
	CommonConfig

	// ListInvoices lists all our invoices.
	ListInvoices func() ([]lndclient.Invoice, error)

	// ListPayments lists all our payments.
	ListPayments func() ([]lndclient.Payment, error)

	// ListForwards lists all our forwards over out relevant period.
	ListForwards func() ([]lndclient.ForwardingEvent, error)

	// DecodePayReq decodes a payment request.
	DecodePayReq decodePaymentRequest

	// OwnPubKey is our node's public key. We use this value to identify
	// payments that are made to our own node.
	OwnPubKey route.Vertex
}

// OnChainConfig contains all the functionality required to produce an on chain
// report.
type OnChainConfig struct {
	CommonConfig

	// OpenChannels provides a list of all currently open channels.
	OpenChannels func() ([]lndclient.ChannelInfo, error)

	// ClosedChannels provides a list of all closed channels.
	ClosedChannels func() ([]lndclient.ClosedChannel, error)

	// PendingChannels provides a list of our pending channels.
	PendingChannels func() (*lndclient.PendingChannels, error)

	// OnChainTransactions provides a list of all on chain transactions
	// relevant to our wallet over a block range.
	OnChainTransactions func() ([]lndclient.Transaction, error)

	// ListSweeps returns the transaction ids of the list of sweeps known
	// to lnd.
	ListSweeps func() ([]string, error)

	// GetFee gets the total fees for a transaction. This function may be
	// nil if we do not have access to a bitcoin backend to lookup fees.
	GetFee getFeeFunc
}

// getFeeFunc is the signature used for functions which can lookup fees for a
// transaction.
type getFeeFunc func(chainhash.Hash) (btcutil.Amount, error)

// CommonConfig contains the items that are common to both types of requests.
type CommonConfig struct {
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

	// PriceSourceCfg is the config to be used for initialising the
	// PriceSource used for fiat related queries.
	PriceSourceCfg *fiat.PriceSourceConfig

	// Categories is a set of custom categories which should be added to the
	// report.
	Categories []CustomCategory
}

// NewOnChainConfig returns an on chain config from the lnd services provided.
// The txLookup function may be nil if a connection to a bitcoin backend is not
// available. If this is the case, the fee report will log warnings indicating
// that fee lookups are not possible in certain cases.
func NewOnChainConfig(ctx context.Context, lnd lndclient.LndServices, startTime,
	endTime time.Time, disableFiat bool, txLookup fees.GetDetailsFunc,
	priceCfg *fiat.PriceSourceConfig,
	categories []CustomCategory) *OnChainConfig {

	var getFee func(chainhash.Hash) (btcutil.Amount, error)
	if txLookup != nil {
		getFee = func(txid chainhash.Hash) (btcutil.Amount, error) {
			return fees.CalculateFee(txLookup, &txid)
		}
	}

	return &OnChainConfig{
		OpenChannels: lndwrap.ListChannels(
			ctx, lnd.Client, false,
		),
		ClosedChannels: func() ([]lndclient.ClosedChannel, error) {
			return lnd.Client.ClosedChannels(ctx)
		},
		PendingChannels: func() (*lndclient.PendingChannels, error) {
			return lnd.Client.PendingChannels(ctx)
		},
		OnChainTransactions: func() ([]lndclient.Transaction, error) {
			return lnd.Client.ListTransactions(ctx, 0, 0)
		},
		ListSweeps: func() ([]string, error) {
			return lnd.WalletKit.ListSweeps(ctx, 0)
		},
		CommonConfig: CommonConfig{
			StartTime:      startTime,
			EndTime:        endTime,
			DisableFiat:    disableFiat,
			Categories:     categories,
			PriceSourceCfg: priceCfg,
		},
		GetFee: getFee,
	}
}

// NewOffChainConfig creates a config for creating off chain reports. It takes
// max parameters which allow control over the pagination size for queries to
// lnd.
func NewOffChainConfig(ctx context.Context, lnd lndclient.LndServices,
	maxInvoices, maxPayments, maxForwards uint64, ownPubkey route.Vertex,
	startTime, endTime time.Time, disableFiat bool,
	priceCfg *fiat.PriceSourceConfig,
	categories []CustomCategory) *OffChainConfig {

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
		DecodePayReq: func(payReq string) (*lndclient.PaymentRequest,
			error) {

			return lnd.Client.DecodePaymentRequest(ctx, payReq)
		},
		OwnPubKey: ownPubkey,
		CommonConfig: CommonConfig{
			StartTime:      startTime,
			EndTime:        endTime,
			DisableFiat:    disableFiat,
			Categories:     categories,
			PriceSourceCfg: priceCfg,
		},
	}
}
