package itest

import (
	"github.com/btcsuite/btclog"
)

var (
	backend = btclog.NewBackend(newPrefixStdout("itest"))
	log     = backend.Logger("")
)
