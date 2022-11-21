package frdrpcserver

import (
	"context"

	"github.com/btcsuite/btcd/btcutil"
	"github.com/btcsuite/btcd/chaincfg/chainhash"
	"github.com/lightninglabs/faraday/fees"
	"github.com/lightninglabs/faraday/frdrpc"
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
		CalculateFees: func(hash *chainhash.Hash) (btcutil.Amount, error) {
			return fees.CalculateFee(
				cfg.BitcoinClient.GetTxDetail, hash,
			)
		},
	}
}

func rpcCloseReportResponse(
	report *resolutions.CloseReport) *frdrpc.CloseReportResponse {

	return &frdrpc.CloseReportResponse{
		ChannelPoint:     report.ChannelPoint.String(),
		ChannelInitiator: report.ChannelInitiator,
		CloseType:        report.CloseType.String(),
		CloseTxid:        report.CloseTxid,
		OpenFee:          report.OpenFee.String(),
		CloseFee:         report.CloseFee.String(),
	}
}
