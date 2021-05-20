package fiat

import (
	"context"
	"time"
)

// customPrices implements the fiatBackend interface.
type customPrices struct {
	entries []*Price
}

// rawPriceData returns the custom price point entries.
func (c *customPrices) rawPriceData(_ context.Context, _,
	_ time.Time) ([]*Price, error) {

	return c.entries, nil
}
