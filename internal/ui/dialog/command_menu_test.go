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
	testutil.OnUiThread(t, app, func() { ShowCommandMenu(app, pages, []CommandSection{{Title: "Section", Commands: commands}}) })
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
	testutil.OnUiThread(t, app, func() { ShowCommandMenu(app, pages, []CommandSection{{Title: "Section", Commands: commands}}) })
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
	menu := NewCommandMenu([]CommandSection{
		{Title: "Files", Commands: []shortcut_helper.ShortcutEntry{{Name: "First", Run: func() {}}}},
		{Title: "Empty", Commands: []shortcut_helper.ShortcutEntry{{Name: "Not runnable"}}},
		{Title: "Global", Commands: []shortcut_helper.ShortcutEntry{{Name: "Second", Run: func() {}}}},
	})
	selected := func() string {
		row, _ := menu.list.GetSelection()
		if menu.rows[row] == nil {
			return "title"
		}
		return menu.rows[row].command.Name
	}
	assert.Len(t, menu.rows, 4, "two sections with a title each, the one without commands is left out")
	assert.Equal(t, "First", selected())
	menu.handleKey(tcell.NewEventKey(tcell.KeyDown, 0, tcell.ModNone))
	assert.Equal(t, "Second", selected(), "the title of the section is skipped")
	menu.handleKey(tcell.NewEventKey(tcell.KeyDown, 0, tcell.ModNone))
	assert.Equal(t, "Second", selected(), "stays at the last one")
	menu.handleKey(tcell.NewEventKey(tcell.KeyCtrlP, 0, tcell.ModNone))
	assert.Equal(t, "First", selected())

	// the best match is selected, even if it is not the first one
	menu.filter("sec")
	assert.Equal(t, "Second", selected())

	menu.filter("zzz")
	menu.handleKey(tcell.NewEventKey(tcell.KeyDown, 0, tcell.ModNone))
	menu.handleKey(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone))
	assert.Nil(t, menu.Chosen(), "nothing to choose without matches")
}

func TestCommandMenu_ShowsSectionsAndDescriptions(t *testing.T) {
	app, screen, pages, _ := startCommandMenuApp(t)
	sections := []CommandSection{
		{Title: "Files", Commands: []shortcut_helper.ShortcutEntry{
			{Name: "Columns", Description: "Choose the shown columns", KeyCombo: []string{"F2"}, Run: func() {}},
		}},
		{Title: "Global", Commands: []shortcut_helper.ShortcutEntry{{Name: "Quit", Run: func() {}}}},
	}
	testutil.OnUiThread(t, app, func() { ShowCommandMenu(app, pages, sections) })
	app.QueueUpdateDraw(func() {})

	assert.Eventually(t, func() bool {
		lines := strings.Split(menuScreenText(t, app, screen), "\n")
		has := func(parts ...string) bool {
			for _, line := range lines {
				found := true
				for _, part := range parts {
					found = found && strings.Contains(line, part)
				}
				if found {
					return true
				}
			}
			return false
		}
		return has("Files") && has("Columns", "Choose the shown columns", "F2") && has("Global") && has("Quit")
	}, 3*time.Second, 20*time.Millisecond, "titles, and the description and keys in the row of the command")
}

func TestSelectionDialog_OptionCommands(t *testing.T) {
	app := tview.NewApplication()
	options := []*DialogOption{
		{Id: 1, Name: "📸 Create Snapshot", Description: "Snapshot the dataset now"},
		{Id: 2, Name: "📜 Browse history"},
		{Id: DialogCloseActionId, Name: "Close"},
	}
	d := NewSelectionDialog(app, "test", "title", "description", options, nil, nil)

	commands := d.OptionCommands(func(Dialog) {}, 2)
	require.Len(t, commands, 1, "not close and the skipped option")
	assert.Equal(t, "Create Snapshot", commands[0].Name)
	assert.Equal(t, "Snapshot the dataset now", commands[0].Description)
	assert.True(t, commands[0].MenuOnly)
	assert.NotNil(t, commands[0].Run)
}
