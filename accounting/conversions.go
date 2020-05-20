package accounting

import "github.com/shopspring/decimal"

// msatToFiat is a function which converts a timestamped millisatoshi balance to
// a fiat value.
type msatToFiat func(amount, timestamp int64) (decimal.Decimal, error)
