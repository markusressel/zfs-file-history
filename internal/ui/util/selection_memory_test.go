package util

import (
	"path/filepath"
	"testing"
	"zfs-file-history/internal/state"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type dummyEntry struct {
	Name string
}

func newDummyMemory() *SelectionMemory[dummyEntry] {
	return NewSelectionMemory(func(entry *dummyEntry) string { return entry.Name })
}

func TestSelectionMemory_GetReturnsNilForUnknownKey(t *testing.T) {
	memory := newDummyMemory()

	selection := memory.Get("unknown")

	assert.Nil(t, selection)
}

func TestSelectionMemory_RememberAndGet(t *testing.T) {
	memory := newDummyMemory()

	memory.Remember("/tmp", 3, &dummyEntry{Name: "alpha"})
	selection := memory.Get("/tmp")

	if assert.NotNil(t, selection) {
		assert.Equal(t, SelectionInfo{Index: 3, Key: "alpha"}, *selection)
	}
}

func TestSelectionMemory_RememberOverwritesExistingValue(t *testing.T) {
	memory := newDummyMemory()

	memory.Remember("/tmp", 1, &dummyEntry{Name: "alpha"})
	memory.Remember("/tmp", 5, &dummyEntry{Name: "beta"})
	selection := memory.Get("/tmp")

	if assert.NotNil(t, selection) {
		assert.Equal(t, SelectionInfo{Index: 5, Key: "beta"}, *selection)
	}
}

func TestSelectionMemory_Restore(t *testing.T) {
	alpha, beta, gamma := &dummyEntry{Name: "alpha"}, &dummyEntry{Name: "beta"}, &dummyEntry{Name: "gamma"}
	entries := []*dummyEntry{alpha, beta, gamma}
	memory := newDummyMemory()

	entry, header := memory.Restore("/tmp", nil)
	assert.Nil(t, entry)
	assert.False(t, header)

	// nothing remembered: the first entry
	entry, header = memory.Restore("/tmp", entries)
	assert.Same(t, alpha, entry)
	assert.False(t, header)

	// by identity, after reloading the entries
	memory.Remember("/tmp", 0, &dummyEntry{Name: "gamma"})
	entry, _ = memory.Restore("/tmp", entries)
	assert.Same(t, gamma, entry)

	// gone: the one at its index
	memory.Remember("/tmp", 1, &dummyEntry{Name: "removed"})
	entry, _ = memory.Restore("/tmp", entries)
	assert.Same(t, beta, entry)
	memory.Remember("/tmp", 7, &dummyEntry{Name: "removed"})
	entry, _ = memory.Restore("/tmp", entries)
	assert.Same(t, gamma, entry)

	memory.Remember("/tmp", -1, nil)
	entry, header = memory.Restore("/tmp", entries)
	assert.Nil(t, entry)
	assert.True(t, header)
}

func TestSelectionMemory_Persist(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	store := state.Load(path)
	memory := newDummyMemory().Persist(store, "selection", 10)

	memory.Remember("/tmp", 2, &dummyEntry{Name: "beta"})
	memory.RememberInfo("/home", SelectionInfo{Key: "user"})
	require.NoError(t, store.Flush())

	restored := newDummyMemory().Persist(state.Load(path), "selection", 10)
	assert.Equal(t, &SelectionInfo{Index: 2, Key: "beta"}, restored.Get("/tmp"))
	assert.Equal(t, &SelectionInfo{Key: "user"}, restored.Get("/home"))
	assert.Nil(t, restored.Get("/other"))

	// without a store, nothing is persisted
	assert.Nil(t, newDummyMemory().Persist(nil, "selection", 10).Get("/tmp"))
}
