// Package lndwrap wraps various calls to lndclient for convenience. It offers
// wrapping for paginated queries that will obtain all entries from a desired
// index onwards.
package lndwrap

import (
	"context"
	"fmt"
	"time"

	"github.com/lightninglabs/faraday/paginater"
	"github.com/lightninglabs/lndclient"
)

// ListInvoices makes paginated calls to lnd to get our full set of
// invoices.
func ListInvoices(ctx context.Context, startOffset, maxInvoices uint64,
	lnd lndclient.LightningClient) ([]lndclient.Invoice, error) {

	var invoices []lndclient.Invoice

	query := func(offset, maxInvoices uint64) (uint64, uint64, error) {
		resp, err := lnd.ListInvoices(
			ctx, lndclient.ListInvoicesRequest{
				Offset:      offset,
				MaxInvoices: maxInvoices,
			},
		)
		if err != nil {
			return 0, 0, err
		}

		invoices = append(invoices, resp.Invoices...)

		return resp.LastIndexOffset, uint64(len(resp.Invoices)), nil
	}

	// Make paginated calls to the invoices API, starting at offset 0 and
	// querying our max number of invoices each time.
	if err := paginater.QueryPaginated(
		ctx, query, startOffset, maxInvoices,
	); err != nil {
		return nil, fmt.Errorf("ListInvoices failed: %w", err)
	}

	return invoices, nil
}

// ListPayments makes a set of paginated calls to lnd to get our full set
// of payments.
func ListPayments(ctx context.Context, startOffset, maxPayments uint64,
	lnd lndclient.LightningClient) ([]lndclient.Payment, error) {

	var payments []lndclient.Payment

	query := func(offset, maxEvents uint64) (uint64, uint64, error) {
		resp, err := lnd.ListPayments(
			ctx, lndclient.ListPaymentsRequest{
				Offset:      offset,
				MaxPayments: maxEvents,
			},
		)
		if err != nil {
			return 0, 0, err
		}

		payments = append(payments, resp.Payments...)

		return resp.LastIndexOffset, uint64(len(resp.Payments)), nil
	}

	// Make paginated calls to the payments API, starting at offset 0 and
	// querying our max number of payments each time.
	if err := paginater.QueryPaginated(
		ctx, query, startOffset, maxPayments,
	); err != nil {
		return nil, fmt.Errorf("ListPayments failed: %w", err)
	}

	return payments, nil
}

// ListForwards makes paginated calls to our forwarding events api.
func ListForwards(ctx context.Context, maxForwards uint64, startTime,
	endTime time.Time, lnd lndclient.LightningClient) (
	[]lndclient.ForwardingEvent, error) {

	var forwards []lndclient.ForwardingEvent

	query := func(offset, maxEvents uint64) (uint64, uint64, error) {
		resp, err := lnd.ForwardingHistory(
			ctx, lndclient.ForwardingHistoryRequest{
				StartTime: startTime,
				EndTime:   endTime,
				Offset:    uint32(offset),
				MaxEvents: uint32(maxEvents),
			},
		)
		if err != nil {
			return 0, 0, err
		}

		forwards = append(forwards, resp.Events...)

		return uint64(resp.LastIndexOffset),
			uint64(len(resp.Events)), nil
	}

	// Make paginated calls to the forwards API, starting at offset 0 and
	// querying our max number of payments each time.
	if err := paginater.QueryPaginated(
		ctx, query, 0, maxForwards,
	); err != nil {
		return nil, fmt.Errorf("ListForwards failed: %w", err)
	}

	return forwards, nil
}

// ListChannels wraps the listchannels call to lnd, with a publicOnly bool
// that can be used to toggle whether private channels are included.
func ListChannels(ctx context.Context, lnd lndclient.LightningClient,
	publicOnly bool) func() ([]lndclient.ChannelInfo, error) {

	return func() ([]lndclient.ChannelInfo, error) {
		resp, err := lnd.ListChannels(ctx, false, publicOnly)
		if err != nil {
			return nil, fmt.Errorf("ListChannels failed: %w", err)
		}

		return resp, nil
	}
}
