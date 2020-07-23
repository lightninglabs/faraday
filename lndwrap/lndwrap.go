// Package lndwrap wraps various calls to lndclient for convenience. It offers
// wrapping for paginated queries that will obtain all entries from a desired
// index onwards.
package lndwrap

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/btcsuite/btcd/wire"
	"github.com/btcsuite/btcutil"
	"github.com/lightninglabs/faraday/paginater"
	"github.com/lightninglabs/lndclient"
	"github.com/lightningnetwork/lnd/chainntnfs"
)

// ErrNotificationTimeout is returned when a spend notification from lnd does
// not return in time.
var ErrNotificationTimeout = errors.New("spend notification did not arrive")

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
		return nil, err
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
		return nil, err
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
		return nil, err
	}

	return forwards, nil
}

// ListChannels wraps the listchannels call to lnd, with a publicOnly bool
// that can be used to toggle whether private channels are included.
func ListChannels(ctx context.Context, lnd lndclient.LightningClient,
	publicOnly bool) func() ([]lndclient.ChannelInfo, error) {

	return func() ([]lndclient.ChannelInfo, error) {
		resp, err := lnd.ListChannels(ctx)
		if err != nil {
			return nil, err
		}

		// If we want all channels, we can just return now.
		if !publicOnly {
			return resp, err
		}

		// If we only want public channels, we skip over all private
		// channels and return a list of public only.
		var publicChannels []lndclient.ChannelInfo
		for _, channel := range resp {
			if channel.Private {
				continue
			}

			publicChannels = append(publicChannels, channel)
		}

		return publicChannels, nil
	}
}

// registerSpendNtfnFunc is the function signature of a function which provides
// us with spend notifications.
type registerSpendNtfnFunc func(ctx context.Context,
	outpoint *wire.OutPoint, pkScript []byte, heightHint int32) (
	chan *chainntnfs.SpendDetail, chan error, error)

// GetTransactionFee returns a function which gets the fees paid for a
// transaction by looking up each of its inputs using lnd's spend notification
// api. This is a workaround that allows us to get our fees without connecting
// to a bitcoin node (which would also require txindex).
func GetTransactionFee(ctx context.Context, registerSpend registerSpendNtfnFunc,
	timeout time.Duration) func(tx *wire.MsgTx) (btcutil.Amount, error) {

	return func(tx *wire.MsgTx) (btcutil.Amount, error) {
		var fee btcutil.Amount
		for _, txout := range tx.TxOut {
			fee -= btcutil.Amount(txout.Value)
		}

		inputTotal, err := getInputTotal(
			ctx, registerSpend, timeout, tx.TxIn,
		)
		if err != nil {
			return 0, err
		}

		fee += inputTotal

		return fee, nil
	}
}

// getInputTotal takes a set of transaction inputs and registers spend
// notifications for previous outpoints. This allows us to get the value of the
// inputs (from the previous outpoints) without needing a connection to a
// bitcoin backend, or transaction indexing. This function will *only* work for
// inputs of already confirmed transactions, because registering spends for
// unconfirmed inputs will not return. Since we expect register spend to return
// reasonably soon, we fail with a timeout if we do not get a spend notification
// in time. We dispatch our spend notification registration and consume their
// results in goroutines so that we do not need to wait for each respective
// registration to return.
func getInputTotal(ctx context.Context, registerSpend registerSpendNtfnFunc,
	timeout time.Duration, inputs []*wire.TxIn) (btcutil.Amount, error) {

	var (
		// Create channels that will consume values or errors for each
		// of our inputs.
		valueChan = make(chan btcutil.Amount)
		errorChan = make(chan error)

		// Add a wait group that we will use to track each goroutine we
		// dispatch to lookup a txin value.
		wg sync.WaitGroup
	)

	// Create a done channel that we will close to indicate that we no
	// longer want to get values for our inputs. This channel will be closed
	// in the case where one of our lookups has failed so we want to exit
	// early.
	done := make(chan struct{})

	// Create a helper function which will send into our values channel
	// without blocking if the consumer cancels.
	sendValue := func(value int64) {
		select {
		case valueChan <- btcutil.Amount(value):
		case <-done:
		}
	}

	// Create a helper which will send errors in to our error channel
	// without blocking if the consumer cancels.
	sendErr := func(err error) {
		select {
		case errorChan <- err:
		case <-done:
		}
	}

	for _, txin := range inputs {
		// Create an in-scope variable for our outpoint.
		targetOutpoint := txin.PreviousOutPoint

		// Spin up a goroutine which will register a spend notification
		// and wait for a response.
		wg.Add(1)
		go func() {
			defer wg.Done()

			// If we have not already looked up this transaction's
			// spend, we register a spend notification.
			spendChan, spendErr, err := registerSpend(
				ctx, &targetOutpoint, nil, 0,
			)
			if err != nil {
				sendErr(err)
				return
			}

			// Wait for a spend notification (which we expect
			// because the outpoint has been spent), then find our
			// target input in the transaction's outputs and return
			// its amount.
			select {
			case spend := <-spendChan:
				output := spend.SpendingTx.TxOut[targetOutpoint.Index]
				sendValue(output.Value)

			// If our spend notification fails, we return the error.
			case err := <-spendErr:
				sendErr(err)

			// If the done channel is closed, the consuming function
			// is no longer interested in receiving anything from us
			// so we exit without any return value.
			case <-done:

			// If we timeout, just send an error.
			case <-time.After(timeout):
				sendErr(ErrNotificationTimeout)
			}
		}()
	}

	// Dispatch a goroutine which will wait for all of our values to be
	// looked up, then close the values channel to indicate that we are done
	// providing values. We do not need to wait group this function, because
	// the for-select below will only break once it has executed.
	go func() {
		wg.Wait()
		close(valueChan)
		close(errorChan)
	}()

	// Finally, we want to collect the total value of our inputs. We allow
	// this function to block, because we want to consume everything from
	// our channels before we exit.
	var (
		feeTotal btcutil.Amount
		err      error
	)

consume:
	for {
		select {
		// If we receive a value, we increment our total. If the fee
		// channel is closed, there are no more values to consume so
		// we exit.
		case fee, ok := <-valueChan:
			if !ok {
				break consume
			}
			feeTotal += fee

		// If we receive an error, we can exit early because one of our
		// lookups has failed. We close the done channel so that all of
		// our other goroutines can exit and then break our consumer
		// loop.
		case err = <-errorChan:
			close(done)
			break consume
		}
	}

	if err != nil {
		return 0, err
	}

	return feeTotal, nil
}
