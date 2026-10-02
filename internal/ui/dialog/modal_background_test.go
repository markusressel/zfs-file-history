package dialog

import (
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"github.com/stretchr/testify/assert"
)

// tview's Flex does not clear its area, so gaps between the children of a dialog (e.g. the padding of a table)
// showed the page behind the dialog.
func TestModal_HidesThePageBehind(t *testing.T) {
	app := tview.NewApplication()
	screen := tcell.NewSimulationScreen("UTF-8")
	app.SetScreen(screen)
	screen.SetSize(100, 30)
	behind := tview.NewTextView().SetText(strings.Repeat(strings.Repeat("X", 100)+"\n", 30))
	pages := tview.NewPages().AddPage("behind", behind, true, true)
	app.SetRoot(pages, true)
	go func() { _ = app.Run() }()
	t.Cleanup(app.Stop)

	var dialogX, dialogY, dialogWidth, dialogHeight int
	var lines []string
	onUiThread(t, app, func() {
		d := NewDatasetPropertiesDialog(app, "pool/data", newTestProperties(), false, nil)
		ShowDialogOnPages(app, pages, d, nil)
		app.ForceDraw()
		// the frame, inside the centering wrappers
		dialogX, dialogY, dialogWidth, dialogHeight = findFrame(screen)
		cells, width, height := screen.GetContents()
		for y := 0; y < height; y++ {
			var line strings.Builder
			for x := 0; x < width; x++ {
				if runes := cells[y*width+x].Runes; len(runes) > 0 {
					line.WriteRune(runes[0])
				} else {
					line.WriteRune(' ')
				}
			}
			lines = append(lines, line.String())
		}
	})

	assert.Greater(t, dialogWidth, 10, "dialog found")
	for y := dialogY; y < dialogY+dialogHeight; y++ {
		inside := []rune(lines[y])[dialogX : dialogX+dialogWidth]
		assert.NotContains(t, string(inside), "X", "line %d of the dialog shows the page behind it: %q", y-dialogY, string(inside))
	}
}

// findFrame returns the rect of the dialog frame: from its top left corner "╔" to its bottom right corner "╝".
func findFrame(screen tcell.SimulationScreen) (x, y, width, height int) {
	cells, screenWidth, screenHeight := screen.GetContents()
	runeAt := func(x, y int) rune {
		if runes := cells[y*screenWidth+x].Runes; len(runes) > 0 {
			return runes[0]
		}
		return ' '
	}
	for y := 0; y < screenHeight; y++ {
		for x := 0; x < screenWidth; x++ {
			if runeAt(x, y) != '╔' {
				continue
			}
			right, bottom := x, y
			for right < screenWidth && runeAt(right, y) != '╗' {
				right++
			}
			for bottom < screenHeight && runeAt(x, bottom) != '╚' {
				bottom++
			}
			return x, y, right - x + 1, bottom - y + 1
		}
	}
	return 0, 0, 0, 0
}
