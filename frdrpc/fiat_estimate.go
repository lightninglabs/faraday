package frdrpc

import (
	"fmt"
	"time"

	"github.com/lightninglabs/faraday/fiat"
	"github.com/lightningnetwork/lnd/lnwire"
	"github.com/shopspring/decimal"
)

func parseFiatRequest(req *FiatEstimateRequest) (fiat.Granularity,
	[]*fiat.PriceRequest, error) {

	granularity := fiat.GranularityMinute

	switch req.Granularity {
	// If granularity is not set, allow it to default to one minute
	case FiatEstimateRequest_UNKNOWN:

	case FiatEstimateRequest_MINUTE:
		granularity = fiat.GranularityMinute

	case FiatEstimateRequest_FIVE_MINUTES:
		granularity = fiat.Granularity5Minute

	case FiatEstimateRequest_FIFTEEN_MINUTES:
		granularity = fiat.Granularity15Minute

	case FiatEstimateRequest_THIRTY_MINUTES:
		granularity = fiat.Granularity30Minute

	case FiatEstimateRequest_HOUR:
		granularity = fiat.GranularityHour

	case FiatEstimateRequest_SIX_HOURS:
		granularity = fiat.Granularity6Hour

	case FiatEstimateRequest_TWELVE_HOURS:
		granularity = fiat.Granularity12Hour

	case FiatEstimateRequest_DAY:
		granularity = fiat.GranularityDay

	default:
		return granularity, nil, fmt.Errorf("unknown granularity: %v",
			req.Granularity)
	}

	requests := make([]*fiat.PriceRequest, len(req.Requests))

	for i, request := range req.Requests {
		requests[i] = &fiat.PriceRequest{
			Identifier: request.Id,
			Value:      lnwire.MilliSatoshi(request.AmountMsat),
			Timestamp:  time.Unix(request.Timestamp, 0),
		}
	}

	return granularity, requests, nil
}

func fiatEstimateResponse(prices map[string]decimal.Decimal) *FiatEstimateResponse {
	fiatVals := make(map[string]string, len(prices))
	for k, v := range prices {
		fiatVals[k] = v.String()
	}

	return &FiatEstimateResponse{
		FiatValues: fiatVals,
	}
}
