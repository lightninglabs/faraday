package frdrpc

import (
	"time"

	"github.com/lightninglabs/faraday/fiat"
	"github.com/lightningnetwork/lnd/lnwire"
	"github.com/shopspring/decimal"
)

func parseFiatRequest(req *FiatEstimateRequest) []*fiat.PriceRequest {
	requests := make([]*fiat.PriceRequest, len(req.Requests))

	for i, request := range req.Requests {
		requests[i] = &fiat.PriceRequest{
			Identifier: request.Id,
			Value:      lnwire.MilliSatoshi(request.AmountMsat),
			Timestamp:  time.Unix(request.Timestamp, 0),
		}
	}

	return requests
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
