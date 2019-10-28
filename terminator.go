// Package terminator contains the main function for the terminator.
package terminator

// Main is the real entry point for terminator. It is required to ensure that
// defers are properly executed when os.Exit() is called.
func Main() error {
	log.Infof("That is all. I will be back.")

	return nil
}
