package main

import (
	"fmt"
	"time"

	"github.com/lightninglabs/faraday/frdrpc"
)

// CSVHeaders returns the headers used for harmony csv records.
var CSVHeaders = "Timestamp,OnChain,Type,Category,Amount(Msat),Amount(USD),TxID,Reference,BTCPrice,BTCTimestamp,Note"

// csv returns a csv string of the values contained in a rpc entry. For ease
// of use, the credit field is used to set a negative sign (-) on the amount
// of an entry when it decreases our balance (credit=false).
func csv(e *frdrpc.ReportEntry) string {
	amountPrefix := ""
	if !e.Credit {
		amountPrefix = "-"
	}

	ts := time.Unix(int64(e.Timestamp), 0)

	return fmt.Sprintf("%v,%v,%v,%v,%v%v,%v%v,%v,%v,%v,%v,%v",
		ts, e.OnChain, e.Type, e.CustomCategory, amountPrefix, e.Amount,
		amountPrefix, e.Fiat, e.Txid, e.Reference, e.BtcPrice.Price,
		e.BtcPrice.PriceTimestamp, e.Note)
}
