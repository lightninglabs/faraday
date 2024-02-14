package accounting

import (
	"errors"
	"fmt"
	"time"

	"github.com/lightninglabs/lndclient"
	invoicespkg "github.com/lightningnetwork/lnd/invoices"
	"github.com/lightningnetwork/lnd/lnrpc"
	"github.com/lightningnetwork/lnd/routing/route"
)

// ErrReceiveWithFee is returned if we get an on chain receive with a fee, which
// we do not expect.
var ErrReceiveWithFee = errors.New("on chain receive with non-zero fee")

// inRange returns a boolean that indicates whether a timestamp lies in a
// range with an inclusive start time and exclusive end time.
func inRange(timestamp, startTime, endTime time.Time) bool {
	// Our start time is inclusive, skip any transactions that are
	// strictly before our start time.
	if timestamp.Before(startTime) {
		return false
	}

	// Our end time is exclusive, so we skip any transactions that
	// are after or equal to our end time.
	if !timestamp.Before(endTime) {
		return false
	}

	return true
}

// filterOnChain filters a set of on chain transactions to get only those
// which lie within [startTime, endTime). Unconfirmed transactions are also
// excluded from this set.
func filterOnChain(startTime, endTime time.Time,
	txns []lndclient.Transaction) ([]lndclient.Transaction, error) {

	// nolint: prealloc
	var filtered []lndclient.Transaction

	for _, tx := range txns {
		// Unconfirmed transactions are listed with 0 confirmations,
		// they have no timestamp, so we set a current one.
		//
		// TODO(guggero): Find out why the channel force close sweep
		// doesn't show as confirmed in itests since updating to lnd
		// v0.15.4-beta.
		if tx.Confirmations == 0 {
			tx.Timestamp = time.Now()
		}

		if !inRange(tx.Timestamp, startTime, endTime) {
			continue
		}

		// Account for double counting of our fees in transaction
		// amounts. We do not skip over zero value transactions here
		// because it is possible for us to have channel closes where
		// we are paid out zero (in the case of our own force close),
		// but we still need to account for fees.
		switch {
		// Fees are included in the total amount for sends from our
		// wallet. Since our amount is expressed as a negative value,
		// we just add our positive fee amount to prevent double
		// counting.
		case tx.Amount < 0:
			tx.Amount += tx.Fee

		// We do not expect to have fees for on chain receives, fail if
		// we encounter them.
		case tx.Amount > 0:
			if tx.Fee != 0 {
				return nil, ErrReceiveWithFee
			}
		}

		filtered = append(filtered, tx)
	}

	return filtered, nil
}

// filterInvoices filters out unsettled invoices and those that are outside of
// our desired time range.
func filterInvoices(startTime, endTime time.Time,
	invoices []lndclient.Invoice) []lndclient.Invoice {

	// nolint: prealloc
	var filtered []lndclient.Invoice

	for _, invoice := range invoices {
		// If the invoice was not settled, we do not need to create an
		// entry for it.
		if invoice.State != invoicespkg.ContractSettled {
			continue
		}

		if !inRange(invoice.SettleDate, startTime, endTime) {
			continue
		}

		filtered = append(filtered, invoice)
	}

	return filtered
}

// paymentInfo wraps a lndclient payment struct with a destination, and
// description if available from the information we have available, and its
// settle time. Since we now allow multi-path payments, a single payment may
// have multiple htlcs resolved over a period of time. We use the most recent
// settle time for payment because payments are not considered settled until
// all the htlcs are resolved.
type paymentInfo struct {
	lndclient.Payment
	destination *route.Vertex
	description *string
	settleTime  time.Time
}

