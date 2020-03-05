package gvnrpc

import (
	"context"
	"time"

	"github.com/lightninglabs/governator/revenue"
	"github.com/lightningnetwork/lnd/lnrpc"
)

// parseRevenueRequest parses a request for a revenue report and wraps
// calls to lnd client to produce the config required to get a revenue
// report.
func parseRevenueRequest(ctx context.Context, cfg *Config,
	req *RevenueReportRequest) *revenue.Config {

	// Progress end time to the present if it is not set.
	// We allow start time to be zero so that revenue can
	// be calculated over the channel's full lifetime without
	// knowing the time it was opened.
	endTime := req.EndTime
	if endTime == 0 {
		endTime = uint64(time.Now().Unix())
	}

	return getRevenueConfig(ctx, cfg, req.StartTime, endTime)
}

func getRevenueConfig(ctx context.Context, cfg *Config,
	start, end uint64) *revenue.Config {

	closedChannels := func() ([]*lnrpc.ChannelCloseSummary, error) {
		resp, err := cfg.LightningClient.ClosedChannels(
			ctx, &lnrpc.ClosedChannelsRequest{},
		)
		if err != nil {
			return nil, err
		}

		return resp.Channels, nil
	}

	forwardingHistory := func(offset,
		maxEvents uint32) ([]*lnrpc.ForwardingEvent, uint32, error) {
		resp, err := cfg.LightningClient.ForwardingHistory(
			ctx, &lnrpc.ForwardingHistoryRequest{
				StartTime:    start,
				EndTime:      end,
				IndexOffset:  offset,
				NumMaxEvents: maxEvents,
			},
		)
		if err != nil {
			return nil, 0, err
		}

		return resp.ForwardingEvents, resp.LastOffsetIndex, nil
	}

	return &revenue.Config{
		ListChannels:      cfg.wrapListChannels(ctx, false),
		ClosedChannels:    closedChannels,
		ForwardingHistory: forwardingHistory,
	}
}

// rpcRevenueResponse takes a target channel and revenue report and produces
// a revenue report response. If the channel had no revenue, an empty report is
// returned.
func rpcRevenueResponse(targetChannels []string,
	revenueReport *revenue.Report) (*RevenueReportResponse, error) {

	resp := &RevenueReportResponse{}

	// If no channels were specifically requested, set all channels in the
	// report as our set of target channels.
	if len(targetChannels) == 0 {
		for channel := range revenueReport.ChannelPairs {
			targetChannels = append(targetChannels, channel)
		}
	}

	// Run through each channel that we want a report for, and create a rpc
	// report for it.
	for _, targetChannel := range targetChannels {
		rpcReport := &RevenueReport{
			TargetChannel: targetChannel,
			PairReports:   make(map[string]*PairReport),
		}

		// Lookup the target channel in the revenue report. If it is
		// not found, add an empty report to our set of reports and
		// skip the remainder of the loop.
		report, ok := revenueReport.ChannelPairs[targetChannel]
		if !ok {
			log.Tracef("no revenue found for channel %v",
				targetChannel)

			resp.Reports = append(resp.Reports, rpcReport)
			continue
		}

		// Add revenue reports for each of our peers to the response.
		for peer, rp := range report {
			rpcReport.PairReports[peer] = &PairReport{
				AmountOutgoingMsat: int64(rp.AmountOutgoing),
				FeesOutgoingMsat:   int64(rp.FeesOutgoing),
				AmountIncomingMsat: int64(rp.AmountIncoming),
				FeesIncomingMsat:   int64(rp.FeesIncoming),
			}
		}

		resp.Reports = append(resp.Reports, rpcReport)
	}

	return resp, nil
}
