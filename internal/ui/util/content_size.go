package util

import (
	"strings"

	"github.com/rivo/tview"
)

// TextViewContentSize returns the size the text view needs to show all of its text, including its border: the
// width of its widest line, and its number of lines. 0 if it has no text.
func TextViewContentSize(textView *tview.TextView) (width int, height int) {
	width, height = TextSize(textView.GetText(false))
	if height == 0 {
		return 0, 0
	}
	return width + 2, height + 2
}

// TextSize returns the width of the widest line of the text (which may contain style tags), and its number of
// lines. A trailing line break does not start another line.
func TextSize(text string) (width int, height int) {
	text = strings.TrimSuffix(text, "\n")
	if text == "" {
		return 0, 0
	}
	lines := strings.Split(text, "\n")
	for _, line := range lines {
		width = max(width, tview.TaggedStringWidth(line))
	}
	return width, len(lines)
}

// FitContent returns the size of a pane that fits its content of the given size, but at most the given share
// (percent) of the available size, so the other pane keeps the rest. 0 if there is no content (e.g. not loaded yet).
func FitContent(available int, contentSize int, maxPercent int) int {
	if contentSize <= 0 {
		return 0
	}
	return min(contentSize, available*maxPercent/100)
}
