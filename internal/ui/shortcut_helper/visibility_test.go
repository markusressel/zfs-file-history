package shortcut_helper

import (
	"path/filepath"
	"testing"
	"zfs-file-history/internal/state"

	"github.com/rivo/tview"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestToggleShortcuts(t *testing.T) {
	store := state.Load(filepath.Join(t.TempDir(), "state.json"))
	state.Current = store
	collapsibleMaps = nil
	t.Cleanup(func() {
		_ = store.Flush()
		state.Current = nil
		shortcutsHidden.Store(false)
		collapsibleMaps = nil
	})
	InitShortcutVisibility()
	require.False(t, ShortcutsHidden(), "shown by default")

	app := tview.NewApplication()
	entries := []ShortcutEntry{{KeyCombo: []string{"q"}, Name: "Quit"}}
	newMap := func(collapsible bool) (*ShortcutMapComponent, *int) {
		shortcutMap := NewShortcutMap(app)
		if collapsible {
			shortcutMap.SetCollapsible()
		}
		height := -1
		shortcutMap.SetOnHeightChanged(func(h int) { height = h })
		shortcutMap.SetEntries(entries)
		return shortcutMap, &height
	}
	pageMap, pageHeight := newMap(true)
	dialogMap, dialogHeight := newMap(false)
	require.Positive(t, *pageHeight)
	require.Positive(t, *dialogHeight)

	ToggleShortcuts()
	assert.True(t, ShortcutsHidden())
	assert.True(t, store.Toggle(toggleHideShortcuts, false), "remembered")
	assert.Equal(t, 0, *pageHeight, "collapsible maps take no space")
	assert.Empty(t, pageMap.shortcutEntriesTextView.GetText(false))
	assert.Equal(t, 0, pageMap.CalculateHeightForWidth(80))
	assert.Positive(t, dialogMap.CalculateHeightForWidth(80), "dialogs keep their shortcuts")
	assert.NotEmpty(t, dialogMap.shortcutEntriesTextView.GetText(false))

	// entries set while hidden are shown later
	pageMap.SetEntries([]ShortcutEntry{{KeyCombo: []string{"x"}, Name: "Other"}})
	assert.Equal(t, 0, *pageHeight)
	ToggleShortcuts()
	assert.False(t, ShortcutsHidden())
	assert.Positive(t, *pageHeight)
	assert.Contains(t, pageMap.shortcutEntriesTextView.GetText(true), "Other")

	// loaded on the next start
	ToggleShortcuts()
	shortcutsHidden.Store(false)
	InitShortcutVisibility()
	assert.True(t, ShortcutsHidden())
}

func TestSetCollapsibleRegistersOnce(t *testing.T) {
	collapsibleMaps = nil
	t.Cleanup(func() { collapsibleMaps = nil })

	shortcutMap := NewShortcutMap(tview.NewApplication())
	shortcutMap.SetCollapsible().SetCollapsible()
	assert.Len(t, collapsibleMaps, 1)
}
