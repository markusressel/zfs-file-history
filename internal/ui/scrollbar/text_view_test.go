package scrollbar

import (
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// drawTextView draws a bordered text view of 20x7 (5 lines inside) with the lines, scrolled to row, and returns the
// column right of the text (the bar), top to bottom.
func drawTextView(t *testing.T, lines int, row int) string {
	view := NewTextView(tview.NewApplication())
	view.SetBorder(true)
	text := make([]string, lines)
	for i := range text {
		text[i] = "line"
	}
	view.SetText(strings.Join(text, "\n"))
	view.ScrollTo(row, 0)

	screen := tcell.NewSimulationScreen("UTF-8")
	require.NoError(t, screen.Init())
	screen.SetSize(20, 7)
	view.SetRect(0, 0, 20, 7)
	view.Draw(screen)

	var column strings.Builder
	for y := 1; y < 6; y++ {
		text, _, _ := screen.Get(18, y)
		column.WriteString(text)
	}
	return column.String()
}

func TestTextView_Scrollbar(t *testing.T) {
	assert.Equal(t, "     ", drawTextView(t, 5, 0), "no bar while the text fits")
	assert.Equal(t, "•┃││▾", drawTextView(t, 20, 0), "at the top")
	assert.Equal(t, "▴││┃•", drawTextView(t, 20, 15), "at the end: a whole cell, like at the top")
	assert.Equal(t, "▴││┃•", drawTextView(t, 20, 100), "scrolled past the end: where tview keeps it")
}

// While the bar is shown, it has a column of its own: the text is not drawn below it, and panes sized by their
// content add it to their width.
func TestTextView_BarTakesAColumn(t *testing.T) {
	view := NewTextView(nil)
	view.SetBorder(true)
	view.SetBorderPadding(0, 0, 1, 1)
	screen := tcell.NewSimulationScreen("UTF-8")
	require.NoError(t, screen.Init())
	screen.SetSize(12, 4)
	view.SetRect(0, 0, 12, 4)
	draw := func(text string) (innerWidth int) {
		view.SetText(text)
		view.Draw(screen)
		_, _, innerWidth, _ = view.GetInnerRect()
		return innerWidth
	}

	assert.Equal(t, 8, draw("1234567890"), "border and padding")
	assert.Equal(t, 0, view.BarWidth())

	// wrapped to more lines than fit
	assert.Equal(t, 7, draw("1234567890\nb\nc"))
	assert.Equal(t, 1, view.BarWidth())
	text, _, _ := screen.Get(9, 1)
	assert.Equal(t, "•", text, "the bar right of the text")
	text, _, _ = screen.Get(8, 1)
	assert.Equal(t, "7", text, "the text wrapped before it")

	assert.Equal(t, 8, draw("short"), "gone again")
	assert.Equal(t, 0, view.BarWidth())
}
