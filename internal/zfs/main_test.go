package zfs

import (
	"os"
	"testing"
)

// TestMain uses the System: the tests of this package test it, with the commands it runs replaced (see runCommand),
// unlike the tests of other packages, which get NoSystem.
func TestMain(m *testing.M) {
	SetBackend(System{})
	os.Exit(m.Run())
}
