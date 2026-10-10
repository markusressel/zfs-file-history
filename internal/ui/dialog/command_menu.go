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

// CommandMenu lists commands (shortcut entries that can be run, see shortcut_helper.ShortcutEntry.Run) and filters
// them while typing, like the command line of Vim. It is shown at the bottom of the screen, above the page, so the
// layout of the page does not change. Show it with ShowCommandMenu.
type CommandMenu struct {
	layout        *tview.Flex
	input         *tview.InputField
	list          *tview.Table
	actionChannel chan DialogActionId

	commands []shortcut_helper.ShortcutEntry
	matches  []commandMatch
	// chosen is the command to run once the menu is closed, nil if it was closed without choosing one
	chosen *shortcut_helper.ShortcutEntry
}

// ShowCommandMenu shows a menu of the commands, in their order (e.g. the ones of the focused component first). The
// chosen command is run after the menu is closed, once the focus is back where it was, so a dialog opened by the
// command returns the focus there as well. Must be called on the UI thread.
func ShowCommandMenu(application *tview.Application, pages *tview.Pages, commands []shortcut_helper.ShortcutEntry) {
	menu := NewCommandMenu(commands)
	ShowDialogOnPages(application, pages, menu, func() {
		if menu.chosen != nil {
			menu.chosen.Run()
		}
	})
}

// NewCommandMenu creates the menu of the commands that can be run, see ShowCommandMenu.
func NewCommandMenu(commands []shortcut_helper.ShortcutEntry) *CommandMenu {
	menu := &CommandMenu{
		actionChannel: make(chan DialogActionId),
		commands:      shortcut_helper.Commands(commands),
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
	rows := min(len(menu.commands), commandMenuMaxRows)
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

func (menu *CommandMenu) moveSelection(delta int) {
	if len(menu.matches) == 0 {
		return
	}
	row, _ := menu.list.GetSelection()
	menu.list.Select(min(max(row+delta, 0), len(menu.matches)-1), 0)
}

// filter shows the commands matching the query, and selects the best match.
func (menu *CommandMenu) filter(query string) {
	menu.matches = filterCommands(menu.commands, query)
	menu.list.Clear()
	if len(menu.matches) == 0 {
		menu.list.SetCell(0, 0, tview.NewTableCell("No matching command").
			SetTextColor(tcell.ColorGray).
			SetSelectable(false))
		return
	}
	for row, match := range menu.matches {
		menu.list.SetCell(row, 0, tview.NewTableCell(highlightMatch(match)).SetExpansion(1))
		if keys := match.command.KeyCombo; len(keys) > 0 {
			menu.list.SetCell(row, 1, tview.NewTableCell(" "+strings.Join(keys, " ")).
				SetTextColor(theme.Colors.ShortcutMap.KeyCombo).
				SetAlign(tview.AlignRight))
		}
	}
	menu.list.Select(0, 0)
	menu.list.ScrollToBeginning()
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
	if row < 0 || row >= len(menu.matches) {
		return
	}
	menu.chosen = &menu.matches[row].command
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
