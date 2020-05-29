package accounting

import (
	"context"
	"time"

	"github.com/lightninglabs/faraday/fiat"
	"github.com/lightningnetwork/lnd/lnrpc"
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

	// PaidSelf checks the invoice that we paid and returns true if we paid
	// ourselves. This indicates that the payment was part of a circular
	// rebalance.
	PaidSelf func(string) (bool, error)

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

func OffChainReport(ctx context.Context, cfg *OffChainConfig) (Report, error) {
	invoices, err := cfg.ListInvoices()
	if err != nil {
		return nil, err
	}
	filteredInvoices := filterInvoices(cfg.StartTime, cfg.EndTime, invoices)

	payments, err := cfg.ListPayments()
	if err != nil {
		return nil, err
	}

	// Run through all payments and get those that were made to our own
	// node. We identify these payments by payment request so that we can
	// identify associated invoices.
	paymentsToSelf := make(map[string]bool)
	for _, payment := range payments {
		toSelf, err := cfg.PaidSelf(payment.PaymentRequest)
		if err != nil {
			return nil, err
		}

		if toSelf {
			paymentsToSelf[payment.PaymentRequest] = true
		}
	}

	// Get all our forwards, we do not need to filter them because they
	// are already supplied over the relevant range for our query.
	forwards, err := cfg.ListForwards()
	if err != nil {
		return nil, err
	}

	getPrice, err := getConversion(
		ctx, cfg.StartTime, cfg.EndTime, cfg.Granularity,
	)
	if err != nil {
		return nil, err
	}

	return offChainReport(
		filteredInvoices, paymentsToSelf, forwards, getPrice,
	)
}

// offChainReport produces an off chain transaction report. This function
// assumes that all entries passed into this function fall within our target
// date range, with the exception of payments to self which tracks payments
// that were made to ourselves for the sake of appropriately reporting the
// invoices they paid.
func offChainReport(invoices []*lnrpc.Invoice, circularPayments map[string]bool,
	forwards []*lnrpc.ForwardingEvent, convert msatToFiat) (Report, error) {

	var reports Report

	for _, invoice := range invoices {
		// If the invoice's payment request is in our set of circular
		// payments, we know that this payment was made to ourselves.
		toSelf := circularPayments[invoice.PaymentRequest]

		entry, err := invoiceEntry(invoice, toSelf, convert)
		if err != nil {
			return nil, err
		}

		reports = append(reports, entry)
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
