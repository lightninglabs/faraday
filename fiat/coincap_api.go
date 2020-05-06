package fiat

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"sort"
	"strconv"
	"time"

	"github.com/lightninglabs/faraday/utils"
)

const (
	// maxQueries is the total number of queries we allow a call to coincap
	// api to be split up.
	maxQueries = 5

	// coinCapHistoryAPI is the endpoint we hit for historical price data.
	coinCapHistoryAPI = "https://api.coincap.io/v2/assets/bitcoin/history"
)

var (
	errUnknownGranularity = errors.New("unknown level of granularity")

	errPeriodTooLong = errors.New("period too long for " +
		"granularity level")
)

// Granularity indicates the level of aggregation price information will be
// provided at.
type Granularity string

const (
	// GranularityMinute aggregates the bitcoin price over 1 minute.
	GranularityMinute Granularity = "m1"

	// GranularityMinute aggregates the bitcoin price over 1 minute.
	Granularity5Minute Granularity = "m5"

	// GranularityMinute aggregates the bitcoin price over 15 minutes.
	Granularity15Minute Granularity = "m15"

	// GranularityMinute aggregates the bitcoin price over 30 minutes.
	Granularity30Minute Granularity = "m30"

	// GranularityHour aggregates the bitcoin price over 1 hour.
	GranularityHour Granularity = "h1"

	// Granularity6Hour aggregates the bitcoin price over 6 hours.
	Granularity6Hour Granularity = "h6"

	// Granularity12Hour aggregates the bitcoin price over 12h hours.
	Granularity12Hour Granularity = "h12"

	// GranularityDay aggregates the bitcoin price over one day.
	GranularityDay Granularity = "d1"
)

var (
	// maxGranularityPeriod there is a maximum total queryable period for
	// each level of granularity on coincap's api. We record those limits
	// here so that we can size our requests appropriately.
	maxGranularityPeriod = map[Granularity]time.Duration{
		GranularityMinute:   time.Hour * 24,
		Granularity5Minute:  time.Hour * 24 * 5,
		Granularity15Minute: time.Hour * 24 * 7,
		Granularity30Minute: time.Hour * 24 * 14,
		GranularityHour:     time.Hour * 24 * 30,
		Granularity6Hour:    time.Hour * 24 * 183,
		Granularity12Hour:   time.Hour * 24 * 365,
		GranularityDay:      time.Hour * 24 * 7305,
	}

	// minGranularityPeriod maps each granularity to the minimum amount of
	// time you may query coincap's api per granularity level. If you
	// request a period that is shorter than the granularity itself, the
	// api may not return a price point for that period (presumably due to
	// the way they store/calculate their time series).
	minGranularityPeriod = map[Granularity]time.Duration{
		GranularityMinute:   time.Minute,
		Granularity5Minute:  time.Minute * 5,
		Granularity15Minute: time.Minute * 15,
		Granularity30Minute: time.Minute * 30,
		GranularityHour:     time.Hour,
		Granularity6Hour:    time.Hour * 6,
		Granularity12Hour:   time.Hour * 12,
		GranularityDay:      time.Hour * 24,
	}
)

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
	convert func([]byte) ([]*USDPrice, error)
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
		coinCapHistoryAPI, granularity, startMs,
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
func parseCoinCapData(data []byte) ([]*USDPrice, error) {
	var priceEntries coinCapResponse
	if err := json.Unmarshal(data, &priceEntries); err != nil {
		return nil, err
	}

	var usdRecords = make([]*USDPrice, len(priceEntries.Data))

	// Convert each entry from the api to a usable record with a converted
	// time and parsed price.
	for i, entry := range priceEntries.Data {
		floatPrice, err := strconv.ParseFloat(entry.Price, 64)
		if err != nil {
			return nil, err
		}

		usdRecords[i] = &USDPrice{
			timestamp: time.Unix(0, entry.Timestamp),
			price:     floatPrice,
		}
	}

	return usdRecords, nil
}

// GetPrices retrieves price information from coincap's api. If necessary, this
// call splits up the request for data into multiple requests. This is required
// because the more granular we want our price data to be, the smaller the
// period coincap allows us to query is.
func (c *coinCapAPI) GetPrices(ctx context.Context, startTime,
	endTime time.Time) ([]*USDPrice, error) {

	// First, check that we have a valid start and end time, and that the
	// range specified is not in the future.
	if err := utils.ValidateTimeRange(
		startTime, endTime, utils.DisallowFutureRange,
	); err != nil {
		return nil, err
	}

	// Calculate our total range in seconds.
	totalDuration := endTime.Sub(startTime).Seconds()

	// Get the minimum period that we can query at this granularity.
	min, ok := minGranularityPeriod[c.granularity]
	if !ok {
		return nil, errUnknownGranularity
	}

	// If we are beneath minimum period, we shift our start time back by
	// this minimum period. If we do not do this, we will not get any data
	// from the coincap api. We shift start time backwards rather than end
	// time forwards so that we do not accidentally query for times in
	// the future.
	if totalDuration < min.Seconds() {
		startTime = startTime.Add(-1 * min)
		totalDuration = min.Seconds()
	}

	// Get maximum queryable period and ensure that we can obtain all the
	// records within the limit we place on api calls.
	max, ok := maxGranularityPeriod[c.granularity]
	if !ok {
		return nil, errUnknownGranularity
	}

	requiredRequests := totalDuration / max.Seconds()
	if requiredRequests > maxQueries {
		return nil, errPeriodTooLong
	}

	var historicalRecords []*USDPrice
	queryStart := startTime

	// The number of requests we require may be a fraction, so we use a
	// float to ensure that we perform an accurate number of request.
	for i := float64(0); i < requiredRequests; i++ {
		queryEnd := queryStart.Add(max)

		// If the end time is beyond the end time we require, we reduce
		// it. This will only be the case for our last request.
		if queryEnd.After(endTime) {
			queryEnd = endTime
		}

		query := func() ([]byte, error) {
			return c.query(queryStart, queryEnd, c.granularity)
		}

		// Query the api for this page of data. We allow retries at this
		// stage in case the api experiences a temporary limit.
		records, err := retryQuery(ctx, query, c.convert)
		if err != nil {
			return nil, err
		}

		historicalRecords = append(historicalRecords, records...)

		// Progress our start time to our end time.
		queryStart = queryEnd
	}

	// Sort by ascending timestamp.
	sort.SliceStable(historicalRecords, func(i, j int) bool {
		return historicalRecords[i].timestamp.Before(
			historicalRecords[j].timestamp,
		)
	})

	return historicalRecords, nil
}
