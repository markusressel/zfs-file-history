package util

import (
	"zfs-file-history/internal/ui/theme"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// BorderFooter draws a text into the bottom border of a box, right-aligned, similar to the title in the top border.
// tview has no native support for this. It does not take up any space of the box content.
//
// The text may contain tview style tags. Text without tags uses the footer color.
// All methods must be called on the UI thread, which is where the footer is drawn.
type BorderFooter struct {
	text  string
	color tcell.Color
}

// NewBorderFooter installs a footer on the given box, which must have a border.
// Install it only once per box, and keep the returned footer to change its text.
// A draw func that the box already has is kept and called first.
func NewBorderFooter(box *tview.Box) *BorderFooter {
	footer := &BorderFooter{
		color: theme.Colors.ShortcutMap.Name,
	}

	previousDrawFunc := box.GetDrawFunc()
	box.SetDrawFunc(func(screen tcell.Screen, x, y, width, height int) (int, int, int, int) {
		// tview uses the return value as the inner rect of the box.
		// An inner x of -1 makes tview compute its default inner rect (border and padding) on demand,
		// just like when a box has no draw func.
		innerX, innerY, innerWidth, innerHeight := -1, 0, 0, 0
		if previousDrawFunc != nil {
			innerX, innerY, innerWidth, innerHeight = previousDrawFunc(screen, x, y, width, height)
		}
		footer.draw(screen, x, y, width, height)
		return innerX, innerY, innerWidth, innerHeight
	})

	return footer
}

// SetText sets the footer text. An empty text hides the footer.
func (f *BorderFooter) SetText(text string) {
	f.text = text
}

// GetText returns the footer text, including style tags.
func (f *BorderFooter) GetText() string {
	return f.text
}

// SetColor sets the color of footer text without style tags.
func (f *BorderFooter) SetColor(color tcell.Color) {
	f.color = color
}

// draw prints the footer into the bottom border, leaving the corners intact.
// If it does not fit, the beginning is shown, followed by an ellipsis.
func (f *BorderFooter) draw(screen tcell.Screen, x, y, width, height int) {
	if f.text == "" || width < 4 || height < 2 {
		return
	}

	text := theme.CreateTitleText(f.text)
	bottomY := y + height - 1
	maxWidth := width - 2 // between the corners

	if tview.TaggedStringWidth(text) <= maxWidth {
		tview.Print(screen, text, x+1, bottomY, maxWidth, tview.AlignRight, f.color)
		return
	}

	tview.Print(screen, text, x+1, bottomY, maxWidth-1, tview.AlignLeft, f.color)
	tview.Print(screen, string(tview.SemigraphicsHorizontalEllipsis), x+1+maxWidth-1, bottomY, 1, tview.AlignLeft, f.color)
}
