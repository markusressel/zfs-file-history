package dialog

import (
	"fmt"
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

func (p *HelpPage) createLayout() {
	helpTable := tview.NewTable()

	helpTableEntries := []*TableEntry{
		{Key: "up, k", Value: "Move cursor up"},
		{Key: "down, j", Value: "Move cursor down"},
		{Key: "left, h", Value: "Open parent directory"},
		{Key: "right", Value: "Open selected directory"},
		{Key: "tab, backtab", Value: "Switch focus"},
	}

	columns, rows := 2, len(helpTableEntries)
	for row := 0; row < rows; row++ {
		for column := 0; column < columns; column++ {
			entry := helpTableEntries[row]

			for col := 0; col < columns; col++ {
				var text string
				var cellAlignment int
				var cellColor = tcell.ColorWhite
				if col == 0 {
					text = fmt.Sprintf("%s:", entry.Key)
					cellAlignment = tview.AlignRight
					cellColor = tcell.ColorSteelBlue
				} else {
					text = entry.Value
					cellAlignment = tview.AlignLeft
				}
				helpTable.SetCell(
					row, col,
					tview.NewTableCell(text).SetAlign(cellAlignment).SetTextColor(cellColor),
				)
			}
		}
	}

	p.layout = createModal(" Help ", helpTable, 40, 10)
}

func (p *HelpPage) GetLayout() *tview.Flex {
	return p.layout
}
