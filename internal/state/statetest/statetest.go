// Package statetest gives tests a state of their own (see state.Current).
package statetest

import (
	"path/filepath"
	"testing"
	"zfs-file-history/internal/state"
	"zfs-file-history/internal/testutil"

	"github.com/stretchr/testify/require"
)

// Use sets state.Current to a new store in a temporary directory until the end of the test, and returns it. Without
// it, tests run without a state (nothing is restored or saved).
func Use(t testing.TB) *state.Store {
	t.Helper()
	return UseFile(t, filepath.Join(t.TempDir(), "state.json"))
}

// UseFile sets state.Current to the store of the file until the end of the test, and returns it, e.g. to load the
// state a test saved before as the next start of the application would.
func UseFile(t testing.TB, path string) *state.Store {
	t.Helper()
	store := state.Load(path)
	testutil.Replace(t, &state.Current, store)
	// before the state is restored (cleanups run in reverse order), so pending saves end with the test
	t.Cleanup(func() { _ = store.Flush() })
	return store
}

// AfterRestart saves state.Current to the file and runs create with state.Current set to the state the file has
// then, as the next start of the application loads it, e.g. to see what a new component restores. state.Current is
// set back afterwards.
func AfterRestart(t testing.TB, path string, create func()) {
	t.Helper()
	current := state.Current
	require.NoError(t, current.Flush())
	state.Current = state.Load(path)
	defer func() { state.Current = current }()
	create()
}
