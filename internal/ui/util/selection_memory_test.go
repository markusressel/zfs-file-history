package util

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

type dummyEntry struct {
	Name string
}

func TestSelectionMemory_GetReturnsNilForUnknownKey(t *testing.T) {
	memory := NewSelectionMemory[dummyEntry]()

	selection := memory.Get("unknown")

	assert.Nil(t, selection)
}

func TestSelectionMemory_RememberAndGet(t *testing.T) {
	memory := NewSelectionMemory[dummyEntry]()
	entry := &dummyEntry{Name: "alpha"}

	memory.Remember("/tmp", 3, entry)
	selection := memory.Get("/tmp")

	if assert.NotNil(t, selection) {
		assert.Equal(t, 3, selection.Index)
		assert.Equal(t, entry, selection.Entry)
	}
}

func TestSelectionMemory_RememberOverwritesExistingValue(t *testing.T) {
	memory := NewSelectionMemory[dummyEntry]()
	entryA := &dummyEntry{Name: "alpha"}
	entryB := &dummyEntry{Name: "beta"}

	memory.Remember("/tmp", 1, entryA)
	memory.Remember("/tmp", 5, entryB)
	selection := memory.Get("/tmp")

	if assert.NotNil(t, selection) {
		assert.Equal(t, 5, selection.Index)
		assert.Equal(t, entryB, selection.Entry)
	}
}
