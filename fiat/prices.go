package fiat

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/lightninglabs/faraday/utils"
	"github.com/lightningnetwork/lnd/lnwire"
	"github.com/shopspring/decimal"
)

var (
	errNoPrices            = errors.New("no price data provided")
	errUnknownPriceBackend = errors.New("unknown price backend")
	errPriceOutOfRange     = errors.New("timestamp before beginning of price " +
		"dataset")

	// errCoincapGranularityRequired is returned when a request is made
	// for the coincap API but the granularity of those prices is not set.
	errCoincapGranularityRequired = errors.New("granularity required " +
		"fiat prices are enabled")

	errGranularityUnexpected = errors.New("granularity unexpect for " +
		"default price backend")

	errPricePointsRequired = errors.New("at least one price point " +
		"required for a custom price backend")

	errPriceSourceConfigExpected = errors.New("a non-nil " +
		"PriceSourceConfig is expected")

	errGranularityUnsupported = errors.New("api does not support " +
		"requested granularity")
)

// fiatBackend is an interface that must be implemented by any backend that
// is used to fetch fiat price information.
type fiatBackend interface {
	rawPriceData(ctx context.Context, startTime,
		endTime time.Time) ([]*Price, error)
}

// PriceSource holds a fiatBackend that can be used to fetch fiat price
// information.
type PriceSource struct {
	impl fiatBackend
}

// PriceSourceConfig is a struct holding various config options used for
// initialising a new PriceSource.
type PriceSourceConfig struct {
	// Backend is the PriceBackend to be used for fetching price data.
	Backend PriceBackend

	// Granularity specifies the level of granularity with which we want to
	// get fiat prices. This option is only used for the CoinCap
	// PriceBackend.
	Granularity *Granularity

	// PricePoints is a set of price points that is used for fiat related
	// queries if the PriceBackend being used is the CustomPriceBackend.
	PricePoints []*Price
}

// validatePriceSourceConfig checks that the PriceSourceConfig fields are valid
// given the chosen price backend.
func (cfg *PriceSourceConfig) validatePriceSourceConfig() error {
	switch cfg.Backend {
	// If an unknown price backend is set, we don't expect granularity to
	// be set. The caller should provide a backend if they wish to use
	// a specific granularity.
	case UnknownPriceBackend:
		if cfg.Granularity != nil {
			return errGranularityUnexpected
		}

	case CoinCapPriceBackend:
		if cfg.Granularity == nil {
			return errCoincapGranularityRequired
		}

	// CoinDesk only supports daily price granularity, fail if another
	// value is set but allow setting to the correct value.
	case CoinDeskPriceBackend:
		if cfg.Granularity != nil &&
			*cfg.Granularity != GranularityDay {

			return fmt.Errorf("%w: coindesk only provides daily "+
				"price granularity", errGranularityUnsupported)
		}

	case CoinGeckoPriceBackend:
		if cfg.Granularity != nil {
			return fmt.Errorf("%w: coingecko automatically "+
				"provides hourly price granularity for the "+
				"last 90 days and daily price granularity for "+
				"dates older than that",
				errGranularityUnsupported)
		}

	case CustomPriceBackend:
		if len(cfg.PricePoints) == 0 {
			return errPricePointsRequired
		}
	}

	return nil
}

// GetPrices fetches price information using the given the PriceSource
// fiatBackend implementation. GetPrices also validates the time parameters and
// sorts the results.
func (p PriceSource) GetPrices(ctx context.Context, startTime,
	endTime time.Time) ([]*Price, error) {

	// First, check that we have a valid start and end time, and that the
	// range specified is not in the future.
	if err := utils.ValidateTimeRange(
		startTime, endTime, utils.DisallowFutureRange,
	); err != nil {
		return nil, err
	}

	historicalRecords, err := p.impl.rawPriceData(ctx, startTime, endTime)
	if err != nil {
		return nil, err
	}

	// Sort by ascending timestamp once we have all of our records. We
	// expect these records to already be sorted, but we do not trust our
	// external source to do so (just in case).
	sort.SliceStable(historicalRecords, func(i, j int) bool {
		return historicalRecords[i].Timestamp.Before(
			historicalRecords[j].Timestamp,
		)
	})

	return historicalRecords, nil
}

// PriceBackend is an enum that indicates which backend we are using for fiat
// information.
type PriceBackend uint8

const (
	// UnknownPriceBackend is used to indicate that no specific backend
	// was specified for fiat price data and that the defaults should
	// instead be used.
	UnknownPriceBackend PriceBackend = iota

	// CoinCapPriceBackend uses CoinCap's API for fiat price data.
	CoinCapPriceBackend

	// CoinDeskPriceBackend uses CoinDesk's API for fiat price data.
	CoinDeskPriceBackend

	// CustomPriceBackend uses user provided fiat price data.
	CustomPriceBackend

	// CoinGeckoPriceBackend uses CoinGecko's API for fiat price data.
	CoinGeckoPriceBackend
)

