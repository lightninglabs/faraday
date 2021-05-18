package main

import (
	"fmt"
	"os"

	"github.com/lightninglabs/faraday"
)

// main calls the "real" main function in a nested manner so that defers will be
// properly executed if os.Exit() is called.
func main() {
	if err := faraday.Main(); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "Error starting faraday: %v", err)
	}

	os.Exit(1)
}
