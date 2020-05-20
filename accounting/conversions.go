package accounting

import "github.com/shopspring/decimal"

// msatToFiat is a function which converts a timestamped millisatoshi balance to
// a fiat value.
type msatToFiat func(amount, timestamp int64) (decimal.Decimal, error)

// satsToMsat converts an amount expressed in sats to msat.
func satsToMsat(sats int64) int64 {
	return sats * 1000
}

// satsToMsat converts an amount expressed in sats to msat, flipping the
// sign on the value.
func invertedSatsToMsats(sats int64) int64 {
	return satsToMsat(sats) * -1
}
