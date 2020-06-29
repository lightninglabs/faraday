package frdrpc

import (
	"context"

	"github.com/lightninglabs/faraday/resolutions"
	"github.com/lightninglabs/lndclient"
)

func parseCloseReportRequest(ctx context.Context, cfg *Config) *resolutions.Config {
	return &resolutions.Config{
		ClosedChannels: func() ([]lndclient.ClosedChannel, error) {
			return cfg.Lnd.Client.ClosedChannels(ctx)
		},
		GetTxDetail: cfg.BitcoinClient.GetTxDetail,
		WalletTransactions: func() ([]lndclient.Transaction, error) {
			return cfg.Lnd.Client.ListTransactions(ctx, 0, 0)
		},
	}
}

func rpcCloseReportResponse(report *resolutions.CloseReport) *CloseReportResponse {
	return &CloseReportResponse{
		ChannelPoint:     report.ChannelPoint.String(),
		ChannelInitiator: report.ChannelInitiator,
		CloseType:        report.CloseType.String(),
		CloseTxid:        report.CloseTxid,
		OpenFee:          report.OpenFee.String(),
		CloseFee:         report.CloseFee.String(),
	}
}
