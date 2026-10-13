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
