package shortcut_helper

import (
	"testing"
	"zfs-file-history/internal/ui/theme"

	"github.com/rivo/tview"
	"github.com/stretchr/testify/assert"
)

func TestNewShortcutMap(t *testing.T) {
	app := tview.NewApplication()
	sm := NewShortcutMap(app)

	assert.NotNil(t, sm)
	assert.NotNil(t, sm.GetLayout())
	assert.NotNil(t, sm.shortcutEntriesTextView)
}

func TestSetOnHeightChanged(t *testing.T) {
	app := tview.NewApplication()
	sm := NewShortcutMap(app)

	heightCalled := -1
	sm.SetOnHeightChanged(func(height int) {
		heightCalled = height
	})

	assert.NotNil(t, sm.onHeightChanged)

	entries := []ShortcutEntry{
		{KeyCombo: []string{"ctrl+q"}, Name: "Quit"},
	}

	sm.SetEntries(entries)
	assert.Greater(t, heightCalled, 0)
}

func TestSetEntriesAndClear(t *testing.T) {
	// Setup custom theme colors if needed (or fallback to default theme color struct)
	theme.Colors.ShortcutMap.KeyCombo = 0
	theme.Colors.ShortcutMap.Name = 0

	app := tview.NewApplication()
	sm := NewShortcutMap(app)

	entries := []ShortcutEntry{
		{KeyCombo: []string{"⭾", "shift+⭾"}, Name: "Cycle focus"},
		{KeyCombo: []string{"ctrl+q"}, Name: "Quit"},
	}

	sm.SetEntries(entries)

	assert.Equal(t, entries, sm.ShortCutEntries)

	text := sm.shortcutEntriesTextView.GetText(false)
	// Verify formatting contains non-breaking spaces (\u00a0) inside cycle focus name
	assert.Contains(t, text, "Cycle\u00a0focus")
	// Verify multiple combos joined with non-breaking dental click (\u01c0)
	assert.Contains(t, text, "⭾\u01c0shift+⭾")

	sm.Clear()
	assert.Empty(t, sm.shortcutEntriesTextView.GetText(false))
}

func TestCalculateHeightForWidth(t *testing.T) {
	app := tview.NewApplication()
	sm := NewShortcutMap(app)

	// No entries
	sm.ShortCutEntries = nil
	assert.Equal(t, 1, sm.CalculateHeightForWidth(80))

	// Short entry
	sm.ShortCutEntries = []ShortcutEntry{
		{KeyCombo: []string{"ctrl+q"}, Name: "Quit"},
	}

	// "[ctrl+q]:\u00a0Quit  " -> trimmed: "[ctrl+q]:\u00a0Quit" (14 runes)
	// availableWidth = 30 - 2 = 28 -> fits (1 line)
	// availableWidth = 10 - 2 = 8 -> wraps (2 lines because word length 14 > 8)
	assert.Equal(t, 1, sm.CalculateHeightForWidth(30))
	assert.Equal(t, 2, sm.CalculateHeightForWidth(10))

	// Multi-combo entry with Unicode symbols
	sm.ShortCutEntries = []ShortcutEntry{
		{KeyCombo: []string{"⭾", "shift+⭾"}, Name: "Cycle focus"},
	}
	// "[⭾\u01c0shift+⭾]:\u00a0Cycle\u00a0focus  " -> trimmed length: 24 runes (including multi-byte symbols counted as 1)
	// availableWidth = 30 - 2 = 28 -> fits (1 line)
	// availableWidth = 20 - 2 = 18 -> wraps (2 lines because word length 24 > 18)
	assert.Equal(t, 1, sm.CalculateHeightForWidth(30))
	assert.Equal(t, 2, sm.CalculateHeightForWidth(20))

	// Long list of entries
	sm.ShortCutEntries = []ShortcutEntry{
		{KeyCombo: []string{"⭾", "shift+⭾"}, Name: "Cycle focus"},
		{KeyCombo: []string{"F5"}, Name: "Refresh"},
		{KeyCombo: []string{"ctrl+q"}, Name: "Quit"},
		{KeyCombo: []string{"↑", "k"}, Name: "Move up"},
		{KeyCombo: []string{"↓", "j"}, Name: "Move down"},
	}

	// With very large terminal width, should fit in 1 line
	assert.Equal(t, 1, sm.CalculateHeightForWidth(200))
	// With narrow terminal width, should wrap into multiple lines
	assert.Greater(t, sm.CalculateHeightForWidth(40), 1)
}

func TestCalculateHeightFromTerminal(t *testing.T) {
	app := tview.NewApplication()
	sm := NewShortcutMap(app)

	sm.ShortCutEntries = []ShortcutEntry{
		{KeyCombo: []string{"ctrl+q"}, Name: "Quit"},
	}

	// Height from terminal is calculated. Even if it falls back to 80 width,
	// it should return a valid height >= 1
	height := sm.CalculateHeightFromTerminal()
	assert.GreaterOrEqual(t, height, 1)
}

func TestDrawFuncHeightResize(t *testing.T) {
	app := tview.NewApplication()
	sm := NewShortcutMap(app)

	sm.SetOnHeightChanged(func(height int) {
		t.Logf("Height changed to %d", height)
	})

	sm.ShortCutEntries = []ShortcutEntry{
		{KeyCombo: []string{"ctrl+q"}, Name: "Quit"},
	}

	drawFunc := sm.shortcutEntriesTextView.GetDrawFunc()
	assert.NotNil(t, drawFunc)

	// Invoke the DrawFunc to trigger size verification logic
	drawFunc(nil, 0, 0, 8, 1)
}
