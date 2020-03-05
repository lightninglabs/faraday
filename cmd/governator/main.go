package main

import (
	"os"

	"github.com/lightninglabs/governator"
)

// main calls the "real" main function in a nested manner so that defers will be
// properly executed if os.Exit() is called.
func main() {
	if err := governator.Main(); err != nil {
		log.Infof("Error starting governator: %v", err)
	}

	os.Exit(1)
}
