package fiat

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"time"

	"github.com/shopspring/decimal"
)

const (
	// coinDeskHistoryAPI is the endpoint we hit for historical price data.
	coinDeskHistoryAPI = "https://api.coindesk.com/v1/bpi/historical/close.json"

	// coinDeskTimeFormat is the date format used by coindesk.
	coinDeskTimeFormat = "2006-01-02"

	// coinDeskDefaultCurrency is the default currency that the price data
	// returned by the Coin Desk API is quoted in.
	coinDeskDefaultCurrency = "USD"
)

// coinDeskAPI implements the fiatBackend interface.
type coinDeskAPI struct{}

type coinDeskResponse struct {
	Data map[string]float64 `json:"bpi"`
}

// queryCoinDesk constructs and sends a request to coindesk to query historical
// price information.
func queryCoinDesk(start, end time.Time) ([]byte, error) {
	queryURL := fmt.Sprintf("%v?start=%v&end=%v",
		coinDeskHistoryAPI, start.Format(coinDeskTimeFormat),
		end.Format(coinDeskTimeFormat))

	log.Debugf("coindesk url: %v", queryURL)

	// Query the http endpoint with the url provided
	// #nosec G107
	response, err := http.Get(queryURL)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()

	return ioutil.ReadAll(response.Body)
}

// parseCoinDeskData parses http response data from coindesk into Price
// structs.
func parseCoinDeskData(data []byte) ([]*Price, error) {
	var priceEntries coinDeskResponse
	if err := json.Unmarshal(data, &priceEntries); err != nil {
		return nil, err
	}

	var usdRecords = make([]*Price, 0, len(priceEntries.Data))

	for date, price := range priceEntries.Data {
		timestamp, err := time.Parse(coinDeskTimeFormat, date)
		if err != nil {
			return nil, err
		}

		usdRecords = append(usdRecords, &Price{
			Timestamp: timestamp,
			Price:     decimal.NewFromFloat(price),
			Currency:  coinDeskDefaultCurrency,
		})
	}

	return usdRecords, nil
}

// rawPriceData retrieves price information from coindesks's api for the given
// time range.
func (c *coinDeskAPI) rawPriceData(ctx context.Context, start,
	end time.Time) ([]*Price, error) {

	query := func() ([]byte, error) {
		return queryCoinDesk(start, end)
	}

	// CoinDesk uses a granularity of 1 day and does not include the current
	// day's price information. So subtract 1 period from the start date so
	// that at least one day's price data is always included.
	start = start.Add(time.Hour * -24)

	// Query the api for this page of data. We allow retries at this
	// stage in case the api experiences a temporary limit.
	records, err := retryQuery(ctx, query, parseCoinDeskData)
	if err != nil {
		return nil, err
	}

	return records, nil
}
