package frdrpc

import (
	"context"
	"encoding/hex"
	"fmt"

	"github.com/lightninglabs/faraday/accounting"
	"github.com/lightninglabs/loop/lndclient"
	"github.com/lightningnetwork/lnd/lnrpc"
)

// parseNodeReportRequest parses a report request and returns the config
// required to produce a report containing on chain and off chain.
func parseNodeReportRequest(ctx context.Context, cfg *Config,
	req *NodeReportRequest) (*accounting.OnChainConfig,
	*accounting.OffChainConfig, error) {

	start, end, err := validateTimes(req.StartTime, req.EndTime)
	if err != nil {
		return nil, nil, err
	}

	granularity, err := granularityFromRPC(req.Granularity)
	if err != nil {
		return nil, nil, err
	}

	onChain := &accounting.OnChainConfig{
		OpenChannels: cfg.wrapListChannels(ctx, false),
		ClosedChannels: func() ([]lndclient.ClosedChannel, error) {
			return cfg.Lnd.Client.ClosedChannels(ctx)
		},
		OnChainTransactions: cfg.wrapGetChainTransactions(ctx),
		StartTime:           start,
		EndTime:             end,
		Granularity:         granularity,
	}

	// We lookup our pubkey once so that our paid to self function does
	// not need to do a lookup for every payment it checks.
	info, err := cfg.Lnd.Client.GetInfo(ctx)
	if err != nil {
		return nil, nil, err
	}

	offChain := &accounting.OffChainConfig{
		ListInvoices: func() ([]lndclient.Invoice, error) {
			return cfg.wrapListInvoices(ctx)
		},
		ListPayments: func() ([]*lnrpc.Payment, error) {
			return cfg.wrapListPayments(ctx)
		},
		ListForwards: func() ([]lndclient.ForwardingEvent, error) {
			return cfg.wrapListForwards(ctx, start, end)
		},
		OwnPubKey:   hex.EncodeToString(info.IdentityPubkey[:]),
		StartTime:   start,
		EndTime:     end,
		Granularity: granularity,
	}

	return onChain, offChain, nil
}

func rpcReportResponse(report accounting.Report) (*NodeReportResponse,
	error) {

	entries := make([]*ReportEntry, len(report))

	for i, entry := range report {
		rpcEntry := &ReportEntry{
			Timestamp: uint64(entry.Timestamp.Unix()),
			OnChain:   entry.OnChain,
			Amount:    uint64(entry.Amount),
			Credit:    entry.Credit,
			Asset:     "BTC",
			Txid:      entry.TxID,
			Fiat:      entry.FiatValue.String(),
			Reference: entry.Reference,
			Note:      entry.Note,
		}

		rpcType, err := rpcEntryType(entry.Type)
		if err != nil {
			return nil, err
		}
		rpcEntry.Type = rpcType

		entries[i] = rpcEntry
	}

	return &NodeReportResponse{Reports: entries}, nil
}

func rpcEntryType(t accounting.EntryType) (EntryType, error) {
	switch t {
	case accounting.EntryTypeLocalChannelOpen:
		return EntryType_LOCAL_CHANNEL_OPEN, nil

	case accounting.EntryTypeRemoteChannelOpen:
		return EntryType_REMOTE_CHANNEL_OPEN, nil

	case accounting.EntryTypeChannelOpenFee:
		return EntryType_CHANNEL_OPEN_FEE, nil

	case accounting.EntryTypeChannelClose:
		return EntryType_CHANNEL_CLOSE, nil

	case accounting.EntryTypeReceipt:
		return EntryType_RECEIPT, nil

	case accounting.EntryTypePayment:
		return EntryType_PAYMENT, nil

	case accounting.EntryTypeFee:
		return EntryType_FEE, nil

	case accounting.EntryTypeCircularReceipt:
		return EntryType_CIRCULAR_RECEIPT, nil

	case accounting.EntryTypeForward:
		return EntryType_FORWARD, nil

	case accounting.EntryTypeForwardFee:
		return EntryType_FORWARD_FEE, nil

	case accounting.EntryTypeCircularPayment:
		return EntryType_CIRCULAR_PAYMENT, nil

	case accounting.EntryTypeCircularPaymentFee:
		return EntryType_CIRCULAR_FEE, nil

	default:
		return 0, fmt.Errorf("unknown entrytype: %v", t)
	}
}
