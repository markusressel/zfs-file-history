package testutil

import (
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"github.com/stretchr/testify/require"
)

// NewScreen returns an initialized simulation screen of the given size, for a test that draws primitives on it
// itself, without an application (see NewApp otherwise). Initializing resets the size, so it is set afterwards: tests
// that set it before drew on 80x25.
func NewScreen(t testing.TB, width int, height int) tcell.SimulationScreen {
	t.Helper()
	screen := tcell.NewSimulationScreen("UTF-8")
	require.NoError(t, screen.Init())
	t.Cleanup(screen.Fini)
	screen.SetSize(width, height)
	return screen
}

// DrawToScreen draws the primitive on a new screen of the given size (see NewScreen), filling it, and returns the
// screen.
func DrawToScreen(t testing.TB, primitive tview.Primitive, width int, height int) tcell.SimulationScreen {
	t.Helper()
	screen := NewScreen(t, width, height)
	primitive.SetRect(0, 0, width, height)
	primitive.Draw(screen)
	return screen
}

// screenCell is a rune on the screen and the column it is drawn in.
type screenCell struct {
	r rune
	x int
}

// screenRows returns the runes on the screen by row, a rune per cell: a space where nothing was drawn, wide
// characters once (the cells they cover are skipped), so the text lines up with the columns of the screen. It shows
// what was drawn first, but does not draw.
func screenRows(screen tcell.SimulationScreen) [][]screenCell {
	screen.Show()
	width, height := screen.Size()
	rows := make([][]screenCell, height)
	for y := range height {
		for x := 0; x < width; {
			text, _, cellWidth := screen.Get(x, y)
			r := ' '
			if text != "" {
				r = []rune(text)[0]
			}
			rows[y] = append(rows[y], screenCell{r: r, x: x})
			x += max(cellWidth, 1)
		}
	}
	return rows
}

// rowText returns the text of a row of screenRows.
func rowText(row []screenCell) string {
	var text strings.Builder
	for _, cell := range row {
		text.WriteRune(cell.r)
	}
	return text.String()
}

// ScreenLines returns the text on the screen, a line per row with a rune per cell (see screenRows). It does not draw: it is for screens a
// test draws on itself (see DrawToScreen); the screen of a running app is read with ScreenText.
func ScreenLines(screen tcell.SimulationScreen) []string {
	rows := screenRows(screen)
	lines := make([]string, len(rows))
	for y, row := range rows {
		lines[y] = rowText(row)
	}
	return lines
}

// ScreenText draws the running app and returns the text on its screen, a line per row (see ScreenLines). It draws
// first: a change made on the UI thread (e.g. with OnUiThread) is only shown once something draws, and a test must
// not depend on something else doing so in time (see TestScreenIsDrawnBeforeRead).
func ScreenText(t testing.TB, app *tview.Application, screen tcell.SimulationScreen) string {
	t.Helper()
	var lines []string
	OnUiThread(t, app, func() {
		app.ForceDraw()
		lines = ScreenLines(screen)
	})
	return strings.Join(lines, "\n") + "\n"
}

// FindInScreen returns the column and row of the first occurrence of text on the screen, and whether it is shown.
// The column is the one on the screen, also behind wide characters. It does not draw (see ScreenLines).
func FindInScreen(screen tcell.SimulationScreen, text string) (x int, y int, found bool) {
	for y, row := range screenRows(screen) {
		line := rowText(row)
		if index := strings.Index(line, text); index >= 0 {
			return row[len([]rune(line[:index]))].x, y, true
		}
	}
	return 0, 0, false
}

// FindText draws the running app and returns where text is on its screen, like FindInScreen. Tests that click on
// something find it with it, instead of computing where it is drawn.
func FindText(t testing.TB, app *tview.Application, screen tcell.SimulationScreen, text string) (x int, y int, found bool) {
	t.Helper()
	OnUiThread(t, app, func() {
		app.ForceDraw()
		x, y, found = FindInScreen(screen, text)
	})
	return x, y, found
}
