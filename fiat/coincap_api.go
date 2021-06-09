package fiat

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"time"

	"github.com/shopspring/decimal"
)

const (
	// coinCapHistoryAPI is the endpoint we hit for historical price data.
	coinCapHistoryAPI = "https://api.coincap.io/v2/assets/bitcoin/history"

	// coinCapDefaultCurrency is the currency that the price data returned
	// by the Coin Cap API is quoted in.
	coinCapDefaultCurrency = "USD"
)

// ErrQueryTooLong is returned when we cannot get a granularity level for a
// period of time because it is too long.
var ErrQueryTooLong = errors.New("period too long for coincap api, " +
	"please reduce")

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

	// query is the function that makes the http call out to coincap's api.
	// It is set within the struct so that it can be mocked for testing.
	query func(start, end time.Time, g Granularity) ([]byte, error)

	// convert produces usd prices from the output of the query function.
	// It is set within the struct so that it can be mocked for testing.
	convert func([]byte) ([]*Price, error)
}

// newCoinCapAPI returns a coin cap api struct which can be used to query
// historical prices.
func newCoinCapAPI(granularity Granularity) *coinCapAPI {
	return &coinCapAPI{
		granularity: granularity,
		query:       queryCoinCap,
		convert:     parseCoinCapData,
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

// parseCoinCapData parses http response data to usc price structs, using
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
			Currency:  coinCapDefaultCurrency,
		}
	}

	return usdRecords, nil
}

// rawPriceData retrieves price information from coincap's api. If the range
// requested is more than coincap will serve us in a single request, we break
// our queries up into multiple chunks.
func (c *coinCapAPI) rawPriceData(ctx context.Context, startTime,
	endTime time.Time) ([]*Price, error) {

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

	// Make chunked queries of size max duration until we reach our end
	// time. We can check equality because we cut our query end back to our
	// target end time if it surpasses it.
	for start.Before(endTime) {
		query := func() ([]byte, error) {
			return c.query(start, end, c.granularity)
		}

		// Query the api for this page of data. We allow retries at this
		// stage in case the api experiences a temporary limit.
		records, err := retryQuery(ctx, query, c.convert)
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

	return historicalRecords, nil
}
