package scrollbar

import (
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// Table draws a tview.Table with a scrollbar on its right while not all of its rows fit, like
// table.RowSelectionTable: for the plain tables of dialogs that may have more rows than lines (e.g. the command menu,
// the features of a pool). The fixed rows (e.g. a header, see tview.Table.SetFixed) stay visible, so they are not
// counted. It takes the place
// of the table in the layout; focus and change the table itself, the methods of Table are the ones of the table.
type Table struct {
	*tview.Table
	bar *ScrollbarComponent
	// fixedRows is the number of fixed rows of the table, which tview has no getter for
	fixedRows int
}

// NewTable returns the table with a scrollbar, see Table. fixedRows is the number of its fixed rows (as passed to
// SetFixed).
func NewTable(table *tview.Table, fixedRows int) *Table {
	return &Table{Table: table, bar: NewScrollbarComponent(nil, ScrollBarVertical, 0, 0, 0, 0), fixedRows: fixedRows}
}

// Draw draws the table, one column narrower and the scrollbar in the last one while not all rows fit. The bar is
// drawn after the table: tview sets the row offset while drawing it (e.g. to keep the selection visible). Runs while
// drawing.
func (t *Table) Draw(screen tcell.Screen) {
	x, y, width, height := t.GetRect()
	rows := t.GetRowCount() - t.fixedRows
	if rows <= height-t.fixedRows || width < 2 {
		t.Table.Draw(screen)
		return
	}
	t.SetRect(x, y, width-1, height)
	t.Table.Draw(screen)
	t.SetRect(x, y, width, height)
	offset, _ := t.GetOffset()
	t.bar.SetMax(rows)
	t.bar.SetPosition(offset)
	t.bar.SetWidth(max(height-t.fixedRows, 1))
	layout := t.bar.GetLayout()
	layout.SetRect(x+width-1, y, 1, height)
	layout.Draw(screen)
}
