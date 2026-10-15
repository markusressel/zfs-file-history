package util

import (
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// IgnoreClicksBelowRows makes clicks into the empty space below the last row of the table do nothing. tview selects
// the first row for them instead (e.g. the header), and a click on the selected row also chooses it.
//
// Uses the mouse capture of the table.
func IgnoreClicksBelowRows(table *tview.Table) {
	table.SetMouseCapture(func(action tview.MouseAction, event *tcell.EventMouse) (tview.MouseAction, *tcell.EventMouse) {
		if action != tview.MouseLeftClick || event == nil {
			return action, event
		}
		x, y := event.Position()
		if !table.InRect(x, y) {
			return action, event
		}
		if row, _ := table.CellAt(x, y); row < 0 {
			return tview.MouseConsumed, nil
		}
		return action, event
	})
}

// OnClickOnSelectedRow calls onClick with the row when the row that was selected (and highlighted: the table had the
// focus) is clicked, e.g. to check or uncheck an item of a list of switches. A click on another row only selects it,
// so a click on a row that was not highlighted never changes it by accident, like Space acts on the highlighted row.
// Runs on the UI thread.
//
// Uses the mouse capture of the table, after the one set before (e.g. IgnoreClicksBelowRows).
func OnClickOnSelectedRow(table *tview.Table, onClick func(row int)) {
	previous := table.GetMouseCapture()
	// pressedRow is the row the mouse was pressed on if it was highlighted then, -1 otherwise: tview focuses and
	// selects on the press, before the click
	pressedRow := -1
	table.SetMouseCapture(func(action tview.MouseAction, event *tcell.EventMouse) (tview.MouseAction, *tcell.EventMouse) {
		if previous != nil {
			action, event = previous(action, event)
		}
		if event == nil || (action != tview.MouseLeftDown && action != tview.MouseLeftClick) {
			return action, event
		}
		x, y := event.Position()
		row := -1
		if table.InRect(x, y) {
			row, _ = table.CellAt(x, y)
		}
		selected, _ := table.GetSelection()
		switch {
		case action == tview.MouseLeftDown:
			pressedRow = -1
			if row >= 0 && row == selected && table.HasFocus() {
				pressedRow = row
			}
		case row >= 0 && row == pressedRow:
			pressedRow = -1
			onClick(row)
		}
		return action, event
	})
}
