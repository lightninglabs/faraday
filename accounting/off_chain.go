package accounting

import (
	"context"
	"encoding/hex"
	"errors"
	"time"

	"github.com/lightninglabs/faraday/fiat"
	"github.com/lightningnetwork/lnd/lnrpc"
)

var (
	// errNoHops is returned when we see a payment which has htlcs with no
	// hops in its route.
	errNoHops = errors.New("payment htlc has a route with zero hops")

	// errDifferentDuplicates is returned if we have payments with duplicate
	// payment hashes where one is made to our own node and one is made to
	// another node. This is unexpected because legacy duplicate payments in
	// lnd reflect multiple attempts to pay the same invoice.
	errDifferentDuplicates = errors.New("duplicate payments paid to " +
		"different sources")

	// errDuplicatesNotSupported is returned when we see payments with
	// duplicate payment hashes. This was allowed in legacy versions of lnd,
	// but is not supported for accounting purposes. Nodes with duplicates
	// will be required to delete the duplicates or query over a range that
	// excludes them.
	errDuplicatesNotSupported = errors.New("duplicate payments not " +
		"supported, query more recent timestamp to exclude duplicates")
)

// OffChainConfig contains all the functionality required to produce an off
// chain report.
type OffChainConfig struct {
	// ListInvoices lists all our invoices.
	ListInvoices func() ([]*lnrpc.Invoice, error)

	// ListPayments lists all our payments.
	ListPayments func() ([]*lnrpc.Payment, error)

	// ListForwards lists all our forwards over out relevant period.
	ListForwards func() ([]*lnrpc.ForwardingEvent, error)

	// OwnPubKey is our node's public key. We use this value to identify
	// payments that are made to our own node.
	OwnPubKey string

	// StartTime is the time from which the report should be created,
	// inclusive.
	StartTime time.Time

	// EndTime is the time until which the report should be created,
	// exclusive.
	EndTime time.Time

	// Granularity is the level of granularity we require for our price
	// estimates.
	Granularity fiat.Granularity
}

// OffChainReport gets a report of off chain activity using live price data.
func OffChainReport(ctx context.Context, cfg *OffChainConfig) (Report, error) {
	getPrice, err := getConversion(
		ctx, cfg.StartTime, cfg.EndTime, cfg.Granularity,
	)
	if err != nil {
		return nil, err
	}

	return offChainReportWithPrices(cfg, getPrice)
}

// offChainReportWithPrices produces off chain reports using the getPrice
// function provided. This allows testing of our report creation without calling
// the actual price API.
func offChainReportWithPrices(cfg *OffChainConfig, getPrice msatToFiat) (Report,
	error) {

	invoices, err := cfg.ListInvoices()
	if err != nil {
		return nil, err
	}
	filteredInvoices := filterInvoices(cfg.StartTime, cfg.EndTime, invoices)

	payments, err := cfg.ListPayments()
	if err != nil {
		return nil, err
	}

	// Get a list of all the payments we made to ourselves.
	paymentsToSelf, err := getCircularPayments(cfg.OwnPubKey, payments)
	if err != nil {
		return nil, err
	}

	filteredPayments := filterPayments(cfg.StartTime, cfg.EndTime, payments)
	if err := sanityCheckDuplicates(filteredPayments); err != nil {
		return nil, err
	}

	// Get all our forwards, we do not need to filter them because they
	// are already supplied over the relevant range for our query.
	forwards, err := cfg.ListForwards()
	if err != nil {
		return nil, err
	}

	return offChainReport(
		filteredInvoices, filteredPayments, paymentsToSelf, forwards,
		getPrice,
	)
}

// offChainReport produces an off chain transaction report. This function
// assumes that all entries passed into this function fall within our target
// date range, with the exception of payments to self which tracks payments
// that were made to ourselves for the sake of appropriately reporting the
// invoices they paid.

func offChainReport(invoices []*lnrpc.Invoice, payments []settledPayment,
	circularPayments map[string]bool, forwards []*lnrpc.ForwardingEvent,
	convert msatToFiat) (Report, error) {

	var reports Report

	for _, invoice := range invoices {
		hash := hex.EncodeToString(invoice.RHash)

		// If the invoice's payment hash is in our set of circular
		// payments, we know that this payment was made to ourselves.
		toSelf := circularPayments[hash]

		entry, err := invoiceEntry(invoice, toSelf, convert)
		if err != nil {
			return nil, err
		}

		reports = append(reports, entry)
	}

	for _, payment := range payments {
		// If the payment's payment request is in our set of circular
		// payments, we know that this payment was made to ourselves.
		toSelf := circularPayments[payment.PaymentHash]

		entries, err := paymentEntry(payment, toSelf, convert)
		if err != nil {
			return nil, err
		}

		reports = append(reports, entries...)
	}

	for _, forward := range forwards {
		entries, err := forwardingEntry(forward, convert)
		if err != nil {
			return nil, err
		}

		reports = append(reports, entries...)
	}

	return reports, nil
}

// getCircularPayments returns a map of the payments that we made to our node.
// Note that this function does only account for settled payments because it
// is possible that we made a payment to ourselves, settled the invoice and
// queried listPayments while the payment was still being settled back. We
// rather examine their htlcs, since we will check whether they are settled in
// our relevant period at a later stage.
//
// To allow for legacy nodes that have payments with duplicate payment hashes,
// we allow for payments with duplicate payment hashes. We only fail if we
// detect payments with the same payment hash where one is to our node and one
// is not. This would make lookup in our circular payment map wrong for one of
// the payments (resulting in bugs) and is not expected, because duplicate
// payments are expected to reflect multiple attempts of the same payment.
func getCircularPayments(ourPubkey string,
	payments []*lnrpc.Payment) (map[string]bool, error) {

	// Run through all payments and get those that were made to our own
	// node. We identify these payments by payment hash so that we can
	// identify associated invoices.
	paymentsToSelf := make(map[string]bool)

	for _, payment := range payments {
		// If our payment has no htlc attempts, it has not yet been sent
		// our by our node. This payment therefore cannot be a payment
		// to ourselves within this accounting period; if we are paying
		// a regular invoice, it will not be settled yet, and if we are
		// making a keysend, the invoice will not exist in our node yet.
		if len(payment.Htlcs) == 0 {
			continue
		}

		// Since all htlcs go to the same node, we only need to get the
		// destination of our first htlc to determine whether it's our
		// own node. We expect the route this htlc took to have at least
		// one hop, and fail if it does not.
		hops := payment.Htlcs[0].Route.Hops
		if len(hops) == 0 {
			return nil, errNoHops
		}

		lastHop := hops[len(hops)-1]
		toSelf := lastHop.PubKey == ourPubkey

		// Before we add our entry to the map, we sanity check that if
		// it has any duplicates, the value in the map is the same as
		// the value we are about to add.
		duplicateToSelf, ok := paymentsToSelf[payment.PaymentHash]
		if ok && duplicateToSelf != toSelf {
			return nil, errDifferentDuplicates
		}

		if toSelf {
			paymentsToSelf[payment.PaymentHash] = toSelf
		}
	}

	return paymentsToSelf, nil
}

// sanityCheckDuplicates checks that we have no payments with duplicate payment
// hashes. We do not support accounting for duplicate payments.
func sanityCheckDuplicates(payments []settledPayment) error {
	uniqueHashes := make(map[string]bool, len(payments))

	for _, payment := range payments {
		_, ok := uniqueHashes[payment.PaymentHash]
		if ok {
			return errDuplicatesNotSupported
		}

		uniqueHashes[payment.PaymentHash] = true
	}

	return nil
}
