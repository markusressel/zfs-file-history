package dialog

import (
	"fmt"
	"zfs-file-history/internal/ui/theme"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

type HelpPage struct {
	layout *tview.Flex
}

func NewHelpPage() *HelpPage {
	helpPage := &HelpPage{}

	helpPage.createLayout()

	return helpPage
}

type TableEntry struct {
	Key   string
	Value string
}

var (
	emptyEntry = &TableEntry{Key: "", Value: ""}
)

func (p *HelpPage) createLayout() {
	helpTable := tview.NewTable()

	helpTableEntries := []*TableEntry{
		{Key: "F1, ?", Value: "Opens help dialog"},
		{Key: "up, k", Value: "Moves cursor up"},
		{Key: "down, j", Value: "Moves cursor down"},
		{Key: "⬅️", Value: "Opens parent directory"},
		{Key: "➡️", Value: "Enters selected directory"},
		{Key: "space", Value: "Toggle Multi-Selection"},
		{Key: "tab, shift+tab", Value: "Cycles window focus"},
		emptyEntry,
		{Key: "esc", Value: "Closes any currently open dialog"},
		{Key: "ctrl+q", Value: "Quits zfs-file-history"},
	}

	for row, entry := range helpTableEntries {
		setHelpTableRow(helpTable, row, entry)
	}

	p.layout = createModal(" ℹ️ Help ", helpTable, 60, 14)
}

func setHelpTableRow(helpTable *tview.Table, row int, entry *TableEntry) {
	keyText := ""
	keyColor := tcell.ColorWhite
	if entry != emptyEntry {
		keyText = fmt.Sprintf("%s:", entry.Key)
		keyColor = theme.Colors.Layout.Table.Header
	}

	helpTable.SetCell(row, 0, tview.NewTableCell(keyText).
		SetAlign(tview.AlignRight).
		SetTextColor(keyColor),
	)
	helpTable.SetCell(row, 1, tview.NewTableCell(entry.Value).
		SetAlign(tview.AlignLeft).
		SetTextColor(tcell.ColorWhite),
	)
}

func (p *HelpPage) GetLayout() *tview.Flex {
	return p.layout
}
