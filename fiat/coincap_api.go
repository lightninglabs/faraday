package fiat

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/shopspring/decimal"

	"github.com/lightninglabs/faraday/utils"
)

const (
	// coinCapHistoryAPI is the endpoint we hit for historical price data.
	coinCapHistoryAPI = "https://api.coincap.io/v2/assets/bitcoin/history"

	// coinCapRatesAPI is the endpoint we hit for conversion rates between
	// Bitcoin and various other currencies.
	coinCapRatesAPI = "https://api.coincap.io/v2/rates"
)

var (
	// ErrQueryTooLong is returned when we cannot get a granularity level for a
	// period of time because it is too long.
	ErrQueryTooLong = errors.New("period too long for coincap api, " +
		"please reduce")

	// errCurrencySymbolNotFound is returned when the no conversion rate is
	// found for the given currency symbol.
	errCurrencySymbolNotFound = errors.New("usd rate for symbol not found")
)

// Granularity indicates the level of aggregation price information will be
// provided at.
type Granularity struct {
	// aggregation is the level of aggregation at which prices are provided.
	aggregation time.Duration

	// label is the string that we send to the API to select granularity.
	label string

	// maximumQuery is the maximum time range that prices can be queried
	// at this level of granularity.
	maximumQuery time.Duration
}

func newGranularity(aggregation, maxQuery time.Duration,
	label string) Granularity {

	return Granularity{
		aggregation:  aggregation,
		label:        label,
		maximumQuery: maxQuery,
	}
}

var (
	// GranularityMinute aggregates the bitcoin price over 1 minute.
	GranularityMinute = newGranularity(time.Minute, time.Hour*24, "m1")

	// Granularity5Minute aggregates the bitcoin price over 5 minute.
	Granularity5Minute = newGranularity(
		time.Minute*5, time.Hour*24*5, "m5",
	)

	// Granularity15Minute aggregates the bitcoin price over 15 minutes.
	Granularity15Minute = newGranularity(
		time.Minute*15, time.Hour*24*7, "m15",
	)

	// Granularity30Minute aggregates the bitcoin price over 30 minutes.
	Granularity30Minute = newGranularity(
		time.Minute*30, time.Hour*24*14, "m30",
	)

	// GranularityHour aggregates the bitcoin price over 1 hour.
	GranularityHour = newGranularity(
		time.Hour, time.Hour*24*30, "h1",
	)

	// Granularity6Hour aggregates the bitcoin price over 6 hours.
	Granularity6Hour = newGranularity(
		time.Hour*6, time.Hour*24*183, "h6",
	)

	// Granularity12Hour aggregates the bitcoin price over 12 hours.
	Granularity12Hour = newGranularity(
		time.Hour*12, time.Hour*24*365, "h12",
	)

	// GranularityDay aggregates the bitcoin price over one day.
	GranularityDay = newGranularity(
		time.Hour*24, time.Hour*24*7305, "d1",
	)
)

// ascendingGranularity stores all the levels of granularity that coincap
// allows in ascending order so that we can get the best value for a query
// duration. We require this list because we cannot iterate through maps in
// order.
var ascendingGranularity = []Granularity{
	GranularityMinute, Granularity5Minute, Granularity15Minute,
	Granularity30Minute, GranularityHour, Granularity6Hour,
	Granularity12Hour, GranularityDay,
}

// BestGranularity takes a period of time and returns the lowest granularity
// that we can query the coincap api in a single query. This helper is used
// to provide default granularity periods when they are not provided by
// requests.
func BestGranularity(duration time.Duration) (Granularity, error) {
	for _, granularity := range ascendingGranularity {
		// If our target duration is less than the upper limit for this
		// granularity level, we can use it.
		if duration <= granularity.maximumQuery {
			return granularity, nil
		}
	}

	// If our duration is longer than all maximum query periods, we fail
	// and request a query over a shorter period.
	return Granularity{}, ErrQueryTooLong
}

// coinCapAPI implements the fiatApi interface, getting historical Bitcoin
// prices from coincap.
type coinCapAPI struct {
	// Coincap's api allows us to request prices at varying levels of
	// granularity. This field represents the granularity requested.
	granularity Granularity

	// queryHistory is the function that makes the http call out to
	// coincap's api to fetch price history.
	// It is set within the struct so that it can be mocked for testing.
	queryHistory func(start, end time.Time, g Granularity) ([]byte, error)

	// convert produces fiat prices from the output of the queryHistory function.
	// It is set within the struct so that it can be mocked for testing.
	convert func([]byte) ([]*Price, error)

	// queryUsdRates is the function that makes the http call out to
	// coincap's api fetch conversion rates between USD and various other
	// currencies.
	// It is set within the struct so that it can be mocked for testing.
	queryUsdRates func() ([]byte, error)

	// parseUsdRates takes the output from queryUsdRates, parses the results
	// and returns the exchange rate between USD and the given currency.
	// It is set within the struct so that it can be mocked for testing.
	parseUsdRates func([]byte, string) (decimal.Decimal, error)
}

// newCoinCapAPI returns a coin cap api struct which can be used to query
// historical prices.
func newCoinCapAPI(granularity Granularity) *coinCapAPI {
	return &coinCapAPI{
		granularity:   granularity,
		queryHistory:  queryCoinCap,
		queryUsdRates: queryCoinCapRates,
		convert:       parseCoinCapData,
		parseUsdRates: parseAndFindCoinCapRate,
	}
}

