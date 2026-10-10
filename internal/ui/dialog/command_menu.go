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
	// commandMenuMaxRows is the number of commands shown at once, the others are scrolled to
	commandMenuMaxRows = 10
	// commandMenuMaxWidth is the width of the menu, if the screen is wide enough
	commandMenuMaxWidth = 100
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
}

// ShowCommandMenu shows a menu of the commands, in the order of the sections (e.g. the focused component first). The
// chosen command is run after the menu is closed, once the focus is back where it was, so a dialog opened by the
// command returns the focus there as well. Must be called on the UI thread.
func ShowCommandMenu(application *tview.Application, pages *tview.Pages, sections []CommandSection) {
	menu := NewCommandMenu(sections)
	ShowDialogOnPages(application, pages, menu, func() {
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
		if commands := shortcut_helper.Commands(section.Commands); len(commands) > 0 {
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
	frame.SetBorderPadding(0, 0, 1, 1)
	frame.AddItem(content, 0, 1, true)

	// the nil items keep the page visible around the menu (see isOnDialog)
	columns := tview.NewFlex().
		AddItem(nil, 0, 1, false).
		AddItem(frame, 0, 0, true).
		AddItem(nil, 0, 1, false)
	columns.SetDrawFunc(func(screen tcell.Screen, x, y, width, height int) (int, int, int, int) {
		// not wider than needed, so the keys are next to the names
		columns.ResizeItem(frame, min(width, commandMenuMaxWidth), 0)
		return x, y, width, height
	})
	rows := 0
	for _, section := range menu.sections {
		rows += 1 + len(section.Commands)
	}
	rows = min(rows, commandMenuMaxRows)
	menu.layout = tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(nil, 0, 1, false).
		AddItem(columns, max(rows, 1)+1+2, 0, true)
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
	case tcell.KeyUp, tcell.KeyCtrlP:
		menu.moveSelection(-1)
	case tcell.KeyDown, tcell.KeyCtrlN:
		menu.moveSelection(1)
	case tcell.KeyPgUp:
		menu.moveSelection(-commandMenuMaxRows)
	case tcell.KeyPgDn:
		menu.moveSelection(commandMenuMaxRows)
	default:
		return event
	}
	return nil
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
	menu.list.Select(newRow, 0)
}

// filter shows the commands matching the query, and selects the best match.
func (menu *CommandMenu) filter(query string) {
	menu.list.Clear()
	menu.rows = nil
	bestRow, bestScore := -1, -1
	for _, section := range menu.sections {
		matches := filterCommands(section.Commands, query)
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
	menu.list.SetCell(row, 0, tview.NewTableCell("  "+highlightMatch(match)+"  "))
	menu.list.SetCell(row, 1, tview.NewTableCell(tview.Escape(strings.Join(command.KeyCombo, " "))+"  ").
		SetTextColor(command.Group.KeyColor()))
	menu.list.SetCell(row, 2, tview.NewTableCell(tview.Escape(command.Description)).
		SetTextColor(tcell.ColorGray).
		SetExpansion(1))
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
	menu.chosen = &menu.rows[row].command
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
