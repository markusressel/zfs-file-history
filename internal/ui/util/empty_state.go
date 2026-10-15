package util

import (
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// DrawEmptyState draws the text in place of the content of a pane that has none (yet): why a list is empty, or what
// is loading (see LoadingView). Each line of text (with style tags, escaped by the caller) is wrapped to the width,
// centered, and the lines are centered in the area, so these texts look the same everywhere. Lines below the area are
// cut off. Runs while drawing.
func DrawEmptyState(screen tcell.Screen, x, y, width, height int, text string, color tcell.Color) {
	if width <= 0 || height <= 0 {
		return
	}
	var lines []string
	for line := range strings.SplitSeq(text, "\n") {
		wrapped := tview.WordWrap(line, width)
		if len(wrapped) == 0 {
			// an empty line, kept as a gap
			wrapped = []string{""}
		}
		lines = append(lines, wrapped...)
	}
	top := y + max((height-len(lines))/2, 0)
	for i, line := range lines {
		if top+i >= y+height {
			break
		}
		tview.Print(screen, line, x, top+i, width, tview.AlignCenter, color)
	}
}
