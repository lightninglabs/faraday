package itest

import "github.com/btcsuite/btclog/v2"

var (
	handler = btclog.NewDefaultHandler(newPrefixStdout("itest"))
	log     = btclog.NewSLogger(handler)
)
