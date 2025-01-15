package frdrpcserver

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/btcsuite/btcd/chaincfg/chainhash"
	"github.com/lightninglabs/faraday/accounting"
	"github.com/lightninglabs/faraday/fees"
	"github.com/lightninglabs/faraday/fiat"
	"github.com/lightninglabs/faraday/frdrpc"
	"github.com/lightninglabs/lndclient"
	"github.com/lightningnetwork/lnd/routing/route"
	"github.com/shopspring/decimal"
)

// Since Bitcoin blocks are not guaranteed to be completely ordered
// by timestamp, and the timestamps can be manipulated by miners within a
// certain range, we will apply a buffer on the time range which we use to
// find start and end block heights. This should ensure we widen the block
// height range enough to fetch all relevant transactions within a time range.
const blockTimeRangeBuffer = time.Hour * 24

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
	req *frdrpc.NodeAuditRequest) (*accounting.OnChainConfig,
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

	priceSourceCfg, err := priceCfgFromRPC(
		req.FiatBackend, req.Granularity, false, start, end,
		req.CustomPrices,
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
		pubkey, start, end, req.DisableFiat, priceSourceCfg,
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

	var blockRangeLookup func(start, end time.Time) (uint32, uint32, error)

	// If a time range is set, we will use a block height lookup function
	// to find the block heights for the start and end time.
	timeRangeSet := req.StartTime > 0 || req.EndTime > 0
	if timeRangeSet {
		blockRangeLookup = func(start, end time.Time) (uint32, uint32, error) {
			return resolveBlockHeightRange(
				ctx, cfg.Lnd, info.BlockHeight, start, end,
			)
		}
	}

	onChain := accounting.NewOnChainConfig(
		ctx, cfg.Lnd, start, end, blockRangeLookup, req.DisableFiat,
		feeLookup, priceSourceCfg, onChainCategories,
	)

	return onChain, offChain, nil
}

// validateCustomCategories validates a set of custom categories. It checks that
// each has a name, and at least one bool indicating which transactions to
// classify, as well as checking that each regex provided is unique.
func validateCustomCategories(categories []*frdrpc.CustomCategory) error {
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

func pricePointsFromRPC(prices []*frdrpc.BitcoinPrice) ([]*fiat.Price, error) {
	res := make([]*fiat.Price, len(prices))

	for i, p := range prices {
		price, err := decimal.NewFromString(p.Price)
		if err != nil {
			return nil, err
		}

		res[i] = &fiat.Price{
			Timestamp: time.Unix(int64(p.PriceTimestamp), 0),
			Price:     price,
			Currency:  p.Currency,
		}
	}

	return res, nil
}

// validateCustomPricePoints checks that there is at lease one price point
// in the set before the given start time.
func validateCustomPricePoints(prices []*fiat.Price,
	startTime time.Time) error {

	for _, price := range prices {
		if price.Timestamp.Before(startTime) {
			return nil
		}
	}

	return errors.New("expected at least one price point with a " +
		"timestamp preceding the given start time")
}

func getCategories(
	categories []*frdrpc.CustomCategory) ([]accounting.CustomCategory,
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

func rpcReportResponse(report accounting.Report) (*frdrpc.NodeAuditResponse,
	error) {

	entries := make([]*frdrpc.ReportEntry, len(report))

	for i, entry := range report {
		rpcEntry := &frdrpc.ReportEntry{
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
			BtcPrice: &frdrpc.BitcoinPrice{
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

	return &frdrpc.NodeAuditResponse{Reports: entries}, nil
}

func rpcEntryType(t accounting.EntryType) (frdrpc.EntryType, error) {
	switch t {
	case accounting.EntryTypeLocalChannelOpen:
		return frdrpc.EntryType_LOCAL_CHANNEL_OPEN, nil

	case accounting.EntryTypeRemoteChannelOpen:
		return frdrpc.EntryType_REMOTE_CHANNEL_OPEN, nil

	case accounting.EntryTypeChannelOpenFee:
		return frdrpc.EntryType_CHANNEL_OPEN_FEE, nil

	case accounting.EntryTypeChannelClose:
		return frdrpc.EntryType_CHANNEL_CLOSE, nil

	case accounting.EntryTypeReceipt:
		return frdrpc.EntryType_RECEIPT, nil

	case accounting.EntryTypePayment:
		return frdrpc.EntryType_PAYMENT, nil

	case accounting.EntryTypeFee:
		return frdrpc.EntryType_FEE, nil

	case accounting.EntryTypeCircularReceipt:
		return frdrpc.EntryType_CIRCULAR_RECEIPT, nil

	case accounting.EntryTypeForward:
		return frdrpc.EntryType_FORWARD, nil

	case accounting.EntryTypeForwardFee:
		return frdrpc.EntryType_FORWARD_FEE, nil

	case accounting.EntryTypeCircularPayment:
		return frdrpc.EntryType_CIRCULAR_PAYMENT, nil

	case accounting.EntryTypeCircularPaymentFee:
		return frdrpc.EntryType_CIRCULAR_FEE, nil

	case accounting.EntryTypeSweep:
		return frdrpc.EntryType_SWEEP, nil

	case accounting.EntryTypeSweepFee:
		return frdrpc.EntryType_SWEEP_FEE, nil

	case accounting.EntryTypeChannelCloseFee:
		return frdrpc.EntryType_CHANNEL_CLOSE_FEE, nil

	default:
		return 0, fmt.Errorf("unknown entrytype: %v", t)
	}
}

// resolveBlockHeightRange determines the block height range that should be
// used for in queries based on the start and end time of the report.
// The function will apply a buffer to ensure the block height range is
// too large rather than too small, so that all relevant transactions are
// fetched from the backend.
func resolveBlockHeightRange(ctx context.Context,
	lndClient lndclient.LndServices, latestHeight uint32,
	startTime, endTime time.Time) (uint32, uint32, error) {

	// Apply a buffer on the start time which we use to find the block height.
	// This should ensure we use a low enough height to fetch all relevant
	// transactions following the start time.
	bufferedStartTime := startTime.Add(-blockTimeRangeBuffer)

	if bufferedStartTime.Before(time.Unix(0, 0)) {
		bufferedStartTime = time.Unix(0, 0)
	}

	startHeight, err := findFirstBlockBeforeTimestamp(
		ctx, lndClient, latestHeight, bufferedStartTime,
	)
	if err != nil {
		return 0, 0, err
	}

	// Apply a buffer on the end time which we use to find the block height.
	// This should ensure we use a high enough height to fetch all relevant
	// transactions up to the end time.
	bufferedEndTime := endTime.Add(blockTimeRangeBuffer)

	endHeight, err := findFirstBlockBeforeTimestamp(
		ctx, lndClient, latestHeight, bufferedEndTime,
	)
	if err != nil {
		return 0, 0, err
	}

	if startHeight > endHeight {
		log.Errorf("Start height: %v is greater than end height: %v, "+
			"setting both to 0", startHeight, endHeight)

		// If startHeight somehow ended up being greater than endHeight,
		// set both start and end height to 0, meaning we will query for
		// all onchain history.
		startHeight = 0
		endHeight = 0
	}

	return startHeight, endHeight, nil
}

// findFirstBlockBeforeTimestamp finds the block height from just before the
// given timestamp.
func findFirstBlockBeforeTimestamp(ctx context.Context,
	lndClient lndclient.LndServices, latestHeight uint32,
	targetTime time.Time) (uint32, error) {

	targetTimestamp := targetTime.Unix()

	// Set the search range to the genesis block and the latest block.
	low := uint32(0)
	high := latestHeight

	// Perform binary search to find the block height that is just before the
	// target timestamp.
	for low <= high {
		mid := (low + high) / 2

		// Lookup the block in the middle of the search range.
		blockHash, err := getBlockHash(ctx, lndClient, mid)
		if err != nil {
			return 0, err
		}

		blockTime, err := getBlockTimestamp(ctx, lndClient, blockHash)
		if err != nil {
			return 0, err
		}

		blockTimestamp := blockTime.Unix()
		if blockTimestamp < targetTimestamp {
			// If the block we looked up is before the target timestamp,
			// we set the new low height to the next block after that.
			low = mid + 1
		} else if blockTimestamp > targetTimestamp {
			// If the block we looked up is after the target timestamp,
			// we set the new high height to the block before that.
			high = mid - 1
		} else {
			// If we find an exact match of block timestamp and target
			// timestamp, ruturn the height of this block.
			return mid, nil
		}
	}

	log.Debugf("Binary search done for targetTimestamp: %v. "+
		"Returning height: %v", targetTimestamp, high)

	// Closest block before the timestamp.
	return high, nil
}

// getBlockHash retrieves the block hash for a given height.
func getBlockHash(ctx context.Context, lndClient lndclient.LndServices,
	height uint32) (chainhash.Hash, error) {

	blockHash, err := lndClient.ChainKit.GetBlockHash(ctx, int64(height))
	if err != nil {
		return chainhash.Hash{}, err
	}

	return blockHash, nil
}

// getBlockTimestamp retrieves the block timestamp for a given block hash.
func getBlockTimestamp(ctx context.Context,
	lndClient lndclient.LndServices, hash chainhash.Hash) (time.Time, error) {

	blockHeader, err := lndClient.ChainKit.GetBlockHeader(ctx, hash)
	if err != nil {
		return time.Time{}, err
	}

	return blockHeader.Timestamp, nil
}
