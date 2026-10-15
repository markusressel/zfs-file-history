package shortcut_helper

import (
	"testing"
	"zfs-file-history/internal/testutil"
	"zfs-file-history/internal/ui/theme"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLayoutEntries(t *testing.T) {
	entries := []ShortcutEntry{
		{KeyCombo: []string{"h"}, Name: "History"},
		{KeyCombo: []string{"q"}, Name: "Quit", Group: GroupGlobal},
	}
	// "[h]: History  │  [q]: Quit"
	items := layoutEntries(entries, 100)
	require.Len(t, items, 3)
	assert.Equal(t, [3]int{0, 0, 12}, [3]int{items[0].line, items[0].x, items[0].width})
	assert.Nil(t, items[1].entry, "the separator")
	assert.Equal(t, 14, items[1].x)
	assert.Equal(t, 17, items[2].x)
	assert.Equal(t, 1, lineCount(items))

	// the separator fits, the entry after it does not
	items = layoutEntries(entries, 20)
	assert.Equal(t, [2]int{0, 14}, [2]int{items[1].line, items[1].x})
	assert.Equal(t, [2]int{1, 0}, [2]int{items[2].line, items[2].x})
	assert.Equal(t, 2, lineCount(items))

	assert.Equal(t, 1, lineCount(layoutEntries(nil, 20)))
}

// newTestBar draws a bar of 40 columns with the entries, recording the pressed keys.
func newTestBar(t *testing.T, entries []ShortcutEntry) (*shortcutBar, tcell.SimulationScreen, *[]*tcell.EventKey) {
	screen := testutil.NewScreen(t, 40, 2)
	bar := newShortcutBar(testutil.App())
	var pressed []*tcell.EventKey
	bar.pressKey = func(event *tcell.EventKey) { pressed = append(pressed, event) }
	bar.setEntries(entries)
	bar.SetRect(0, 0, 40, 2)
	bar.Draw(screen)
	return bar, screen, &pressed
}

func mouseAt(bar *shortcutBar, action tview.MouseAction, x int, y int) (bool, tview.Primitive) {
	event := tcell.NewEventMouse(x, y, tcell.ButtonNone, tcell.ModNone)
	return bar.MouseHandler()(action, event, func(tview.Primitive) { panic("the bar does not take the focus") })
}

func backgroundAt(screen tcell.SimulationScreen, x int, y int) tcell.Color {
	_, style, _ := screen.Get(x, y)
	_, background, _ := style.Decompose()
	return background
}

// The bar is drawn with a padding of 1: "[1ǀ2ǀ3]: Go to page  [h]: History" starts at column 1.
func TestShortcutBar_ClickPressesTheKey(t *testing.T) {
	ran := false
	entries := []ShortcutEntry{
		{KeyCombo: []string{"1", "2", "3"}, Name: "Go to page"},
		{KeyCombo: []string{"h"}, Name: "History"},
		{Name: "Only runs"},
		{KeyCombo: []string{"1-9"}, Name: "Runs", Run: func() { ran = true }},
	}
	bar, _, pressed := newTestBar(t, entries)

	// the name of an entry: its first key
	consumed, _ := mouseAt(bar, tview.MouseLeftClick, 13, 0)
	assert.True(t, consumed)
	require.Len(t, *pressed, 1)
	assert.Equal(t, '1', (*pressed)[0].Rune())

	// one of several keys: that one
	mouseAt(bar, tview.MouseLeftClick, 4, 0)
	require.Len(t, *pressed, 2)
	assert.Equal(t, '2', (*pressed)[1].Rune())

	// "[h]: History" at 22
	mouseAt(bar, tview.MouseLeftClick, 25, 0)
	require.Len(t, *pressed, 3)
	assert.Equal(t, 'h', (*pressed)[2].Rune())

	// between the entries: nothing
	mouseAt(bar, tview.MouseLeftClick, 21, 0)
	assert.Len(t, *pressed, 3)

	// no key that can be pressed: runs it, if it can be run
	items := bar.layout()
	require.Len(t, items, 4)
	bar.click(items[2].entry, -1)
	assert.False(t, ran)
	bar.click(items[3].entry, -1)
	assert.True(t, ran)
	assert.Len(t, *pressed, 3)
}

func TestShortcutBar_Hover(t *testing.T) {
	entries := []ShortcutEntry{
		{KeyCombo: []string{"1", "2", "3"}, Name: "Go to page"},
		{KeyCombo: []string{"h"}, Name: "History"},
	}
	bar, screen, _ := newTestBar(t, entries)
	hover := theme.Colors.ShortcutMap.HoverBackground
	require.NotEqual(t, hover, backgroundAt(screen, 25, 0))

	// the whole entry
	consumed, capture := mouseAt(bar, tview.MouseMove, 25, 0)
	assert.True(t, consumed, "redrawn")
	assert.Equal(t, bar, capture, "captures the mouse, to know when it leaves")
	bar.Draw(screen)
	for x := 22; x < 34; x++ {
		assert.Equal(t, hover, backgroundAt(screen, x, 0), "column %d", x)
	}
	assert.NotEqual(t, hover, backgroundAt(screen, 21, 0))

	// one of several keys: only that key
	mouseAt(bar, tview.MouseMove, 4, 0)
	bar.Draw(screen)
	assert.Equal(t, hover, backgroundAt(screen, 4, 0))
	assert.NotEqual(t, hover, backgroundAt(screen, 2, 0))
	assert.NotEqual(t, hover, backgroundAt(screen, 25, 0))

	// left the bar
	consumed, capture = mouseAt(bar, tview.MouseMove, 4, 5)
	assert.True(t, consumed)
	assert.Nil(t, capture)
	bar.Draw(screen)
	assert.NotEqual(t, hover, backgroundAt(screen, 4, 0))
}

func TestShortcutBar_Disabled(t *testing.T) {
	bar, _, pressed := newTestBar(t, []ShortcutEntry{{KeyCombo: []string{"h"}, Name: "History"}})
	bar.enabled = func() bool { return false }

	mouseAt(bar, tview.MouseMove, 2, 0)
	mouseAt(bar, tview.MouseLeftClick, 2, 0)
	_, hovered := bar.regions.Hovered()
	assert.False(t, hovered)
	assert.Empty(t, *pressed)
}

func TestShortcutBar_NewEntriesEndTheHover(t *testing.T) {
	bar, _, _ := newTestBar(t, []ShortcutEntry{{KeyCombo: []string{"h"}, Name: "History"}})
	mouseAt(bar, tview.MouseMove, 2, 0)
	_, hovered := bar.regions.Hovered()
	require.True(t, hovered)

	bar.setEntries([]ShortcutEntry{{KeyCombo: []string{"q"}, Name: "Quit"}})
	_, hovered = bar.regions.Hovered()
	assert.False(t, hovered)
}
