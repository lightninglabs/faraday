// Package frdrpcserver contains the server logic for faraday's grpc server
// which serves requests for close recommendations.
//
// The Faraday server interface is implemented by the RPCServer struct.
// To keep this file readable, each function implemented by the interface
// has a file named after the function call which contains rpc parsing
// code for the request and response. If the call requires extensive
// additional logic, and unexported function with the same name should
// be created in this file as well.
package frdrpcserver

import (
	"context"
	"errors"

	"github.com/lightninglabs/faraday/accounting"
	"github.com/lightninglabs/faraday/chain"
	"github.com/lightninglabs/faraday/chanevents"
	"github.com/lightninglabs/faraday/fiat"
	"github.com/lightninglabs/faraday/frdrpc"
	"github.com/lightninglabs/faraday/recommend"
	"github.com/lightninglabs/faraday/resolutions"
	"github.com/lightninglabs/faraday/revenue"
	"github.com/lightninglabs/lndclient"
)

var (
	// maxInvoiceQueries is the maximum number of invoices we request from
	// lnd at a time.
	maxInvoiceQueries = 1000

	// maxPaymentQueries is the maximum number of invoices we request from
	// lnd at a time. It is less than the number of invoices we request
	// because payments have a lot of htlc data.
	maxPaymentQueries = 500

	// maxForwardQueries is the maximum number of forwards we request from
	// lnd at a time. It is more than the number of invoices we request
	// because forwards have less data.
	maxForwardQueries = 2000

	// ErrBitcoinNodeRequired is required when an endpoint which requires
	// a bitcoin node backend is hit and we are not connected to one.
	ErrBitcoinNodeRequired = errors.New("bitcoin node required")
)

// RPCServer implements the faraday service, serving requests over grpc.
type RPCServer struct {
	// Required by the grpc-gateway/v2 library for forward compatibility.
	frdrpc.UnimplementedFaradayServerServer

	// cfg contains closures and settings required for operation.
	cfg *Config
}

// Config provides closures and settings required to run the rpc server.
type Config struct {
	// Lnd is a client which can be used to query lnd.
	Lnd lndclient.LndServices

	// ChanEvents is a database of channel events.
	ChanEvents *chanevents.Store

	// BitcoinClient is an optional client which can be used to query
	// on-chain data from a connected bitcoin node. If nil, faraday will
	// not be able to serve endpoints which require on-chain data.
	BitcoinClient chain.BitcoinClient
}

// NewRPCServer returns a new RPCServer backed by the given config.
func NewRPCServer(cfg *Config) *RPCServer {
	return &RPCServer{
		cfg: cfg,
	}
}

// OutlierRecommendations provides a set of close recommendations for the
// current set of open channels based on whether they are outliers.
func (s *RPCServer) OutlierRecommendations(ctx context.Context,
	req *frdrpc.OutlierRecommendationsRequest) (*frdrpc.CloseRecommendationsResponse,
	error) {

	if req.RecRequest == nil {
		return nil, errors.New("recommendation request field required")
	}

	log.Debugf("[OutlierRecommendations]: metric: %v, multiplier: %v",
		req.RecRequest.Metric, req.OutlierMultiplier)

	cfg, multiplier := parseOutlierRequest(ctx, s.cfg, req)

	report, err := recommend.OutlierRecommendations(cfg, multiplier)
	if err != nil {
		return nil, err
	}

	return rpcResponse(report), nil
}

// ThresholdRecommendations provides a set of close recommendations for the
// current set of open channels based on whether they are above or below a
// given threshold.
func (s *RPCServer) ThresholdRecommendations(ctx context.Context,
	req *frdrpc.ThresholdRecommendationsRequest) (*frdrpc.CloseRecommendationsResponse,
	error) {

	if req.RecRequest == nil {
		return nil, errors.New("recommendation request field required")
	}

	log.Debugf("[ThresholdRecommendations]: metric: %v, threshold: %v",
		req.RecRequest.Metric, req.ThresholdValue)

	cfg, threshold := parseThresholdRequest(ctx, s.cfg, req)

	report, err := recommend.ThresholdRecommendations(cfg, threshold)
	if err != nil {
		return nil, err
	}

	return rpcResponse(report), nil
}

// RevenueReport returns a pairwise revenue report for a channel
// over the period requested.
func (s *RPCServer) RevenueReport(ctx context.Context,
	req *frdrpc.RevenueReportRequest) (*frdrpc.RevenueReportResponse, error) {

	log.Debugf("[RevenueReport]: range: %v-%v, channels: %v", req.StartTime,
		req.EndTime, req.ChanPoints)

	revenueConfig := parseRevenueRequest(ctx, s.cfg, req)

	report, err := revenue.GetRevenueReport(revenueConfig)
	if err != nil {
		return nil, err
	}

	return rpcRevenueResponse(req.GetChanPoints(), report)
}

// ChannelInsights returns the channel insights for our currently open set
// of channels.
func (s *RPCServer) ChannelInsights(ctx context.Context,
	_ *frdrpc.ChannelInsightsRequest) (*frdrpc.ChannelInsightsResponse, error) {

	log.Debugf("[ChannelInsights]")

	insights, err := channelInsights(ctx, s.cfg)
	if err != nil {
		return nil, err
	}

	return rpcChannelInsightsResponse(insights), nil
}

// ExchangeRate provides a fiat estimate for a set of timestamped bitcoin
// prices.
func (s *RPCServer) ExchangeRate(ctx context.Context,
	req *frdrpc.ExchangeRateRequest) (*frdrpc.ExchangeRateResponse, error) {

	log.Debugf("[FiatEstimate]: %v requests", len(req.Timestamps))

	timestamps, priceCfg, err := parseExchangeRateRequest(req)
	if err != nil {
		return nil, err
	}

	prices, err := fiat.GetPrices(ctx, timestamps, priceCfg)
	if err != nil {
		return nil, err
	}

	return exchangeRateResponse(prices), nil
}

// NodeAudit returns an on chain report for the period requested.
func (s *RPCServer) NodeAudit(ctx context.Context,
	req *frdrpc.NodeAuditRequest) (*frdrpc.NodeAuditResponse, error) {

	log.Debugf("[NodeAudit]: range: %v-%v, fiat: %v", req.StartTime,
		req.EndTime, req.DisableFiat)

	onChain, offChain, err := parseNodeAuditRequest(ctx, s.cfg, req)
	if err != nil {
		return nil, err
	}

	onChainReport, err := accounting.OnChainReport(ctx, onChain)
	if err != nil {
		return nil, err
	}

	offChainReport, err := accounting.OffChainReport(ctx, offChain)
	if err != nil {
		return nil, err
	}

	return rpcReportResponse(append(onChainReport, offChainReport...))
}

// CloseReport returns a close report for the channel provided. Note that this
// endpoint requires connection to an external bitcoind node.
func (s *RPCServer) CloseReport(ctx context.Context,
	req *frdrpc.CloseReportRequest) (*frdrpc.CloseReportResponse, error) {

	log.Debugf("[CloseReport]: %v", req.ChannelPoint)

	if err := s.requireNode(); err != nil {
		return nil, err
	}

	cfg := parseCloseReportRequest(ctx, s.cfg)

	report, err := resolutions.ChannelCloseReport(cfg, req.ChannelPoint)
	if err != nil {
		return nil, err
	}

	return rpcCloseReportResponse(report), nil
}

// requireNode fails if we do not have a connection to a backing bitcoin node.
func (s *RPCServer) requireNode() error {
	if s.cfg.BitcoinClient == nil {
		return ErrBitcoinNodeRequired
	}

	return nil
}