var priceBackendNames = map[PriceBackend]string{
	UnknownPriceBackend:   "unknown",
	CoinCapPriceBackend:   "coincap",
	CoinDeskPriceBackend:  "coindesk",
	CustomPriceBackend:    "custom",
	CoinGeckoPriceBackend: "coingecko",
}

// String returns the string representation of a price backend.
func (p PriceBackend) String() string {
	return priceBackendNames[p]
}

// NewPriceSource returns a PriceSource which can be used to query price
// data.
func NewPriceSource(cfg *PriceSourceConfig) (*PriceSource, error) {
	if cfg == nil {
		return nil, errPriceSourceConfigExpected
	}

	if err := cfg.validatePriceSourceConfig(); err != nil {
		return nil, err
	}

	switch cfg.Backend {
	// We expect granularity to be set for coincap.
	case CoinCapPriceBackend:
		return &PriceSource{
			impl: newCoinCapAPI(*cfg.Granularity),
		}, nil

	// Default to coindesk api.
	case UnknownPriceBackend, CoinDeskPriceBackend:
		return &PriceSource{
			impl: &coinDeskAPI{},
		}, nil

	case CustomPriceBackend:
		return &PriceSource{
			impl: &customPrices{
				entries: cfg.PricePoints,
			},
		}, nil

	case CoinGeckoPriceBackend:
		return &PriceSource{
			impl: &coinGeckoAPI{},
		}, nil
	}

	return nil, errUnknownPriceBackend
}

// PriceRequest describes a request for price information.
type PriceRequest struct {
	// Identifier uniquely identifies the request.
	Identifier string

	// Value is the amount of BTC in msat.
	Value lnwire.MilliSatoshi

	// Timestamp is the time at which the price should be obtained.
	Timestamp time.Time
}

// GetPrices gets a set of prices for a set of timestamps.
func GetPrices(ctx context.Context, timestamps []time.Time,
	priceCfg *PriceSourceConfig) (
	map[time.Time]*Price, error) {

	if len(timestamps) == 0 {
		return nil, nil
	}

	log.Debugf("getting prices for: %v requests", len(timestamps))

	// Sort our timestamps in ascending order so that we can get the start
	// and end period we need.
	sort.SliceStable(timestamps, func(i, j int) bool {
		return timestamps[i].Before(timestamps[j])
	})

	// Get the earliest and latest timestamps we can, these may be the same
	// timestamp if we have 1 entry, but that's ok.
	start, end := timestamps[0], timestamps[len(timestamps)-1]

	client, err := NewPriceSource(priceCfg)
	if err != nil {
		return nil, err
	}

	priceData, err := client.GetPrices(ctx, start, end)
	if err != nil {
		return nil, err
	}

	// Prices will map transaction timestamps to their fiat prices.
	var prices = make(map[time.Time]*Price, len(timestamps))

	for _, ts := range timestamps {
		price, err := GetPrice(priceData, ts)
		if err != nil {
			return nil, err
		}

		prices[ts] = price
	}

	return prices, nil
}

// MsatToFiat converts a msat amount to fiat. Note that this function converts
// values to Bitcoin values, then gets the fiat price for that BTC value.
func MsatToFiat(price decimal.Decimal, amt lnwire.MilliSatoshi) decimal.Decimal {
	msatDecimal := decimal.NewFromInt(int64(amt))

	// We are quoted price per whole bitcoin. We need to scale this price
	// down to price per msat - 1 BTC * 10000000 sats * 1000 msats.
	pricePerMSat := price.Div(decimal.NewFromInt(100000000000))

	return pricePerMSat.Mul(msatDecimal)
}

// GetPrice gets the price for a given time from a set of price data. This
// function expects the price data to be sorted with ascending timestamps and
// for first timestamp in the price data to be before any timestamp we are
// querying. The last datapoint's timestamp may be before the timestamp we are
// querying. If a request lies between two price points, we just return the
// earlier price.
func GetPrice(prices []*Price, timestamp time.Time) (*Price, error) {
	if len(prices) == 0 {
		return nil, errNoPrices
	}

	var lastPrice *Price

	// Run through our prices until we find a timestamp that our price
	// point lies before. Since we always return the previous price, this
	// also works for timestamps that are exactly equal (at the cost of a
	// single extra iteration of this loop).
	for _, price := range prices {
		if timestamp.Before(price.Timestamp) {
			break
		}

		lastPrice = price
	}

	// If we have broken our loop without setting the value of our last
	// price, we have a timestamp that is before the first entry in our
	// series. We expect our range of price points to start before any
	// timestamps we query, so we fail.
	if lastPrice == nil {
		return nil, errPriceOutOfRange
	}

	// Otherwise, we return the last price that was before (or equal to)
	// our timestamp.
	return lastPrice, nil
}