// queryCoinCap returns a function which will httpQuery coincap for historical
// prices.
func queryCoinCap(start, end time.Time, granularity Granularity) ([]byte,
	error) {

	// The coincap api requires milliseconds.
	startMs := start.Unix() * 1000
	endMs := end.Unix() * 1000
	url := fmt.Sprintf("%v?interval=%v&start=%v&end=%v",
		coinCapHistoryAPI, granularity.label, startMs,
		endMs)

	log.Debugf("coincap url: %v", url)

	// Query the http endpoint with the url provided
	// #nosec G107
	response, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()

	return ioutil.ReadAll(response.Body)
}

type coinCapResponse struct {
	Data []*coinCapDataPoint `json:"data"`
}

type coinCapDataPoint struct {
	Price     string `json:"priceUsd"`
	Timestamp int64  `json:"time"`
}

// parseCoinCapData parses http response data to usd price structs, using
// intermediary structs to get around parsing.
func parseCoinCapData(data []byte) ([]*Price, error) {
	var priceEntries coinCapResponse
	if err := json.Unmarshal(data, &priceEntries); err != nil {
		return nil, err
	}

	var usdRecords = make([]*Price, len(priceEntries.Data))

	// Convert each entry from the api to a usable record with a converted
	// time and parsed price.
	for i, entry := range priceEntries.Data {
		decPrice, err := decimal.NewFromString(entry.Price)
		if err != nil {
			return nil, err
		}

		ns := time.Duration(entry.Timestamp) * time.Millisecond
		usdRecords[i] = &Price{
			Timestamp: time.Unix(0, ns.Nanoseconds()),
			Price:     decPrice,
			Currency:  "USD",
		}
	}

	return usdRecords, nil
}

// GetPrices retrieves price information from coincap's api. If the range
// requested is more than coincap will serve us in a single request, we break
// our queries up into multiple chunks.
func (c *coinCapAPI) GetPrices(ctx context.Context, startTime,
	endTime time.Time, currency string) ([]*Price, error) {

	// First, check that we have a valid start and end time, and that the
	// range specified is not in the future.
	if err := utils.ValidateTimeRange(
		startTime, endTime, utils.DisallowFutureRange,
	); err != nil {
		return nil, err
	}

	// When we query prices over a range, it is likely that the first data
	// point we get is after our starting point, since we have discrete
	// points in time. To make sure that the first price point we get is
	// before our starting time, we add a buffer (equal to our granularity
	// level) to our start time so that the first timestamp in our data
	// will definitely be before our start time. We only do this once off,
	// so that we do not have overlapping data across queries.
	startTime = startTime.Add(c.granularity.aggregation * -1)

	var historicalRecords []*Price

	// Create start and end vars to query one maximum length at a time.
	maxPeriod := c.granularity.maximumQuery
	start, end := startTime, startTime.Add(maxPeriod)

	// If the target currency is USD, then avoid the unnecessary API call
	var usdRate decimal.Decimal
	if currency != "USD" {
		rates, err := c.queryUsdRates()
		if err != nil {
			return nil, err
		}

		usdRate, err = c.parseUsdRates(rates, currency)
		if err != nil {
			return nil, err
		}
	}

	// Make chunked queries of size max duration until we reach our end
	// time. We can check equality because we cut our query end back to our
	// target end time if it surpasses it.
	for start.Before(endTime) {
		query := func() ([]byte, error) {
			return c.queryHistory(start, end, c.granularity)
		}

		convert := func(data []byte) ([]*Price, error) {
			usdPrices, err := c.convert(data)
			if err != nil {
				return nil, err
			}

			if currency != "USD" {
				return convertFromUSD(usdPrices, currency, usdRate), nil
			}
			return usdPrices, nil
		}

		// Query the api for this page of data. We allow retries at this
		// stage in case the api experiences a temporary limit.
		records, err := retryQuery(ctx, query, convert)
		if err != nil {
			return nil, err
		}

		historicalRecords = append(historicalRecords, records...)

		// Progress our start time to the end of the period we just
		// queried for, and increase our end time by the maximum
		// queryable period.
		start, end = end, end.Add(maxPeriod)

		// If our end time is after the period we need, we cut it off.
		if end.After(endTime) {
			end = endTime
		}
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

// coinCapRatesResponse holds the list of conversion rates returned by CoinCap.
type coinCapRatesResponse struct {
	Data []*coinCapRatePoint `json:"data"`
}

// coinCapRatePoint holds a currency symbol along with its conversion
// rate to USD.
type coinCapRatePoint struct {
	RateUsd string `json:"rateUsd"`
	Symbol  string `json:"symbol"`
}

// queryCoinCapRates makes the http request to CoinCap to fetch the list of
// currency conversion rates.
func queryCoinCapRates() ([]byte, error) {
	response, err := http.Get(coinCapRatesAPI)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()

	return ioutil.ReadAll(response.Body)
}

// parseAndFindCoinCapRate unmarshals the data returned by queryCoinCapRates
// and filters through it for a data point that matches the given currency
// symbol. If found then the usd conversion rate for that currency is returned.
func parseAndFindCoinCapRate(data []byte, currency string) (decimal.Decimal,
	error) {

	var resp coinCapRatesResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return decimal.Decimal{}, err
	}

	for _, r := range resp.Data {
		if strings.ToUpper(currency) == r.Symbol {
			return decimal.NewFromString(r.RateUsd)
		}
	}

	return decimal.Decimal{}, errCurrencySymbolNotFound
}
