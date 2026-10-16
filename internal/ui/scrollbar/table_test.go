package scrollbar

import (
	"strings"
	"testing"
	"zfs-file-history/internal/testutil"

	"github.com/rivo/tview"
	"github.com/stretchr/testify/assert"
)

// drawTable draws a table of 20x5 with the rows (the first one fixed if fixedRows is 1), selecting row, and returns
// the last column (the bar), top to bottom, and the first row.
func drawTable(t *testing.T, rows int, fixedRows int, row int) (bar string, first string) {
	table := tview.NewTable().SetSelectable(true, false).SetFixed(fixedRows, 0)
	for i := range rows {
		table.SetCell(i, 0, tview.NewTableCell(strings.Repeat("x", 20)).SetExpansion(1))
	}
	table.Select(row, 0)
	view := NewTable(table, fixedRows)

	screen := testutil.NewScreen(t, 20, 5)
	view.SetRect(0, 0, 20, 5)
	view.Draw(screen)

	var column strings.Builder
	for y := range 5 {
		text, _, _ := screen.Get(19, y)
		column.WriteString(text)
	}
	return column.String(), testutil.ScreenLines(screen)[0]
}

func TestTable_Scrollbar(t *testing.T) {
	bar, first := drawTable(t, 5, 0, 0)
	assert.Equal(t, "xxxxx", bar, "no bar while the rows fit: the table has the whole width")
	assert.Equal(t, strings.Repeat("x", 20), first)

	bar, _ = drawTable(t, 20, 0, 0)
	assert.Equal(t, "•┃││▾", bar, "at the top")
	bar, _ = drawTable(t, 20, 0, 19)
	assert.Equal(t, "▴││┃•", bar, "at the end, where the selection scrolled the table to")

	bar, _ = drawTable(t, 5, 1, 0)
	assert.Equal(t, "xxxxx", bar, "the fixed row is not counted")
	bar, _ = drawTable(t, 6, 1, 0)
	assert.NotEqual(t, "xxxxx", bar, "one row more than fits below the fixed one")
}
