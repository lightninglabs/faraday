package itest

import "testing"

// TestFail only serves to detect bugs that prevent itest errors from surfacing.
// It always fails and is caught at a higher level in run_itest.sh.
func TestFail(t *testing.T) {
	t.Fatal("failure canary")
}
