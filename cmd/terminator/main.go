package main

import (
	"os"

	"github.com/lightninglabs/terminator"
)

// main calls the "real" main function in a nested manner so that defers will be
// properly executed if os.Exit() is called.
func main() {
	if err := terminator.Main(); err != nil {
		log.Infof("Error starting termintor: %v", err)
	}

	os.Exit(1)
}
