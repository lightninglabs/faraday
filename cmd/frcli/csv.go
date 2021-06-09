package main

import (
	"encoding/csv"
	"errors"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/lightninglabs/faraday/frdrpc"
)

// CSVHeaders returns the headers used for harmony csv records.
var CSVHeaders = "Timestamp,OnChain,Type,Category,Amount(Msat),Amount(%v),TxID,Reference,BTCPrice,BTCTimestamp,Note"

// writeToCSV returns a csv string of the values contained in a rpc entry. For ease
// of use, the credit field is used to set a negative sign (-) on the amount
// of an entry when it decreases our balance (credit=false).
func writeToCSV(e *frdrpc.ReportEntry) string {
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

// parsePricesFromCSV reads price point data from the csv at the specified path.
// This function expects the first csv line to be headers and expects the rest
// of the lines to be tuples of the following format:
// 'unix tx seconds, price of 1 BTC in chosen currency'.
func parsePricesFromCSV(path, currency string) ([]*frdrpc.BitcoinPrice, error) {
	if path == "" || currency == "" {
		return nil, errors.New("custom price csv path and " +
			"currency must both be specified")
	}
	csvFile, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer csvFile.Close()

	csvLines, err := csv.NewReader(csvFile).ReadAll()
	if err != nil {
		return nil, err
	}

	if len(csvLines) < 2 {
		return nil, errors.New("no price points found in CSV")
	}

	// Skip the first line in the CSV file since we expect this line
	// to contain column headers.
	csvLines = csvLines[1:]

	prices := make([]*frdrpc.BitcoinPrice, len(csvLines))

	for i, line := range csvLines {
		if len(line) != 2 {
			return nil, errors.New("incorrect csv format. " +
				"Two columns items are expected per row")
		}

		timestamp, err := strconv.ParseInt(line[0], 10, 64)
		if err != nil {
			return nil, err
		}

		prices[i] = &frdrpc.BitcoinPrice{
			PriceTimestamp: uint64(timestamp),
			Price:          line[1],
			Currency:       currency,
		}
	}

	return prices, nil
}
