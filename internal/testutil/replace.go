package testutil

import "testing"

// Replace sets the package variable to value until the end of the test, e.g. a function that runs a process or calls
// libzfs (see AGENTS.md, Tests). The variable is shared by all tests of the package, so a test that replaces one
// cannot run in parallel: Replace fails it (t.Setenv panics in parallel tests). The apps of the test (RunApp) are
// stopped before the value is restored, also those started before Replace, so they never see the change.
func Replace[T any](t testing.TB, variable *T, value T) {
	t.Helper()
	t.Setenv("ZFS_FILE_HISTORY_TEST_REPLACES_VARIABLES", "1")
	previous := *variable
	*variable = value
	t.Cleanup(func() {
		runningApps.stopAll(t)
		*variable = previous
	})
}
