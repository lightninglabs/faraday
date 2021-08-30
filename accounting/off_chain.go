package accounting

import (
	"bytes"
	"context"
	"errors"

	"github.com/lightninglabs/lndclient"
	"github.com/lightningnetwork/lnd/lntypes"
	"github.com/lightningnetwork/lnd/routing/route"
)

var (
	// errNoHops is returned when we see a payment which has htlcs with no
	// hops in its route.
	errNoHops = errors.New("payment htlc has a route with zero hops")

	// errNoHtlcs is returned when we encounter a legacy payment which is
	// settled but has no htlcs recorded with it.
	errNoHtlcs = errors.New("settled payment has no htlcs")

	// errNoPaymentRequest is returned when a payment does not have a
	// payment request.
	errNoPaymentRequest = errors.New("no payment request present")

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

// OffChainReport gets a report of off chain activity using live price data.
func OffChainReport(ctx context.Context, cfg *OffChainConfig) (Report, error) {
	// Retrieve a function which can be used to query individual prices,
	// or a no-op function if we do not want prices.
	getPrice, err := getConversion(
		ctx, cfg.StartTime, cfg.EndTime, cfg.DisableFiat,
		cfg.PriceSourceCfg,
	)
	if err != nil {
		return nil, err
	}

	return offChainReportWithPrices(cfg, getPrice)
}

// offChainReportWithPrices produces off chain reports using the getPrice
// function provided. This allows testing of our report creation without calling
// the actual price API.
func offChainReportWithPrices(cfg *OffChainConfig, getPrice fiatPrice) (Report,
	error) {

	invoices, err := cfg.ListInvoices()
	if err != nil {
		return nil, err
	}
	filteredInvoices := filterInvoices(cfg.StartTime, cfg.EndTime, invoices)

	log.Infof("Retrieved: %v invoices, %v filtered", len(invoices),
		len(filteredInvoices))

	payments, err := cfg.ListPayments()
	if err != nil {
		return nil, err
	}

	preProcessed, err := preProcessPayments(payments, cfg.DecodePayReq)
	if err != nil {
		return nil, err
	}

	// Get a list of all the payments we made to ourselves.
	paymentsToSelf, err := getCircularPayments(cfg.OwnPubKey, preProcessed)
	if err != nil {
		return nil, err
	}

	filteredPayments := filterPayments(
		cfg.StartTime, cfg.EndTime, preProcessed,
	)
	if err := sanityCheckDuplicates(filteredPayments); err != nil {
		return nil, err
	}

	log.Infof("Retrieved: %v payments, %v filtered, %v circular",
		len(payments), len(filteredPayments), len(paymentsToSelf))

	// Get all our forwards, we do not need to filter them because they
	// are already supplied over the relevant range for our query.
	forwards, err := cfg.ListForwards()
	if err != nil {
		return nil, err
	}

	log.Infof("Retrieved: %v forwards", len(forwards))

	u := entryUtils{
		getFiat:          getPrice,
		customCategories: cfg.Categories,
	}

	return offChainReport(
		filteredInvoices, filteredPayments, paymentsToSelf, forwards,
		u,
	)
}

// offChainReport produces an off chain transaction report. This function
// assumes that all entries passed into this function fall within our target
// date range, with the exception of payments to self which tracks payments
// that were made to ourselves for the sake of appropriately reporting the
// invoices they paid.
func offChainReport(invoices []lndclient.Invoice, payments []paymentInfo,
	circularPayments map[string]bool, forwards []lndclient.ForwardingEvent,
	utils entryUtils) (Report, error) {

	var reports Report

	for _, invoice := range invoices {
		// If the invoice's payment hash is in our set of circular
		// payments, we know that this payment was made to ourselves.
		toSelf := circularPayments[invoice.Hash.String()]

		entry, err := invoiceEntry(invoice, toSelf, utils)
		if err != nil {
			return nil, err
		}

		reports = append(reports, entry)
	}

	for _, payment := range payments {
		// If the payment's payment request is in our set of circular
		// payments, we know that this payment was made to ourselves.
		toSelf := circularPayments[payment.Hash.String()]

		entries, err := paymentEntry(payment, toSelf, utils)
		if err != nil {
			return nil, err
		}

		reports = append(reports, entries...)
	}

	for _, forward := range forwards {
		entries, err := forwardingEntry(forward, utils)
		if err != nil {
			return nil, err
		}

		reports = append(reports, entries...)
	}

	return reports, nil
}

// getCircularPayments returns a map of the payments that we made to our node.
// Note that this function does not only account for settled payments because it
// is possible that we made a payment to ourselves, settled the invoice and
// queried listPayments while the payment was still being settled back. If a
// payment does not have a record of its payment hash (which is the case for
// legacy payments, and payments that have just been dispatched), we log a
// warning but do not fail so that legacy nodes can still use this feature (they
// may just not detect old circular rebalances, which we document).
//
// To allow for legacy nodes that have payments with duplicate payment hashes,
// we allow for payments with duplicate payment hashes. We only fail if we
// detect payments with the same payment hash where one is to our node and one
// is not. This would make lookup in our circular payment map wrong for one of
// the payments (resulting in bugs) and is not expected, because duplicate
// payments are expected to reflect multiple attempts of the same payment.
func getCircularPayments(ourPubkey route.Vertex,
	payments []paymentInfo) (map[string]bool, error) {

	// Run through all payments and get those that were made to our own
	// node. We identify these payments by payment hash so that we can
	// identify associated invoices.
	paymentsToSelf := make(map[string]bool)

	for _, payment := range payments {
		// Try to determine whether a payment is made to our own
		// node by checking its htlcs. If we cannot get this information
		// from our set of htlcs, we fallback to trying to decode our
		// payment request. If the payment request is not present as
		// well, we skip over this payment and log a warning (because
		// legacy nodes will always have these payment present).
		if payment.destination == nil {
			log.Warnf("payment %v destination unknown",
				payment.Hash)

			continue
		}

		// Check whether the payment is made to our own node.
		toSelf := bytes.Equal(ourPubkey[:], payment.destination[:])

		// Before we add our entry to the map, we sanity check that if
		// it has any duplicates, the value in the map is the same as
		// the value we are about to add.
		duplicateToSelf, ok := paymentsToSelf[payment.Hash.String()]
		if ok && duplicateToSelf != toSelf {
			return nil, errDifferentDuplicates
		}

		if toSelf {
			paymentsToSelf[payment.Hash.String()] = toSelf
		}
	}

	return paymentsToSelf, nil
}

// sanityCheckDuplicates checks that we have no payments with duplicate payment
// hashes. We do not support accounting for duplicate payments.
func sanityCheckDuplicates(payments []paymentInfo) error {
	uniqueHashes := make(map[lntypes.Hash]bool, len(payments))

	for _, payment := range payments {
		_, ok := uniqueHashes[payment.Hash]
		if ok {
			return errDuplicatesNotSupported
		}

		uniqueHashes[payment.Hash] = true
	}

	return nil
}
