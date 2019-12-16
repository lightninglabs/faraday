package terminator

import (
	"github.com/btcsuite/btclog"
	"github.com/lightninglabs/terminator/recommend"
	"github.com/lightningnetwork/lnd/build"
)

// Subsystem defines the logging code for this subsystem.
const Subsystem = "TERM"

var (
	logWriter = build.NewRotatingLogWriter()

	// log is a logger that is initialized with no output filters. This
	// means the package will not perform any logging by default until the
	// caller requests it.
	log = build.NewSubLogger(Subsystem, logWriter.GenSubLogger)
)

// The default amount of logging is none.
func init() {
	setSubLogger(Subsystem, log, nil)
	addSubLogger(recommend.Subsystem, recommend.UseLogger)
}

// UseLogger uses a specified Logger to output package logging info.
// This should be used in preference to SetLogWriter if the caller is also
// using btclog.
func UseLogger(logger btclog.Logger) {
	log = logger
}

// addSubLogger is a helper method to conveniently create and register the
// logger of a sub system.
func addSubLogger(subsystem string, useLogger func(btclog.Logger)) {
	logger := build.NewSubLogger(subsystem, logWriter.GenSubLogger)
	setSubLogger(subsystem, logger, useLogger)
}

// setSubLogger is a helper method to conveniently register the logger of a sub
// system.
func setSubLogger(subsystem string, logger btclog.Logger,
	useLogger func(btclog.Logger)) {

	logWriter.RegisterSubLogger(subsystem, logger)
	if useLogger != nil {
		useLogger(logger)
	}
}
