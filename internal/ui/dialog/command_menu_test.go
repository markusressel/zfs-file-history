package dialog

import (
	"slices"
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
	testutil.OnUiThread(t, app, func() {
		ShowCommandMenu(app, pages, []CommandSection{{Title: "Section", Commands: commands}}, &CommandMenuMemory{})
	})
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
	testutil.OnUiThread(t, app, func() {
		ShowCommandMenu(app, pages, []CommandSection{{Title: "Section", Commands: commands}}, &CommandMenuMemory{})
	})
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

// Regression: clicking a command focused the list, so Esc and typing did nothing anymore.
func TestCommandMenu_ClickKeepsTheFocusOnTheQuery(t *testing.T) {
	app, screen, pages, list := startCommandMenuApp(t)

	ran := false
	menu := NewCommandMenu([]CommandSection{{Title: "Section", Commands: []shortcut_helper.ShortcutEntry{
		{Name: "Refresh", Run: func() { ran = true }},
		{Name: "Show diff", Run: func() { ran = true }},
	}}})
	testutil.OnUiThread(t, app, func() { ShowDialogOnPages(app, pages, menu, func() {}) })
	app.QueueUpdateDraw(func() {})
	var lines []string
	require.Eventually(t, func() bool {
		lines = strings.Split(menuScreenText(t, app, screen), "\n")
		return slices.ContainsFunc(lines, func(line string) bool { return strings.Contains(line, "Show diff") })
	}, 3*time.Second, 20*time.Millisecond)
	clickY := slices.IndexFunc(lines, func(line string) bool { return strings.Contains(line, "Show diff") })
	clickX := len([]rune(lines[clickY][:strings.Index(lines[clickY], "Show")]))

	screen.InjectMouse(clickX, clickY, tcell.Button1, tcell.ModNone)
	screen.InjectMouse(clickX, clickY, tcell.ButtonNone, tcell.ModNone)
	require.Eventually(t, func() bool {
		selected := ""
		testutil.OnUiThread(t, app, func() { selected = menu.list.GetCell(menu.list.GetSelection()).Text })
		return strings.Contains(selected, "Show")
	}, 3*time.Second, 20*time.Millisecond, "the click selects the command")
	testutil.OnUiThread(t, app, func() { assert.True(t, menu.input.HasFocus(), "the query keeps the focus") })

	// typing still filters
	typeText(screen, "ref")
	assert.Eventually(t, func() bool { return !strings.Contains(menuScreenText(t, app, screen), "Show diff") },
		3*time.Second, 20*time.Millisecond)

	// Esc still closes
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
	testutil.OnUiThread(t, app, func() { ShowCommandMenu(app, pages, sections, &CommandMenuMemory{}) })
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

// listedNames returns the names of the listed commands.
func listedNames(menu *CommandMenu) []string {
	var names []string
	for _, match := range menu.rows {
		if match != nil {
			names = append(names, match.command.Name)
		}
	}
	return names
}

// While nothing is searched, the commands of the view are listed, not the ones only for searching; an entry with a
// query leads to them.
func TestCommandMenu_SearchOnlyCommands(t *testing.T) {
	sorted := ""
	menu := NewCommandMenu([]CommandSection{{Title: "Files", Commands: []shortcut_helper.ShortcutEntry{
		{Name: "Folder history", Run: func() {}},
		{Name: "Sort by…", Query: "sort"},
		{Name: "Sort by Name", Run: func() { sorted = "name" }, SearchOnly: true},
		{Name: "Sort by Size", Run: func() { sorted = "size" }, SearchOnly: true},
	}}})
	assert.Equal(t, []string{"Folder history", "Sort by…"}, listedNames(menu))

	menu.input.SetText("size")
	assert.Equal(t, []string{"Sort by Size"}, listedNames(menu), "found by searching")

	menu.input.SetText("")
	menu.choose(slices.IndexFunc(menu.rows, func(match *commandMatch) bool { return match != nil && match.command.Query != "" }))
	assert.Nil(t, menu.Chosen(), "the menu stays")
	assert.Equal(t, "sort ", menu.input.GetText())
	assert.Equal(t, []string{"Sort by Name", "Sort by Size"}, listedNames(menu), "not itself")

	row, _ := menu.list.GetSelection()
	menu.choose(row)
	require.NotNil(t, menu.Chosen())
	menu.Chosen().Run()
	assert.Equal(t, "name", sorted)
}

// Switches are listed by what they switch, with their state.
func TestCommandMenu_Switches(t *testing.T) {
	hidden := true
	menu := NewCommandMenu([]CommandSection{{Title: "Datasets", Commands: []shortcut_helper.ShortcutEntry{
		{Name: "Filter", Run: func() {}},
		shortcut_helper.ShortcutEntry{Name: "Show unmounted", Description: "Show them as well", Run: func() {}}.
			AsSwitch("Hide unmounted datasets", "Hide the datasets that are not mounted", func() bool { return hidden }),
		shortcut_helper.ShortcutEntry{Name: "Tree view", Run: func() {}}.
			AsSwitch("Tree view", "Children below their parent", func() bool { return false }),
	}}})
	cell := func(row int) string { return testutil.StripTags(menu.list.GetCell(row, 0).Text) }
	assert.Equal(t, "    Filter  ", cell(1), "indented like the names of the switches")
	assert.Equal(t, "  ● Hide unmounted datasets  ", cell(2))
	assert.Equal(t, "  ○ Tree view  ", cell(3))
	assert.Equal(t, "Hide the datasets that are not mounted", menu.list.GetCell(2, 2).Text)

	menu.input.SetText("unmounted")
	assert.Equal(t, []string{"Hide unmounted datasets"}, listedNames(menu))
}

func TestMenuSize(t *testing.T) {
	tests := []struct {
		name                       string
		contentRows, contentWidth  int
		screenWidth, screenHeight  int
		expectedRows, expectedWide int
	}{
		{name: "fits", contentRows: 6, contentWidth: 60, screenWidth: 120, screenHeight: 40, expectedRows: 6, expectedWide: 64},
		{name: "at least 10 rows and 100 columns", contentRows: 40, contentWidth: 200, screenWidth: 120, screenHeight: 16, expectedRows: 10, expectedWide: 100},
		{name: "half of a high screen, 3/4 of a wide one", contentRows: 40, contentWidth: 200, screenWidth: 200, screenHeight: 60, expectedRows: 30, expectedWide: 150},
		{name: "small screen: all of it", contentRows: 40, contentWidth: 200, screenWidth: 60, screenHeight: 10, expectedRows: 7, expectedWide: 60},
		{name: "tiny screen: a row", contentRows: 40, contentWidth: 200, screenWidth: 20, screenHeight: 3, expectedRows: 1, expectedWide: 20},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			rows, width := menuSize(test.contentRows, test.contentWidth, test.screenWidth, test.screenHeight)
			assert.Equal(t, test.expectedRows, rows)
			assert.Equal(t, test.expectedWide, width)
		})
	}
}

