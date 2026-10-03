package dialog

import (
	"strings"
	"testing"
	"zfs-file-history/internal/ui/theme"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// sparklineRow returns the row of the sparkline with the given label (-1: none) and whether it has a highlighted
// braille cell, the selected version (see uiutil.DrawSparkline).
func sparklineRow(t *testing.T, app *tview.Application, screen tcell.SimulationScreen, label string) (row int, highlighted bool) {
	row = -1
	onUiThread(t, app, func() {
		app.ForceDraw()
		cells, width, height := screen.GetContents()
		for y := 0; y < height && row < 0; y++ {
			var line strings.Builder
			for x := 0; x < width; x++ {
				if runes := cells[y*width+x].Runes; len(runes) > 0 {
					line.WriteRune(runes[0])
				}
			}
			if !strings.Contains(line.String(), label+" ") {
				continue
			}
			for x := 0; x < width; x++ {
				cell := cells[y*width+x]
				_, background, _ := cell.Style.Decompose()
				if len(cell.Runes) > 0 && cell.Runes[0] >= 0x2800 && cell.Runes[0] <= 0x28ff &&
					background == theme.Colors.Sparkline.SelectedBackground {
					row, highlighted = y, true
				}
			}
			if !highlighted {
				row = y
			}
		}
	})
	return row, highlighted
}
