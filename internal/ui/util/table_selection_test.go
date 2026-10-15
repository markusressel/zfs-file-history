package util

import (
	"testing"
	"zfs-file-history/internal/ui/theme"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"github.com/stretchr/testify/assert"
)

// The text color of a cell is kept while its row is selected, wherever tview keeps it (in the style of the cell, or in
// Color for cells without one).
func TestKeepColorWhenSelected(t *testing.T) {
	withStyle := KeepColorWhenSelected(tview.NewTableCell("a").SetTextColor(tcell.ColorRed))
	assert.Equal(t, theme.SelectedStyle(tcell.ColorRed), withStyle.SelectedStyle)

	withoutStyle := &tview.TableCell{Text: "b"}
	withoutStyle.SetTextColor(tcell.ColorYellow)
	KeepColorWhenSelected(withoutStyle)
	assert.Equal(t, theme.SelectedStyle(tcell.ColorYellow), withoutStyle.SelectedStyle)
}
