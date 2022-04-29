package frdrpcserver

import (
	"context"
	"time"

	"github.com/lightninglabs/faraday/frdrpc"
	"github.com/lightninglabs/faraday/lndwrap"
	"github.com/lightninglabs/faraday/revenue"
	"github.com/lightninglabs/lndclient"
)

// parseRevenueRequest parses a request for a revenue report and wraps
// calls to lnd client to produce the config required to get a revenue
// report.
func parseRevenueRequest(ctx context.Context, cfg *Config,
	req *frdrpc.RevenueReportRequest) *revenue.Config {

	// Progress end time to the present if it is not set.
	// We allow start time to be zero so that revenue can
	// be calculated over the channel's full lifetime without
	// knowing the time it was opened.
	endTime := time.Unix(int64(req.EndTime), 0)
	if req.EndTime == 0 {
		endTime = time.Now()
	}

	start := time.Unix(int64(req.StartTime), 0)
	return getRevenueConfig(ctx, cfg, start, endTime)
}

func getRevenueConfig(ctx context.Context, cfg *Config,
	start, end time.Time) *revenue.Config {

	return &revenue.Config{
		ListChannels: lndwrap.ListChannels(ctx, cfg.Lnd.Client, false),
		ClosedChannels: func() ([]lndclient.ClosedChannel, error) {
			return cfg.Lnd.Client.ClosedChannels(ctx)
		},
		ForwardingHistory: func() ([]lndclient.ForwardingEvent, error) {
			return lndwrap.ListForwards(
				ctx, uint64(maxForwardQueries), start, end,
				cfg.Lnd.Client,
			)
		},
	}
}

// rpcRevenueResponse takes a target channel and revenue report and produces
// a revenue report response. If the channel had no revenue, an empty report is
// returned.
func rpcRevenueResponse(targetChannels []string,
	revenueReport *revenue.Report) (*frdrpc.RevenueReportResponse, error) {

	resp := &frdrpc.RevenueReportResponse{}

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
		rpcReport := &frdrpc.RevenueReport{
			TargetChannel: targetChannel,
			PairReports:   make(map[string]*frdrpc.PairReport),
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
			rpcReport.PairReports[peer] = &frdrpc.PairReport{
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
