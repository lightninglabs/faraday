package frdrpcserver

import (
	"testing"
	"time"

	"github.com/lightninglabs/faraday/fiat"
	"github.com/lightninglabs/faraday/frdrpc"
	"github.com/stretchr/testify/require"
)

// TestFiatBackendFromRPC checks mapping from rpc enum values to fiat backend
// implementations.
func TestFiatBackendFromRPC(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		in        frdrpc.FiatBackend
		expected  fiat.PriceBackend
		expectErr bool
	}{
		{
			name:     "unknown",
			in:       frdrpc.FiatBackend_UNKNOWN_FIATBACKEND,
			expected: fiat.UnknownPriceBackend,
		},
		{
			name:     "coincap",
			in:       frdrpc.FiatBackend_COINCAP,
			expected: fiat.CoinCapPriceBackend,
		},
		{
			name:     "coindesk",
			in:       frdrpc.FiatBackend_COINDESK,
			expected: fiat.CoinDeskPriceBackend,
		},
		{
			name:     "custom",
			in:       frdrpc.FiatBackend_CUSTOM,
			expected: fiat.CustomPriceBackend,
		},
		{
			name:     "coingecko",
			in:       frdrpc.FiatBackend_COINGECKO,
			expected: fiat.CoinGeckoPriceBackend,
		},
		{
			name:     "bitfinex",
			in:       fiatBackendBitfinex,
			expected: fiat.BitfinexPriceBackend,
		},
		{
			name:      "invalid enum",
			in:        frdrpc.FiatBackend(999),
			expectErr: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			backend, err := fiatBackendFromRPC(test.in)
			if test.expectErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				require.Equal(t, test.expected, backend)
			}
		})
	}
}

// TestPriceCfgFromRPCBitfinexGranularity verifies that priceCfgFromRPC sets
// a granularity for bitfinex so that the resulting config passes validation.
func TestPriceCfgFromRPCBitfinexGranularity(t *testing.T) {
	t.Parallel()

	start := time.Unix(1711929600, 0).UTC()
	end := start.Add(2 * time.Hour)

	cfg, err := priceCfgFromRPC(
		fiatBackendBitfinex, frdrpc.Granularity_HOUR, false,
		start, end, nil,
	)
	require.NoError(t, err)
	require.NotNil(t, cfg)
	require.Equal(t, fiat.BitfinexPriceBackend, cfg.Backend)
	require.NotNil(t, cfg.Granularity)
	require.Equal(t, fiat.GranularityHour, *cfg.Granularity)

	// Validate that this config can be used to construct a price source.
	_, err = fiat.NewPriceSource(cfg)
	require.NoError(t, err)
}
