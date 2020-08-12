package frdrpc

import (
	"context"
	"fmt"

	"github.com/lightninglabs/faraday/accounting"
	"github.com/lightningnetwork/lnd/routing/route"
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

	// We lookup our pubkey once so that our paid to self function does
	// not need to do a lookup for every payment it checks.
	info, err := cfg.Lnd.Client.GetInfo(ctx)
	if err != nil {
		return nil, nil, err
	}

	granularity, err := granularityFromRPC(req.Granularity, end.Sub(start))
	if err != nil {
		return nil, nil, err
	}

	pubkey, err := route.NewVertexFromBytes(info.IdentityPubkey[:])
	if err != nil {
		return nil, nil, err
	}

	offChain := accounting.NewOffChainConfig(
		ctx, cfg.Lnd, uint64(maxInvoiceQueries),
		uint64(maxPaymentQueries), uint64(maxForwardQueries),
		pubkey, start, end, req.DisableFiat, granularity,
	)

	onChain := accounting.NewOnChainConfig(
		ctx, cfg.Lnd, start, end, req.DisableFiat,
		cfg.BitcoinClient.GetTxDetail, granularity,
	)

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
			BtcPrice: &BitcoinPrice{
				Price:          entry.BTCPrice.Price.String(),
				PriceTimestamp: uint64(entry.BTCPrice.Timestamp.Unix()),
			},
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

	case accounting.EntryTypeSweep:
		return EntryType_SWEEP, nil

	case accounting.EntryTypeSweepFee:
		return EntryType_SWEEP_FEE, nil

	case accounting.EntryTypeChannelCloseFee:
		return EntryType_CHANNEL_CLOSE_FEE, nil

	default:
		return 0, fmt.Errorf("unknown entrytype: %v", t)
	}
}
