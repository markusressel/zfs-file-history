package dialog

import (
	"slices"
	"strings"
	"zfs-file-history/internal/ui/shortcut_helper"
	"zfs-file-history/internal/ui/theme"
	"zfs-file-history/internal/ui/txwidgets"
	"zfs-file-history/internal/ui/util"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

const (
	CommandMenuPage util.Page = "CommandMenu"
	// commandMenuMinRows is the number of rows the menu may always take (if the screen is high enough and there are
	// as many commands), commandMenuMaxHeightPercent how much of a higher screen: the page stays visible above it.
	// The other commands are scrolled to.
	commandMenuMinRows          = 10
	commandMenuMaxHeightPercent = 50
	// commandMenuMinWidth is the width the menu may always take (if the screen is wide enough and the commands need
	// it), commandMenuMaxWidthPercent how much of a wider screen. Descriptions that do not fit are cut off.
	commandMenuMinWidth        = 100
	commandMenuMaxWidthPercent = 75
	// commandMenuChromeHeight and commandMenuChromeWidth are the lines and columns around the list: the border, the
	// query below the list, the padding
	commandMenuChromeHeight = 3
	commandMenuChromeWidth  = 4
)

// CommandSection is a group of commands in the command menu, e.g. the ones of the focused list, shown below its
// title.
type CommandSection struct {
	Title    string
	Commands []shortcut_helper.ShortcutEntry
}

// CommandMenu lists commands (shortcut entries that can be run, see shortcut_helper.ShortcutEntry.Run) and filters
// them while typing, like the command line of Vim. It is shown at the bottom of the screen, above the page, so the
// layout of the page does not change. Show it with ShowCommandMenu.
//
// The commands are listed in sections (e.g. the focused list, the page, global), each row with the name, the keys
// (in the color of the group, like in the shortcut map) and the description (dimmed). While filtering, the
// sections stay, sorted by how well their commands match, and the best match is selected.
//
// While nothing is searched, it lists the commands of the shown views, not the SearchOnly ones (e.g. one per column
// to sort by), which an entry with a query leads to (e.g. "Sort by…"). Switches (see ShortcutEntry.On) show whether
// they are on, so options can be found here like settings. Space switches the selected one without closing the menu,
// while nothing is searched or after the selection was moved (otherwise it is typed: it separates the terms).
type CommandMenu struct {
	layout        *tview.Flex
	input         *tview.InputField
	list          *tview.Table
	actionChannel chan DialogActionId

	sections []CommandSection
	// rows are the matches shown in the list, by row; nil for the titles of the sections
	rows []*commandMatch
	// chosen is the command to run once the menu is closed, nil if it was closed without choosing one
	chosen *shortcut_helper.ShortcutEntry
	// hasSwitches is set if a command is a switch: the names of the others are indented by the width of its state
	hasSwitches bool
	// contentRows and contentWidth are the size the list needs to show all commands (also the ones only shown while
	// searching), without scrolling or cutting off descriptions, see menuSize
	contentRows, contentWidth int
	// pageRows is the number of rows shown, by which PgUp and PgDn move. Only accessed on the UI thread.
	pageRows int
	// moved is set once the selection was moved (keys or mouse) after the query changed: Space switches then, see
	// spaceSwitches
	moved bool
	// hint tells in the border that Space switches the selected command, see updateHint
	hint *util.BorderFooter
}

// CommandMenuMemory is the command selected when the command menu was closed the last time, selected again when it
// is shown again (e.g. to switch a setting back). Kept while the application runs, not saved. Only accessed on the
// UI thread.
type CommandMenuMemory struct {
	// section and name identify the command: its section (e.g. "Datasets") and name (the name of a switch, which
	// does not change with its state)
	section, name string
}

// remember remembers the selected command of the menu, nothing if none is selected.
func (memory *CommandMenuMemory) remember(menu *CommandMenu) {
	row, _ := menu.list.GetSelection()
	if row < 0 || row >= len(menu.rows) || menu.rows[row] == nil {
		return
	}
	memory.section, memory.name = menu.rows[row].section, menu.rows[row].command.Name
}

// ShowCommandMenu shows a menu of the commands, in the order of the sections (e.g. the focused component first),
// with the command remembered in memory (if listed) selected. The chosen command is run after the menu is closed,
// once the focus is back where it was, so a dialog opened by the command returns the focus there as well. Must be
// called on the UI thread.
func ShowCommandMenu(application *tview.Application, pages *tview.Pages, sections []CommandSection, memory *CommandMenuMemory) {
	menu := NewCommandMenu(sections)
	menu.selectCommand(memory.section, memory.name)
	ShowDialogOnPages(application, pages, menu, func() {
		memory.remember(menu)
		if menu.chosen != nil {
			menu.chosen.Run()
		}
	})
}

// NewCommandMenu creates the menu of the commands that can be run, see ShowCommandMenu. Sections without commands
// are left out.
func NewCommandMenu(sections []CommandSection) *CommandMenu {
	menu := &CommandMenu{actionChannel: make(chan DialogActionId)}
	for _, section := range sections {
		commands := shortcut_helper.Commands(section.Commands)
		for i, command := range commands {
			if command.On != nil {
				// found and shown by what it switches, not by what its key does now
				commands[i].Name, commands[i].Description = command.SwitchName, command.SwitchDescription
				menu.hasSwitches = true
			}
		}
		if len(commands) > 0 {
			menu.sections = append(menu.sections, CommandSection{Title: section.Title, Commands: commands})
		}
	}
	menu.createLayout()
	menu.filter("")
	return menu
}

func (menu *CommandMenu) createLayout() {
	menu.list = tview.NewTable().
		SetSelectable(true, false).
		SetSelectedStyle(tcell.StyleDefault.
			Foreground(theme.Colors.Layout.Table.SelectedForeground).
			Background(theme.Colors.Layout.Table.SelectedBackground))
	menu.list.SetSelectedFunc(func(row, column int) { menu.choose(row) })
	menu.list.SetSelectionChangedFunc(func(row, column int) { menu.updateHint() })
	util.IgnoreClicksBelowRows(menu.list)
	ignoreClicksBelowRows := menu.list.GetMouseCapture()
	menu.list.SetMouseCapture(func(action tview.MouseAction, event *tcell.EventMouse) (tview.MouseAction, *tcell.EventMouse) {
		if action == tview.MouseLeftClick {
			menu.moved = true
		}
		if action == tview.MouseLeftDown {
			// the query keeps the focus (tview focuses the list when pressed), so typing and Esc keep working after
			// clicking a command. The click still selects it, or runs it if it is selected already.
			return tview.MouseConsumed, nil
		}
		return ignoreClicksBelowRows(action, event)
	})

	menu.input = tview.NewInputField().
		SetLabel(":").
		SetFieldBackgroundColor(tview.Styles.PrimitiveBackgroundColor).
		SetChangedFunc(menu.filter)
	menu.input.SetInputCapture(menu.handleKey)

	content := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(menu.list, 0, 1, false).
		AddItem(menu.input, 1, 0, true)

	frame := tview.NewFlex()
	frame.SetBorder(true)
	util.SetupDialogWindow(frame, " Commands ")
	clearInside(frame.Box)
	menu.hint = util.NewBorderFooter(frame.Box)
	frame.SetBorderPadding(0, 0, 1, 1)
	frame.AddItem(content, 0, 1, true)

	// the nil items keep the page visible around the menu (see isOnDialog)
	columns := tview.NewFlex().
		AddItem(nil, 0, 1, false).
		AddItem(frame, 0, 0, true).
		AddItem(nil, 0, 1, false)
	menu.contentRows, menu.contentWidth = menu.contentSize()
	menu.pageRows = commandMenuMinRows
	menu.layout = tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(nil, 0, 1, false).
		AddItem(columns, 0, 0, true)
	// sized by the screen on every draw, so a resized terminal is used as well
	menu.layout.SetDrawFunc(func(screen tcell.Screen, x, y, width, height int) (int, int, int, int) {
		rows, frameWidth := menuSize(menu.contentRows, menu.contentWidth, width, height)
		menu.pageRows = rows
		menu.layout.ResizeItem(columns, rows+commandMenuChromeHeight, 0)
		columns.ResizeItem(frame, frameWidth, 0)
		return x, y, width, height
	})
}

// contentSize returns the rows (with the titles of the sections) and the width the list needs for all commands,
// with all columns as wide as their widest cell (see setCommandRow).
func (menu *CommandMenu) contentSize() (rows int, width int) {
	indent := 0
	if menu.hasSwitches {
		// the mark and a space, see setCommandRow
		indent = tview.TaggedStringWidth(switchOn) + 1
	}
	var nameWidth, keysWidth, descriptionWidth int
	for _, section := range menu.sections {
		rows += 1 + len(section.Commands)
		nameWidth = max(nameWidth, tview.TaggedStringWidth(tview.Escape(section.Title)))
		for _, command := range section.Commands {
			nameWidth = max(nameWidth, 2+indent+tview.TaggedStringWidth(tview.Escape(command.Name))+2)
			keysWidth = max(keysWidth, tview.TaggedStringWidth(tview.Escape(strings.Join(command.KeyCombo, " ")))+2)
			descriptionWidth = max(descriptionWidth, tview.TaggedStringWidth(tview.Escape(command.Description)))
		}
	}
	// tview leaves a column between the columns of a table
	return rows, nameWidth + 1 + keysWidth + 1 + descriptionWidth
}

// menuSize returns the rows of the list and the width of the menu (with its border) on a screen of the size: what
// the content needs, but at most commandMenuMinRows or commandMenuMaxHeightPercent of the screen (the larger), and
// commandMenuMinWidth or commandMenuMaxWidthPercent of it. On small screens, all of the screen, but at least a row.
func menuSize(contentRows int, contentWidth int, screenWidth int, screenHeight int) (rows int, width int) {
	maxRows := max(commandMenuMinRows, screenHeight*commandMenuMaxHeightPercent/100)
	rows = min(contentRows, maxRows, screenHeight-commandMenuChromeHeight)
	maxWidth := max(commandMenuMinWidth, screenWidth*commandMenuMaxWidthPercent/100)
	width = min(contentWidth+commandMenuChromeWidth, maxWidth, screenWidth)
	return max(rows, 1), width
}

// handleKey moves the selection in the list while the query is typed, runs the selected command with Enter and
// closes the menu with Esc.
func (menu *CommandMenu) handleKey(event *tcell.EventKey) *tcell.EventKey {
	switch event.Key() {
	case tcell.KeyEscape:
		menu.Close()
	case tcell.KeyEnter:
		row, _ := menu.list.GetSelection()
		menu.choose(row)
	case tcell.KeyRune:
		if event.Rune() != ' ' || !menu.spaceSwitches() {
			return event
		}
		menu.switchSelected()
	case tcell.KeyUp, tcell.KeyCtrlP:
		menu.moveSelection(-1)
	case tcell.KeyDown, tcell.KeyCtrlN:
		menu.moveSelection(1)
	case tcell.KeyPgUp:
		menu.moveSelection(-menu.pageRows)
	case tcell.KeyPgDn:
		menu.moveSelection(menu.pageRows)
	default:
		return event
	}
	return nil
}

// selectCommand selects the listed command of the section with the name. Does nothing if it is not listed.
func (menu *CommandMenu) selectCommand(section string, name string) {
	for row, match := range menu.rows {
		if match != nil && match.section == section && match.command.Name == name {
			menu.list.Select(row, 0)
			return
		}
	}
}

// selectedSwitch returns the row and the match of the selected command if it is a switch, false otherwise.
func (menu *CommandMenu) selectedSwitch() (int, *commandMatch, bool) {
	row, _ := menu.list.GetSelection()
	if row < 0 || row >= len(menu.rows) || menu.rows[row] == nil || menu.rows[row].command.On == nil {
		return 0, nil, false
	}
	return row, menu.rows[row], true
}

// spaceSwitches returns whether Space switches the selected command instead of being typed into the query: if it is
// a switch, and nothing is searched or the selection was moved since.
func (menu *CommandMenu) spaceSwitches() bool {
	_, _, ok := menu.selectedSwitch()
	return ok && (menu.moved || strings.TrimSpace(menu.input.GetText()) == "")
}

// switchSelected switches the selected command and shows its new state; the menu stays, so several can be switched.
func (menu *CommandMenu) switchSelected() {
	row, match, ok := menu.selectedSwitch()
	if !ok {
		return
	}
	match.command.Run()
	menu.setCommandRow(row, *match)
}

// updateHint shows in the border that Space switches the selected command, while it does.
func (menu *CommandMenu) updateHint() {
	if menu.hint == nil {
		return
	}
	text := ""
	if menu.spaceSwitches() {
		text = shortcut_helper.KeySpace + ": switch on/off"
	}
	menu.hint.SetText(text)
}

// moveSelection moves the selection by delta commands, skipping the titles of the sections.
func (menu *CommandMenu) moveSelection(delta int) {
	var commandRows []int
	for row, match := range menu.rows {
		if match != nil {
			commandRows = append(commandRows, row)
		}
	}
	if len(commandRows) == 0 {
		return
	}
	row, _ := menu.list.GetSelection()
	index := max(slices.Index(commandRows, row), 0)
	newRow := commandRows[min(max(index+delta, 0), len(commandRows)-1)]
	if newRow == commandRows[0] {
		// shows the title of the first section as well
		menu.list.ScrollToBeginning()
	}
	menu.moved = true
	menu.list.Select(newRow, 0)
	menu.updateHint()
}

// filter shows the commands matching the query, and selects the best match.
func (menu *CommandMenu) filter(query string) {
	// typed: Space is typed as well until the selection is moved again
	menu.moved = false
	defer menu.updateHint()
	menu.list.Clear()
	menu.rows = nil
	bestRow, bestScore := -1, -1
	searching := strings.TrimSpace(query) != ""
	for _, section := range menu.sections {
		matches := slices.DeleteFunc(filterCommands(section.Commands, query), func(match commandMatch) bool {
			// the SearchOnly ones are found by searching, which the ones with a query do
			if searching {
				return match.command.Query != ""
			}
			return match.command.SearchOnly
		})
		if len(matches) == 0 {
			continue
		}
		row := len(menu.rows)
		menu.list.SetCell(row, 0, tview.NewTableCell(section.Title).
			SetTextColor(theme.Colors.Dialog.SectionTitle).
			SetAttributes(tcell.AttrItalic).
			SetSelectable(false))
		menu.rows = append(menu.rows, nil)
		for _, match := range matches {
			match.section = section.Title
			row := len(menu.rows)
			menu.setCommandRow(row, match)
			menu.rows = append(menu.rows, &match)
			if match.score > bestScore {
				bestRow, bestScore = row, match.score
			}
		}
	}
	if bestRow < 0 {
		menu.list.SetCell(0, 0, tview.NewTableCell("No matching command").
			SetTextColor(tcell.ColorGray).
			SetSelectable(false))
		return
	}
	menu.list.ScrollToBeginning()
	menu.list.Select(bestRow, 0)
}

// setCommandRow shows the command in the row: the name (indented below the title of the section, with the runes
// matching the query highlighted), the keys in the color of their group (like in the shortcut map) and the
// description dimmed. The description is last, so it is the one cut off if the menu is too narrow.
func (menu *CommandMenu) setCommandRow(row int, match commandMatch) {
	command := match.command
	state := ""
	switch {
	case command.On != nil:
		state = switchMark(command.On()) + " "
	case menu.hasSwitches:
		state = "  "
	}
	menu.list.SetCell(row, 0, tview.NewTableCell("  "+state+highlightMatch(match)+"  "))
	menu.list.SetCell(row, 1, tview.NewTableCell(tview.Escape(strings.Join(command.KeyCombo, " "))+"  ").
		SetTextColor(command.Group.KeyColor()))
	menu.list.SetCell(row, 2, tview.NewTableCell(tview.Escape(command.Description)).
		SetTextColor(tcell.ColorGray).
		SetExpansion(1))
}

// switchOn and switchOff show the state of a switch in front of its name, also the items of a checklist: easier
// to tell apart than check boxes (☑, ☐) in many fonts.
const (
	switchOn  = "●"
	switchOff = "○"
)

// switchMark returns the mark of a switch that is on or off, in its color (with style tags).
func switchMark(on bool) string {
	if on {
		return txwidgets.Span(theme.Colors.ShortcutMap.KeyCombo, "%s", switchOn)
	}
	return txwidgets.Span(tcell.ColorGray, "%s", switchOff)
}

// highlightMatch returns the name of the command with the runes matching the query highlighted.
func highlightMatch(match commandMatch) string {
	var text strings.Builder
	name := []rune(match.command.Name)
	for start := 0; start < len(name); {
		highlighted := slices.Contains(match.positions, start)
		end := start + 1
		for end < len(name) && slices.Contains(match.positions, end) == highlighted {
			end++
		}
		if highlighted {
			text.WriteString(txwidgets.Span(theme.Colors.ShortcutMap.KeyCombo, "%s", string(name[start:end])))
		} else {
			text.WriteString(tview.Escape(string(name[start:end])))
		}
		start = end
	}
	return text.String()
}

// choose closes the menu and runs the command of the row, see ShowCommandMenu.
func (menu *CommandMenu) choose(row int) {
	if row < 0 || row >= len(menu.rows) || menu.rows[row] == nil {
		return
	}
	command := menu.rows[row].command
	if command.Query != "" {
		// e.g. lists the commands to sort by a column; the menu stays
		menu.input.SetText(command.Query + " ")
		return
	}
	menu.chosen = &command
	menu.Close()
}

// Chosen returns the command chosen to run, nil if none was (yet).
func (menu *CommandMenu) Chosen() *shortcut_helper.ShortcutEntry {
	return menu.chosen
}

func (menu *CommandMenu) GetName() string {
	return string(CommandMenuPage)
}

func (menu *CommandMenu) GetLayout() *tview.Flex {
	return menu.layout
}

func (menu *CommandMenu) GetActionChannel() <-chan DialogActionId {
	return menu.actionChannel
}

func (menu *CommandMenu) Close() {
	go func() {
		menu.actionChannel <- DialogCloseActionId
	}()
}
