package fiat

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"sort"
	"time"

	"github.com/lightninglabs/faraday/utils"
	"github.com/shopspring/decimal"
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

	// ErrQueryTooLong is returned when we cannot get a granularity level
	// for a period of time because it is too long.
	ErrQueryTooLong = errors.New("period too long for coincap api, " +
		"please reduce")
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

// maxDuration returns the maximum duration that can be queried for a given
// level of granularity.
func (g Granularity) maxDuration() (time.Duration, error) {
	switch g {
	case GranularityMinute:
		return time.Hour * 24, nil

	case Granularity5Minute:
		return time.Hour * 24 * 5, nil

	case Granularity15Minute:
		return time.Hour * 24 * 7, nil

	case Granularity30Minute:
		return time.Hour * 24 * 14, nil

	case GranularityHour:
		return time.Hour * 24 * 30, nil

	case Granularity6Hour:
		return time.Hour * 24 * 183, nil

	case Granularity12Hour:
		return time.Hour * 24 * 365, nil

	case GranularityDay:
		return time.Hour * 24 * 7305, nil

	default:
		return 0, errUnknownGranularity
	}
}

// minDuration returns the minimum duration that can be queried for a given
// level of granularity.
func (g Granularity) minDuration() (time.Duration, error) {
	switch g {
	case GranularityMinute:
		return time.Minute, nil

	case Granularity5Minute:
		return time.Minute * 5, nil

	case Granularity15Minute:
		return time.Minute * 15, nil

	case Granularity30Minute:
		return time.Minute * 30, nil

	case GranularityHour:
		return time.Hour, nil

	case Granularity6Hour:
		return time.Hour * 6, nil

	case Granularity12Hour:
		return time.Hour * 12, nil

	case GranularityDay:
		return time.Hour * 24, nil

	default:
		return 0, errUnknownGranularity
	}
}

// ascendingGranularity stores all the levels of granularity that coincap
// allows in ascending order so that we can get the best value for a query
// duration. We require this list because we cannot iterate through maps in
// order.
var ascendingGranularity = []Granularity{
	GranularityMinute, Granularity5Minute, Granularity15Minute,
	Granularity30Minute, GranularityHour, Granularity6Hour,
	Granularity12Hour, GranularityDay,
}

// maxSplitDuration returns the total amount of time we can query the coincap
// api at a given granularity level, given that we split our query up into
// parts.
func (g Granularity) maxSplitDuration() (time.Duration, error) {
	maxDuration, err := g.maxDuration()
	if err != nil {
		return 0, err
	}

	return maxDuration * maxQueries, nil
}

// BestGranularity takes a period of time and returns the lowest granularity
// that we can query the coincap api for taking into account that we allow
// splitting up of queries into 5 parts to get more accurate price information.
// If the period of time can't be catered for, we return an error.
func BestGranularity(duration time.Duration) (Granularity, error) {
	for _, granularity := range ascendingGranularity {
		// Get the total amount of time we can query for at this level
		// of granularity.
		period, err := granularity.maxSplitDuration()
		if err != nil {
			return "", err
		}

		// If our target duration is less than the upper limit for this
		// granularity level, we can use it.
		if duration <= period {
			return granularity, nil
		}
	}

	// If our duration is longer than all maximum query periods, we fail
	// and request a query over a shorter period.
	return "", ErrQueryTooLong
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
		decPrice, err := decimal.NewFromString(entry.Price)
		if err != nil {
			return nil, err
		}

		usdRecords[i] = &USDPrice{
			timestamp: time.Unix(0, entry.Timestamp),
			price:     decPrice,
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
	min, err := c.granularity.minDuration()
	if err != nil {
		return nil, err
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
	max, err := c.granularity.maxDuration()
	if err != nil {
		return nil, err
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
