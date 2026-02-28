package frdrpcserver

import (
	"testing"

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
