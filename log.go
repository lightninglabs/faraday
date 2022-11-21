package faraday

import (
	"github.com/btcsuite/btclog"
	"github.com/lightninglabs/faraday/accounting"
	"github.com/lightninglabs/faraday/dataset"
	"github.com/lightninglabs/faraday/fiat"
	"github.com/lightninglabs/faraday/frdrpcserver"
	"github.com/lightninglabs/faraday/recommend"
	"github.com/lightninglabs/faraday/revenue"
	"github.com/lightningnetwork/lnd/build"
	"github.com/lightningnetwork/lnd/signal"
)

// Subsystem defines the logging code for this subsystem.
const Subsystem = "FRDY"

var (
	// log is a logger that is initialized with no output filters. This
	// means the package will not perform any logging by default until the
	// caller requests it.
	log btclog.Logger
)

// SetupLoggers initializes all package-global logger variables.
func SetupLoggers(root *build.RotatingLogWriter, intercept signal.Interceptor) {
	genLogger := genSubLogger(root, intercept)

	log = build.NewSubLogger(Subsystem, genLogger)

	setSubLogger(root, Subsystem, log, nil)
	addSubLogger(root, recommend.Subsystem, intercept, recommend.UseLogger)
	addSubLogger(root, dataset.Subsystem, intercept, dataset.UseLogger)
	addSubLogger(
		root, frdrpcserver.Subsystem, intercept, frdrpcserver.UseLogger,
	)
	addSubLogger(root, revenue.Subsystem, intercept, revenue.UseLogger)
	addSubLogger(root, fiat.Subsystem, intercept, fiat.UseLogger)
	addSubLogger(root, accounting.Subsystem, intercept, accounting.UseLogger)
}

// UseLogger uses a specified Logger to output package logging info.
// This should be used in preference to SetLogWriter if the caller is also
// using btclog.
func UseLogger(logger btclog.Logger) {
	log = logger
}

// genSubLogger creates a logger for a subsystem. We provide an instance of
// a signal.Interceptor to be able to shutdown in the case of a critical error.
func genSubLogger(root *build.RotatingLogWriter,
	interceptor signal.Interceptor) func(string) btclog.Logger {

	// Create a shutdown function which will request shutdown from our
	// interceptor if it is listening.
	shutdown := func() {
		if !interceptor.Listening() {
			return
		}

		interceptor.RequestShutdown()
	}

	// Return a function which will create a sublogger from our root
	// logger without shutdown fn.
	return func(tag string) btclog.Logger {
		return root.GenSubLogger(tag, shutdown)
	}
}

// addSubLogger is a helper method to conveniently create and register the
// logger of a sub system.
func addSubLogger(root *build.RotatingLogWriter, subsystem string,
	interceptor signal.Interceptor, useLogger func(btclog.Logger)) {

	logger := build.NewSubLogger(subsystem, genSubLogger(root, interceptor))
	setSubLogger(root, subsystem, logger, useLogger)
}

// setSubLogger is a helper method to conveniently register the logger of a sub
// system.
func setSubLogger(root *build.RotatingLogWriter, subsystem string,
	logger btclog.Logger, useLogger func(btclog.Logger)) {

	root.RegisterSubLogger(subsystem, logger)
	if useLogger != nil {
		useLogger(logger)
	}
}