// Space switches the selected switch without closing the menu, while nothing is searched or after the selection was
// moved; otherwise it is typed, as it separates the terms of the query.
func TestCommandMenu_SpaceSwitches(t *testing.T) {
	hidden, tree := false, false
	menu := NewCommandMenu([]CommandSection{{Title: "Datasets", Commands: []shortcut_helper.ShortcutEntry{
		shortcut_helper.ShortcutEntry{Name: "Hide unmounted", Run: func() { hidden = !hidden }}.
			AsSwitch("Hide unmounted datasets", "", func() bool { return hidden }),
		shortcut_helper.ShortcutEntry{Name: "Tree view", Run: func() { tree = !tree }}.
			AsSwitch("Tree view", "", func() bool { return tree }),
		{Name: "Filter", Run: func() {}},
	}}})
	space := func() *tcell.EventKey { return menu.handleKey(tcell.NewEventKey(tcell.KeyRune, ' ', tcell.ModNone)) }
	cell := func(row int) string { return testutil.StripTags(menu.list.GetCell(row, 0).Text) }

	assert.Nil(t, space(), "nothing searched: switched")
	assert.True(t, hidden)
	assert.Nil(t, menu.Chosen(), "the menu stays")
	assert.Equal(t, "  ● Hide unmounted datasets  ", cell(1))
	assert.Equal(t, "Space: switch on/off", menu.hint.GetText())

	menu.input.SetText("tree")
	assert.NotNil(t, space(), "typed into the query")
	assert.False(t, tree)
	assert.Empty(t, menu.hint.GetText())

	menu.handleKey(tcell.NewEventKey(tcell.KeyDown, 0, tcell.ModNone))
	assert.Nil(t, space(), "after moving the selection: switched")
	assert.True(t, tree)

	menu.input.SetText("")
	menu.handleKey(tcell.NewEventKey(tcell.KeyDown, 0, tcell.ModNone))
	menu.handleKey(tcell.NewEventKey(tcell.KeyDown, 0, tcell.ModNone))
	assert.NotNil(t, space(), "not a switch")
}