// preProcessPayments takes a list of payments and gets their destination and
// settled time from their htlcs and payment request. We use the last hop in our
// stored payment attempt to identify payments to our own nodes because this
// information is readily available for the most recent payments db migration,
// and we do not need to query lnd to decode payment requests. Further, payment
// requests are not stored for keysend payments and payments that were created
// by directly specifying the amount and destination. We fallback to payment
// requests in the case where we do not have htlcs present.
func preProcessPayments(payments []lndclient.Payment,
	decode decodePaymentRequest) ([]paymentInfo, error) {

	paymentList := make([]paymentInfo, len(payments))

	for i, payment := range payments {
		// Attempt to obtain the payment destination and description
		// from our payment request. If this is not possible (which
		// can be the case for legacy payments that did not store
		// payment requests, or payments that pay directly to a
		// payment hash), then try to get it from our HTLCs. Note
		// that HTLCs may also not be available for legacy payments
		// that did not store HTLCs. In the event that we get a
		// destination from both sources, we prefer the destination
		// from the HTLCs.
		payReqDestination, description, err := paymentRequestDetails(
			payment.PaymentRequest, decode,
		)
		if err != nil && err != errNoPaymentRequest {
			return nil, err
		}

		destination, err := paymentHtlcDestination(payment)
		if err != nil {
			destination = payReqDestination
		}

		pmt := paymentInfo{
			Payment:     payment,
			destination: destination,
			description: description,
		}

		// If the payment did not succeed, we can add it to our list
		// with its current zero settle time and continue.
		if payment.Status.State != lnrpc.Payment_SUCCEEDED {
			paymentList[i] = pmt
			continue
		}

		// We run through each htlc for this payment and get the latest
		// resolution time for a successful htlc. This is the time we
		// will use to determine whether this payment lies in the period
		// we are looking at.
		var latestTimeNs int64
		for _, htlc := range payment.Htlcs {
			if htlc.Status != lnrpc.HTLCAttempt_SUCCEEDED {
				continue
			}

			if htlc.ResolveTimeNs > latestTimeNs {
				latestTimeNs = htlc.ResolveTimeNs
			}
		}
		pmt.settleTime = time.Unix(0, latestTimeNs)
		paymentList[i] = pmt
	}

	return paymentList, nil
}

// paymentHtlcDestination examines the htlcs in a payment to determine whether a
// payment was made to our own node.
func paymentHtlcDestination(payment lndclient.Payment) (*route.Vertex, error) {
	// If our payment has no htlcs and it is settled, it is either a legacy
	// payment that does not have its htlcs stored, or it is currently in
	// flight and no htlcs have been dispatched. We return an error because
	// we cannot get our destination with the information available.
	if len(payment.Htlcs) == 0 {
		return nil, errNoHtlcs
	}

	// Since all htlcs go to the same node, we only need to get the
	// destination of our first htlc to determine whether it's our own node.
	// We expect the route this htlc took to have at least one hop, and fail
	// if it does not.
	hops := payment.Htlcs[0].Route.Hops
	if len(hops) == 0 {
		return nil, errNoHops
	}

	lastHop := hops[len(hops)-1]
	lastHopPubkey, err := route.NewVertexFromStr(lastHop.PubKey)
	if err != nil {
		return nil, err
	}

	return &lastHopPubkey, nil
}

// paymentRequestDetails attempts to decode a payment address, and returns
// the destination and the description.
func paymentRequestDetails(paymentRequest string,
	decode decodePaymentRequest) (*route.Vertex, *string, error) {

	if paymentRequest == "" {
		return nil, nil, errNoPaymentRequest
	}

	payReq, err := decode(paymentRequest)
	if err != nil {
		return nil, nil, fmt.Errorf(
			"decode payment request failed: %w", err,
		)
	}

	return &payReq.Destination, &payReq.Description, nil
}

// filterPayments filters out unsuccessful payments and those which did not
// occur within the range we specify.
func filterPayments(startTime, endTime time.Time,
	payments []paymentInfo) []paymentInfo {

	// nolint: prealloc
	var filtered []paymentInfo

	for _, payment := range payments {
		if payment.Status.State != lnrpc.Payment_SUCCEEDED {
			continue
		}

		// Skip the payment if the oldest settle time is not within the
		// range we are looking at.
		if !inRange(payment.settleTime, startTime, endTime) {
			continue
		}

		// Add a settled payment to our set of settled payments with its
		// timestamp.
		filtered = append(filtered, payment)
	}

	return filtered
}
