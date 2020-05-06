package fiat

import (
	"context"
	"errors"
	"time"
)

const (
	// maxRetries is the maximum number of retries we allow per call to an
	// api.
	maxRetries = 3

	// retrySleep is the period we backoff for between tries, set to 0.5
	// second.
	retrySleep = time.Millisecond * 500
)

var (
	errShuttingDown  = errors.New("shutting down")
	errRetriesFailed = errors.New("could not get data within max retries")
)

// USDPrice represents the Bitcoin price in USD at a certain time.
type USDPrice struct {
	timestamp time.Time
	price     float64
}

// retryQuery calls an api until it succeeds, or we hit our maximum retries.
// It sleeps between calls and can be terminated early by cancelling the
// context passed in. It takes query and convert functions as parameters for
// testing purposes.
func retryQuery(ctx context.Context, queryAPI func() ([]byte, error),
	convert func([]byte) ([]*USDPrice, error)) ([]*USDPrice, error) {

	for i := 0; i < maxRetries; i++ {
		// If our request fails, log the error, sleep for the retry
		// period and then continue so we can try again.
		response, err := queryAPI()
		if err != nil {
			log.Errorf("http get attempt: %v failed: %v", i, err)

			select {
			case <-time.After(retrySleep):
			case <-ctx.Done():
				return nil, errShuttingDown
			}

			continue
		}

		return convert(response)
	}

	return nil, errRetriesFailed
}
