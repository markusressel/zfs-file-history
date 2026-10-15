package dialog

import (
	"strings"
	"testing"
	"zfs-file-history/internal/testutil"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"github.com/stretchr/testify/assert"
)

// tview's Flex does not clear its area, so gaps between the children of a dialog (e.g. the padding of a table)
// showed the page behind the dialog.
func TestModal_HidesThePageBehind(t *testing.T) {
	app, screen := testutil.NewApp(testutil.WithSize(100, 30))
	behind := tview.NewTextView().SetText(strings.Repeat(strings.Repeat("X", 100)+"\n", 30))
	pages := tview.NewPages().AddPage("behind", behind, true, true)
	testutil.StartApp(t, app, pages)

	var dialogX, dialogY, dialogWidth, dialogHeight int
	var lines []string
	testutil.OnUiThread(t, app, func() {
		d := NewDatasetPropertiesDialog(app, "pool/data", newTestProperties(), false, nil)
		ShowDialogOnPages(app, pages, d, nil)
		app.ForceDraw()
		// the frame, inside the centering wrappers
		dialogX, dialogY, dialogWidth, dialogHeight = findFrame(screen)
		lines = testutil.ScreenLines(screen)
	})

	assert.Greater(t, dialogWidth, 10, "dialog found")
	for y := dialogY; y < dialogY+dialogHeight; y++ {
		inside := []rune(lines[y])[dialogX : dialogX+dialogWidth]
		assert.NotContains(t, string(inside), "X", "line %d of the dialog shows the page behind it: %q", y-dialogY, string(inside))
	}
}

// findFrame returns the rect of the dialog frame: from its top left corner "╔" to its bottom right corner "╝".
func findFrame(screen tcell.SimulationScreen) (x, y, width, height int) {
	screenWidth, screenHeight := screen.Size()
	runeAt := func(x, y int) rune {
		// the cells behind a wide character have no text
		text, _, _ := screen.Get(x, y)
		return []rune(text + " ")[0]
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
