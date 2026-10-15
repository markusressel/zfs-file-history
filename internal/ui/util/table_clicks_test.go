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

// press presses and releases the left button on the row at y, as tview gets it from the application.
func press(table *tview.Table, y int) {
	event := tcell.NewEventMouse(2, y, tcell.Button1, tcell.ModNone)
	setFocus := func(p tview.Primitive) { p.Focus(nil) }
	table.MouseHandler()(tview.MouseLeftDown, event, setFocus)
	table.MouseHandler()(tview.MouseLeftUp, event, setFocus)
	table.MouseHandler()(tview.MouseLeftClick, event, setFocus)
}

func TestOnClickOnSelectedRow(t *testing.T) {
	table := tview.NewTable().SetSelectable(true, false)
	for row, text := range []string{"first", "second", "third"} {
		table.SetCell(row, 0, tview.NewTableCell(text))
	}
	var clicked []int
	IgnoreClicksBelowRows(table)
	OnClickOnSelectedRow(table, func(row int) { clicked = append(clicked, row) })
	table.SetRect(0, 0, 20, 10)

	// selected but not highlighted: the table has no focus
	press(table, 0)
	assert.Empty(t, clicked)

	press(table, 1)
	assert.Empty(t, clicked, "another row is only selected")
	row, _ := table.GetSelection()
	assert.Equal(t, 1, row)

	press(table, 1)
	assert.Equal(t, []int{1}, clicked, "the highlighted row")

	press(table, 5)
	assert.Equal(t, []int{1}, clicked, "below the rows")
}
