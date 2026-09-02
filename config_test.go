package faraday

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// newValidTestConfig returns a config that passes ValidateConfig other than
// the bitcoin backend settings, which are set up by the individual test
// cases below.
func newValidTestConfig(t *testing.T) Config {
	cfg := DefaultConfig()
	cfg.FaradayDir = t.TempDir()
	cfg.ChainConn = true

	// DefaultConfig points Bitcoin at the shared chain.DefaultConfig
	// instance, so copy it before mutating to avoid leaking state
	// across test cases.
	bitcoinCfg := *cfg.Bitcoin
	cfg.Bitcoin = &bitcoinCfg
	cfg.Bitcoin.User = "user"

	return cfg
}

// TestValidateConfigBitcoinPassword tests that ValidateConfig correctly
// resolves the bitcoind/btcd RPC password from either the plaintext
// --bitcoin.password flag or from a file referenced by
// --bitcoin.passwordfile, and rejects invalid combinations of the two.
func TestValidateConfigBitcoinPassword(t *testing.T) {
	t.Parallel()

	pwFile := filepath.Join(t.TempDir(), "pw.txt")
	err := os.WriteFile(pwFile, []byte("filepassword\n"), 0644)
	require.NoError(t, err)

	tests := []struct {
		name        string
		setup       func(cfg *Config)
		expectedPw  string
		expectedErr string
	}{
		{
			name: "plaintext password",
			setup: func(cfg *Config) {
				cfg.Bitcoin.Password = "plainpassword"
			},
			expectedPw: "plainpassword",
		},
		{
			name: "password file",
			setup: func(cfg *Config) {
				cfg.Bitcoin.PasswordFile = pwFile
			},
			expectedPw: "filepassword",
		},
		{
			name: "password file does not exist",
			setup: func(cfg *Config) {
				cfg.Bitcoin.PasswordFile = filepath.Join(
					t.TempDir(), "missing.txt",
				)
			},
			expectedErr: "could not read bitcoin.passwordfile",
		},
		{
			name: "password and password file set",
			setup: func(cfg *Config) {
				cfg.Bitcoin.Password = "plainpassword"
				cfg.Bitcoin.PasswordFile = pwFile
			},
			expectedErr: "mutually exclusive",
		},
		{
			name:        "neither password nor password file set",
			setup:       func(cfg *Config) {},
			expectedErr: "rpc user and password required",
		},
	}

	for _, test := range tests {
		test := test

		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			cfg := newValidTestConfig(t)
			test.setup(&cfg)

			err := ValidateConfig(&cfg)
			if test.expectedErr != "" {
				require.ErrorContains(t, err, test.expectedErr)
				return
			}

			require.NoError(t, err)
			require.Equal(t, test.expectedPw, cfg.Bitcoin.Password)
		})
	}
}