// The command selected when the menu was closed is selected again when it is shown again, e.g. to switch a setting
// back; also if its name is the one of a switch.
func TestCommandMenu_RemembersTheSelection(t *testing.T) {
	app, screen, pages, _ := startCommandMenuApp(t)
	hidden := false
	sections := func() []CommandSection {
		return []CommandSection{{Title: "Datasets", Commands: []shortcut_helper.ShortcutEntry{
			{Name: "Filter", Run: func() {}},
			shortcut_helper.ShortcutEntry{Name: "Hide unmounted", Run: func() { hidden = !hidden }}.
				AsSwitch("Hide unmounted datasets", "", func() bool { return hidden }),
		}}}
	}
	memory := &CommandMenuMemory{}
	frontPage := func() (name string) {
		testutil.OnUiThread(t, app, func() { name, _ = pages.GetFrontPage() })
		return name
	}
	waitFor := func(page string) {
		require.Eventually(t, func() bool { return frontPage() == page }, 3*time.Second, 20*time.Millisecond)
	}

	testutil.OnUiThread(t, app, func() { ShowCommandMenu(app, pages, sections(), memory) })
	waitFor(string(CommandMenuPage))
	screen.InjectKey(tcell.KeyDown, 0, tcell.ModNone)
	screen.InjectKey(tcell.KeyEnter, 0, tcell.ModNone)
	waitFor("main")
	require.Eventually(t, func() bool {
		var on bool
		testutil.OnUiThread(t, app, func() { on = hidden })
		return on
	}, 3*time.Second, 20*time.Millisecond)

	var menu *CommandMenu
	testutil.OnUiThread(t, app, func() {
		menu = NewCommandMenu(sections())
		menu.selectCommand(memory.section, memory.name)
	})
	var selected string
	testutil.OnUiThread(t, app, func() {
		row, _ := menu.list.GetSelection()
		selected = menu.rows[row].command.Name
	})
	assert.Equal(t, "Hide unmounted datasets", selected)

	other := &CommandMenuMemory{section: "Files", name: "Hide unmounted datasets"}
	testutil.OnUiThread(t, app, func() {
		menu = NewCommandMenu(sections())
		menu.selectCommand(other.section, other.name)
		row, _ := menu.list.GetSelection()
		selected = menu.rows[row].command.Name
	})
	assert.Equal(t, "Filter", selected, "another section: the first command")
}

// Regression: the width of the mark of switches was counted in bytes (3), not columns (1).
func TestCommandMenu_ContentWidthWithSwitches(t *testing.T) {
	menu := NewCommandMenu([]CommandSection{{Title: "S", Commands: []shortcut_helper.ShortcutEntry{
		shortcut_helper.ShortcutEntry{Name: "x", KeyCombo: []string{"u"}, Run: func() {}}.AsSwitch("Tree", "Desc", func() bool { return false }),
	}}})
	// "  ○ Tree  " + gap + "u  " + gap + "Desc"
	_, width := menu.contentSize()
	assert.Equal(t, 10+1+3+1+4, width)
}

// The keys of the commands of an unfocused component are dimmed: they only work once it has the focus.
func TestCommandMenu_UnfocusedKeysAreDimmed(t *testing.T) {
	menu := NewCommandMenu([]CommandSection{
		{Title: "Files", Commands: []shortcut_helper.ShortcutEntry{{Name: "Filter", KeyCombo: []string{"/"}, Group: shortcut_helper.GroupView, Run: func() {}}}},
		{Title: "Snapshots", Unfocused: true, Commands: []shortcut_helper.ShortcutEntry{{Name: "Filter", KeyCombo: []string{"/"}, Group: shortcut_helper.GroupView, Run: func() {}}}},
	})
	keyColor := func(row int) tcell.Color {
		color, _, _ := menu.list.GetCell(row, 1).Style.Decompose()
		return color
	}
	assert.Equal(t, shortcut_helper.GroupView.KeyColor(), keyColor(1))
	assert.Equal(t, tcell.ColorGray, keyColor(3))
}
