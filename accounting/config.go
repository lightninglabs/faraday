package accounting

import (
	"time"

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

	// StartTime is the time from which the report should be created,
	// inclusive.
	StartTime time.Time

	// EndTime is the time until which the report should be created,
	// exclusive.
	EndTime time.Time
}
