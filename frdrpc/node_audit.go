package frdrpc

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"github.com/lightningnetwork/lnd/routing/route"

	"github.com/lightninglabs/faraday/accounting"
	"github.com/lightninglabs/faraday/fees"
)

var (
	// ErrNoCategoryName is returned if a category does not have a name.
	ErrNoCategoryName = errors.New("category must have a name")

	// ErrSetChain is returned when on on/off chain boolean is set for
	// a category
	ErrSetChain = errors.New("category must be for on chain, off chain " +
		"or both")
)

// parseNodeAuditRequest parses a report request and returns the config
// required to produce a report containing on chain and off chain.
func parseNodeAuditRequest(ctx context.Context, cfg *Config,
	req *NodeAuditRequest) (*accounting.OnChainConfig,
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

	fiatBackend, err := fiatBackendFromRPC(req.FiatBackend)
	if err != nil {
		return nil, nil, err
	}

	granularity, err := granularityFromRPC(
		req.Granularity, req.DisableFiat, end.Sub(start),
	)
	if err != nil {
		return nil, nil, err
	}

	pubkey, err := route.NewVertexFromBytes(info.IdentityPubkey[:])
	if err != nil {
		return nil, nil, err
	}

	if err := validateCustomCategories(req.CustomCategories); err != nil {
		return nil, nil, err
	}

	onChainCategories, offChainCategories, err := getCategories(
		req.CustomCategories,
	)
	if err != nil {
		return nil, nil, err
	}

	offChain := accounting.NewOffChainConfig(
		ctx, cfg.Lnd, uint64(maxInvoiceQueries),
		uint64(maxPaymentQueries), uint64(maxForwardQueries),
		pubkey, start, end, req.DisableFiat, fiatBackend, granularity,
		offChainCategories,
	)

	// If we have a chain connection, set our tx lookup function. Otherwise
	// log a warning.
	var feeLookup fees.GetDetailsFunc
	if cfg.BitcoinClient != nil {
		feeLookup = cfg.BitcoinClient.GetTxDetail
	} else {
		log.Warn("creating accounting report without bitcoin " +
			"backend, some fee entries will be missing (see logs)")
	}

	onChain := accounting.NewOnChainConfig(
		ctx, cfg.Lnd, start, end, req.DisableFiat,
		feeLookup, fiatBackend, granularity, onChainCategories,
	)

	return onChain, offChain, nil
}

// validateCustomCategories validates a set of custom categories. It checks that
// each has a name, and at least one bool indicating which transactions to
// classify, as well as checking that each regex provided is unique.
func validateCustomCategories(categories []*CustomCategory) error {
	existing := make(map[string]struct{})

	for _, category := range categories {
		if category.Name == "" {
			return ErrNoCategoryName
		}

		if !category.OffChain && !category.OnChain {
			return ErrSetChain
		}

		for _, regex := range category.LabelPatterns {
			_, ok := existing[regex]
			if ok {
				return fmt.Errorf("duplicate category regex: "+
					"%v", regex)
			}

			existing[regex] = struct{}{}
		}
	}

	return nil
}

func getCategories(categories []*CustomCategory) ([]accounting.CustomCategory,
	[]accounting.CustomCategory, error) {

	var onChainCategories, offChainCategories []accounting.CustomCategory

	for _, category := range categories {
		cust, err := accounting.NewCustomCategory(
			category.Name, category.LabelPatterns,
		)
		if err != nil {
			return nil, nil, err
		}

		if category.OnChain {
			onChainCategories = append(onChainCategories, *cust)
		}

		if category.OffChain {
			offChainCategories = append(offChainCategories, *cust)
		}
	}

	return onChainCategories, offChainCategories, nil
}

func rpcReportResponse(report accounting.Report) (*NodeAuditResponse,
	error) {

	entries := make([]*ReportEntry, len(report))

	for i, entry := range report {
		rpcEntry := &ReportEntry{
			Timestamp:      uint64(entry.Timestamp.Unix()),
			OnChain:        entry.OnChain,
			CustomCategory: entry.Category,
			Amount:         uint64(entry.Amount),
			Credit:         entry.Credit,
			Asset:          "BTC",
			Txid:           entry.TxID,
			Fiat:           entry.FiatValue.String(),
			Reference:      entry.Reference,
			Note:           entry.Note,
			BtcPrice: &BitcoinPrice{
				Price:    entry.BTCPrice.Price.String(),
				Currency: entry.BTCPrice.Currency,
			},
		}

		if !entry.BTCPrice.Timestamp.IsZero() {
			rpcEntry.BtcPrice.PriceTimestamp = uint64(
				entry.BTCPrice.Timestamp.Unix(),
			)
		}

		rpcType, err := rpcEntryType(entry.Type)
		if err != nil {
			return nil, err
		}
		rpcEntry.Type = rpcType

		entries[i] = rpcEntry
	}

	// Sort report entries by timestamp.
	sort.SliceStable(entries, func(i, j int) bool {
		return entries[i].Timestamp < entries[j].Timestamp
	})

	return &NodeAuditResponse{Reports: entries}, nil
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
