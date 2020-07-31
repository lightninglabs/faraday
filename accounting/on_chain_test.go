package accounting

import (
	"testing"

	"github.com/lightninglabs/lndclient"
	"github.com/stretchr/testify/require"
)

// TestOnChainReport tests sorting of transactions into different categories.
// It does not test the details of the entries provided, because we have
// individual tests for each entry type.
func TestOnChainReport(t *testing.T) {
	tests := []struct {
		name            string
		tx              lndclient.Transaction
		sweeps          map[string]bool
		openedChannels  map[string]channelInfo
		closedChannels  map[string]closedChannelInfo
		expectedEntries map[EntryType]bool
	}{
		{
			name: "sweep tx",
			tx: lndclient.Transaction{
				TxHash: hash.String(),
			},
			sweeps: map[string]bool{
				hash.String(): true,
			},
			expectedEntries: map[EntryType]bool{
				EntryTypeSweep: true,
			},
		},
		{
			name: "locally opened channel",
			tx: lndclient.Transaction{
				TxHash: hash.String(),
				Amount: -20000,
				Fee:    100,
			},
			openedChannels: map[string]channelInfo{
				hash.String(): {},
			},
			expectedEntries: map[EntryType]bool{
				EntryTypeLocalChannelOpen: true,
				EntryTypeChannelOpenFee:   true,
			},
		},
		{
			name: "remote opened channel",
			tx: lndclient.Transaction{
				TxHash: hash.String(),
				Amount: 0,
			},
			openedChannels: map[string]channelInfo{
				hash.String(): {},
			},
			expectedEntries: map[EntryType]bool{
				EntryTypeRemoteChannelOpen: true,
			},
		},
		{
			name: "closed channel",
			tx: lndclient.Transaction{
				TxHash: hash.String(),
			},
			closedChannels: map[string]closedChannelInfo{
				hash.String(): {},
			},
			expectedEntries: map[EntryType]bool{
				EntryTypeChannelClose: true,
			},
		},
	}

	for _, test := range tests {
		test := test

		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			info := &onChainInformation{
				txns:           []lndclient.Transaction{test.tx},
				priceFunc:      mockPrice,
				sweeps:         test.sweeps,
				openedChannels: test.openedChannels,
				closedChannels: test.closedChannels,
			}

			report, err := onChainReport(info)
			require.NoError(t, err, "on chain report failed")
			require.Equal(t, len(test.expectedEntries), len(report),
				"wrong number of reports")

			// Check that each of the entries we got is an expected
			// type.
			for _, entry := range report {
				_, ok := test.expectedEntries[entry.Type]
				require.True(t, ok, "entry type: %v not "+
					"expected", entry.Type)

				// Once we've found an entry, set it to false
				// so that we do not double count it.
				test.expectedEntries[entry.Type] = false
			}
		})
	}
}
