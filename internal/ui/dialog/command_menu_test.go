package dialog

import (
	"strings"
	"testing"
	"time"
	"zfs-file-history/internal/testutil"
	"zfs-file-history/internal/ui/shortcut_helper"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// startCommandMenuApp runs an app with a focused list on its pages, to show the command menu on.
func startCommandMenuApp(t *testing.T) (*tview.Application, tcell.SimulationScreen, *tview.Pages, *tview.List) {
	app := tview.NewApplication()
	screen := tcell.NewSimulationScreen("UTF-8")
	app.SetScreen(screen)
	screen.SetSize(80, 24)
	list := tview.NewList().AddItem("entry", "", 0, nil)
	pages := tview.NewPages().AddPage("main", list, true, true)
	app.SetRoot(pages, true).SetFocus(list)
	go func() { _ = app.Run() }()
	t.Cleanup(app.Stop)
	return app, screen, pages, list
}

func menuScreenText(t *testing.T, app *tview.Application, screen tcell.SimulationScreen) string {
	var text strings.Builder
	// on the UI thread: the cells are the ones of the screen, not a copy
	testutil.OnUiThread(t, app, func() {
		screen.Show()
		cells, width, height := screen.GetContents()
		for y := 0; y < height; y++ {
			for x := 0; x < width; x++ {
				if runes := cells[y*width+x].Runes; len(runes) > 0 {
					text.WriteRune(runes[0])
				}
			}
			text.WriteRune('\n')
		}
	})
	return text.String()
}

func typeText(screen tcell.SimulationScreen, text string) {
	for _, r := range text {
		screen.InjectKey(tcell.KeyRune, r, tcell.ModNone)
	}
}

func TestCommandMenu_RunsTheChosenCommandOnceClosed(t *testing.T) {
	app, screen, pages, list := startCommandMenuApp(t)

	ran := make(chan bool, 1)
	commands := []shortcut_helper.ShortcutEntry{
		{Name: "Restore file", KeyCombo: []string{shortcut_helper.Ctrl("r")}, Run: func() { ran <- false }},
		// the focus is back when the command runs, so a dialog it opens returns the focus there as well
		{Name: "Show diff", Run: func() { ran <- list.HasFocus() }},
		// only shortcuts that can be run are commands
		{Name: "Move", KeyCombo: []string{"↑"}},
	}
	testutil.OnUiThread(t, app, func() { ShowCommandMenu(app, pages, commands) })
	app.QueueUpdateDraw(func() {})

	assert.Eventually(t, func() bool {
		text := menuScreenText(t, app, screen)
		return strings.Contains(text, "Restore file") && strings.Contains(text, "ctrl+r") && !strings.Contains(text, "Move")
	}, 3*time.Second, 20*time.Millisecond, "the commands and their keys are listed")

	typeText(screen, "diff")
	assert.Eventually(t, func() bool { return !strings.Contains(menuScreenText(t, app, screen), "Restore file") },
		3*time.Second, 20*time.Millisecond, "only matching commands are listed")
	screen.InjectKey(tcell.KeyEnter, 0, tcell.ModNone)

	select {
	case focused := <-ran:
		assert.True(t, focused, "Show diff ran, with the focus back on the list")
	case <-time.After(3 * time.Second):
		require.Fail(t, "no command ran")
	}
	testutil.OnUiThread(t, app, func() { assert.False(t, pages.HasPage(string(CommandMenuPage))) })
}

func TestCommandMenu_EscClosesWithoutRunning(t *testing.T) {
	app, screen, pages, list := startCommandMenuApp(t)

	ran := false
	commands := []shortcut_helper.ShortcutEntry{{Name: "Refresh", Run: func() { ran = true }}}
	testutil.OnUiThread(t, app, func() { ShowCommandMenu(app, pages, commands) })
	app.QueueUpdateDraw(func() {})
	assert.Eventually(t, func() bool { return strings.Contains(menuScreenText(t, app, screen), "Refresh") },
		3*time.Second, 20*time.Millisecond)

	screen.InjectKey(tcell.KeyEscape, 0, tcell.ModNone)
	assert.Eventually(t, func() bool {
		closed := false
		testutil.OnUiThread(t, app, func() { closed = !pages.HasPage(string(CommandMenuPage)) && list.HasFocus() })
		return closed
	}, 3*time.Second, 20*time.Millisecond)
	testutil.OnUiThread(t, app, func() { assert.False(t, ran) })
}

func TestCommandMenu_MovesTheSelection(t *testing.T) {
	menu := NewCommandMenu([]shortcut_helper.ShortcutEntry{
		{Name: "First", Run: func() {}},
		{Name: "Second", Run: func() {}},
	})
	selected := func() string {
		row, _ := menu.list.GetSelection()
		return menu.matches[row].command.Name
	}
	assert.Equal(t, "First", selected())
	menu.handleKey(tcell.NewEventKey(tcell.KeyDown, 0, tcell.ModNone))
	assert.Equal(t, "Second", selected())
	menu.handleKey(tcell.NewEventKey(tcell.KeyDown, 0, tcell.ModNone))
	assert.Equal(t, "Second", selected(), "stays at the last one")
	menu.handleKey(tcell.NewEventKey(tcell.KeyCtrlP, 0, tcell.ModNone))
	assert.Equal(t, "First", selected())

	menu.filter("zzz")
	menu.handleKey(tcell.NewEventKey(tcell.KeyDown, 0, tcell.ModNone))
	menu.handleKey(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone))
	assert.Nil(t, menu.Chosen(), "nothing to choose without matches")
}

func TestSelectionDialog_OptionCommands(t *testing.T) {
	app := tview.NewApplication()
	options := []*DialogOption{
		{Id: 1, Name: "📸 Create Snapshot"},
		{Id: 2, Name: "📜 Browse history"},
		{Id: DialogCloseActionId, Name: "Close"},
	}
	d := NewSelectionDialog(app, "test", "title", "description", options, nil, nil)

	commands := d.OptionCommands(func(Dialog) {}, 2)
	require.Len(t, commands, 1, "not close and the skipped option")
	assert.Equal(t, "Create Snapshot", commands[0].Name)
	assert.True(t, commands[0].MenuOnly)
	assert.NotNil(t, commands[0].Run)
}
