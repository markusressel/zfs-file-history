package dialog

import (
	"testing"
	"zfs-file-history/internal/testutil"
	"zfs-file-history/internal/ui/theme"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// sparklineRow returns the row of the sparkline with the given label (-1: none) and whether it has a highlighted
// braille cell, the selected version (see uiutil.DrawSparkline).
func sparklineRow(t *testing.T, app *tview.Application, screen tcell.SimulationScreen, label string) (row int, highlighted bool) {
	row = -1
	testutil.OnUiThread(t, app, func() {
		app.ForceDraw()
		_, y, found := testutil.FindInScreen(screen, label+" ")
		if !found {
			return
		}
		row = y
		width, _ := screen.Size()
		for x := range width {
			text, style, _ := screen.Get(x, y)
			_, background, _ := style.Decompose()
			if r := []rune(text + " ")[0]; r >= 0x2800 && r <= 0x28ff && background == theme.Colors.Sparkline.SelectedBackground {
				highlighted = true
			}
		}
	})
	return row, highlighted
}
