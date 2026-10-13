package util

import (
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"github.com/stretchr/testify/assert"
)

func click(table *tview.Table, y int) bool {
	consumed, _ := table.MouseHandler()(tview.MouseLeftClick, tcell.NewEventMouse(2, y, tcell.ButtonNone, tcell.ModNone), func(tview.Primitive) {})
	return consumed
}

func TestIgnoreClicksBelowRows(t *testing.T) {
	table := tview.NewTable().SetSelectable(true, false)
	for row, text := range []string{"header", "first", "second"} {
		table.SetCell(row, 0, tview.NewTableCell(text))
	}
	chosen := -1
	table.SetSelectedFunc(func(row, column int) { chosen = row })
	IgnoreClicksBelowRows(table)
	table.SetRect(0, 0, 20, 10)
	table.Select(2, 0)

	// below the last row: nothing is selected (tview would select the first row) or chosen
	assert.True(t, click(table, 5), "consumed")
	row, _ := table.GetSelection()
	assert.Equal(t, 2, row)
	assert.Equal(t, -1, chosen)

	// on a row: selected like before
	click(table, 1)
	row, _ = table.GetSelection()
	assert.Equal(t, 1, row)
}
