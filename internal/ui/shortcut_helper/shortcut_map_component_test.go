package shortcut_helper

import (
	"testing"
	"time"
	"zfs-file-history/internal/ui/theme"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewShortcutMap(t *testing.T) {
	app := tview.NewApplication()
	sm := NewShortcutMap(app)

	assert.NotNil(t, sm)
	assert.NotNil(t, sm.GetLayout())
	assert.NotNil(t, sm.bar)
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

	text := formatEntries(sm.bar.entries)
	// Verify formatting contains non-breaking spaces (\u00a0) inside cycle focus name
	assert.Contains(t, text, "Cycle\u00a0focus")
	// Verify multiple combos joined with non-breaking dental click (\u01c0)
	assert.Contains(t, text, "⭾\u01c0shift+⭾")

	sm.Clear()
	assert.Empty(t, sm.bar.entries)
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

	// "[ctrl+q]:\u00a0Quit" (14 characters)
	// availableWidth = 30 - 2 = 28 -> fits (1 line)
	// availableWidth = 10 - 2 = 8 -> cut off, entries are not wrapped (1 line)
	assert.Equal(t, 1, sm.CalculateHeightForWidth(30))
	assert.Equal(t, 1, sm.CalculateHeightForWidth(10))

	// Multi-combo entry with Unicode symbols
	sm.ShortCutEntries = []ShortcutEntry{
		{KeyCombo: []string{"⭾", "shift+⭾"}, Name: "Cycle focus"},
	}
	// "[⭾\u01c0shift+⭾]:\u00a0Cycle\u00a0focus" (24 characters)
	// availableWidth = 30 - 2 = 28 -> fits (1 line)
	// availableWidth = 20 - 2 = 18 -> cut off (1 line)
	assert.Equal(t, 1, sm.CalculateHeightForWidth(30))
	assert.Equal(t, 1, sm.CalculateHeightForWidth(20))
	// a second entry moves to the next line as a whole
	sm.ShortCutEntries = append(sm.ShortCutEntries, ShortcutEntry{KeyCombo: []string{"q"}, Name: "Quit"})
	assert.Equal(t, 1, sm.CalculateHeightForWidth(24+2+9+2))
	assert.Equal(t, 2, sm.CalculateHeightForWidth(24+2+9+2-1))

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

// Drawing in a width the entries wrap differently in reports the new height.
func TestDrawReportsHeightChanges(t *testing.T) {
	app := tview.NewApplication()
	screen := tcell.NewSimulationScreen("UTF-8")
	require.NoError(t, screen.Init())
	app.SetScreen(screen)
	go func() { _ = app.Run() }()
	t.Cleanup(app.Stop)
	sm := NewShortcutMap(app)
	heights := make(chan int, 10)
	sm.SetOnHeightChanged(func(height int) { heights <- height })
	sm.SetEntries([]ShortcutEntry{{KeyCombo: []string{"ctrl+q"}, Name: "Quit"}, {KeyCombo: []string{"q"}, Name: "Other"}})
	<-heights

	sm.GetLayout().SetRect(0, 0, 20, 1)
	sm.GetLayout().Draw(screen)
	select {
	case height := <-heights:
		assert.Equal(t, 2, height)
	case <-time.After(2 * time.Second):
		t.Fatal("no height reported")
	}
}

func TestFormatEntriesGroupsShortcuts(t *testing.T) {
	entries := []ShortcutEntry{
		{KeyCombo: []string{"q"}, Name: "Quit", Group: GroupGlobal},
		{KeyCombo: []string{"↑", "↓"}, Name: "Move", Group: GroupNavigation},
		{KeyCombo: []string{"h"}, Name: "History"},
		{KeyCombo: []string{"F2"}, Name: "Columns", Group: GroupView},
		{KeyCombo: []string{"r"}, Name: "Restore file"},
	}

	// ordered by group (stable within a group), groups separated by a line
	assert.Equal(t,
		"[h]:\u00a0History  [r]:\u00a0Restore\u00a0file  │  [F2]:\u00a0Columns  │  [↑ǀ↓]:\u00a0Move  │  [q]:\u00a0Quit",
		formatEntries(entries))
	assert.Equal(t, "[h]:\u00a0History", formatEntries(entries[2:3]), "no separator for a single group")

	// the keys of each group have their own color
	colors := map[tcell.Color]bool{}
	for _, item := range layoutEntries(entries, 200) {
		for _, segment := range item.segments {
			colors[segment.color] = true
		}
	}
	for _, color := range []tcell.Color{
		theme.Colors.ShortcutMap.KeyCombo, theme.Colors.ShortcutMap.ViewKeyCombo,
		theme.Colors.ShortcutMap.NavigationKeyCombo, theme.Colors.ShortcutMap.GlobalKeyCombo,
		theme.Colors.ShortcutMap.Separator, theme.Colors.ShortcutMap.Name,
	} {
		assert.True(t, colors[color], "%v", color)
	}
	assert.Equal(t, entries[0].Name, "Quit", "the entries are not reordered in place")
}

func TestGroupColorsDiffer(t *testing.T) {
	colors := map[tcell.Color]ShortcutGroup{}
	for _, group := range []ShortcutGroup{GroupAction, GroupView, GroupNavigation, GroupGlobal} {
		previous, exists := colors[group.KeyColor()]
		assert.False(t, exists, "group %d has the color of group %d", group, previous)
		colors[group.KeyColor()] = group
	}
}

func TestMenuOnlyEntriesAreNotShown(t *testing.T) {
	entries := []ShortcutEntry{
		{KeyCombo: []string{"h"}, Name: "History"},
		{KeyCombo: []string{"F2"}, Name: "Columns", Group: GroupView, MenuOnly: true},
		{KeyCombo: []string{"q"}, Name: "Quit", Group: GroupGlobal},
	}
	assert.Equal(t, "[h]: History  │  [q]: Quit", formatEntries(entries),
		"no separator for the group of the hidden entry either")
}

func TestCommands(t *testing.T) {
	run := func() {}
	history := ShortcutEntry{KeyCombo: []string{"h"}, Name: "History"}.WithRun(run)
	columns := ShortcutEntry{KeyCombo: []string{"F2"}, Name: "Columns"}.WithRun(run).OnlyInMenu()
	move := ShortcutEntry{KeyCombo: []string{"↑"}, Name: "Move"}

	commands := Commands([]ShortcutEntry{history, columns, move})
	assert.Len(t, commands, 2, "only the ones that can be run")
	assert.Equal(t, "History", commands[0].Name)
	assert.Equal(t, "Columns", commands[1].Name)
	assert.True(t, commands[1].MenuOnly)
}
