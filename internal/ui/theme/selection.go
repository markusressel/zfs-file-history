package theme

import (
	"math"

	"github.com/gdamore/tcell/v2"
)

// minSelectedContrast is the contrast (WCAG) the text of the selected row has at least on its background: the
// level for normal text.
const minSelectedContrast = 4.5

// SelectedStyle is the style of a cell of the selected row whose text has the color textColor: the background of
// the selection, the text in its own color, so the colors of a row (e.g. the state of a file) stay visible while it
// is selected. Colors too dark for the background are lightened until they are readable on it.
func SelectedStyle(textColor tcell.Color) tcell.Style {
	return tcell.StyleDefault.
		Foreground(readableOn(textColor, Colors.Layout.Table.SelectedBackground)).
		Background(Colors.Layout.Table.SelectedBackground)
}

// readableOn returns color, mixed with white until it has minSelectedContrast on background. Colors without a
// value (the default color of the terminal) are replaced by SelectedForeground.
func readableOn(color tcell.Color, background tcell.Color) tcell.Color {
	r, g, b := color.RGB()
	if r < 0 {
		return Colors.Layout.Table.SelectedForeground
	}
	backgroundLuminance := luminance(background.RGB())
	for i := 0; i < 10 && contrast(luminance(r, g, b), backgroundLuminance) < minSelectedContrast; i++ {
		r, g, b = r+(255-r)/4+1, g+(255-g)/4+1, b+(255-b)/4+1
		r, g, b = min(r, 255), min(g, 255), min(b, 255)
	}
	return tcell.NewRGBColor(r, g, b)
}

// luminance is the relative luminance (WCAG) of a color.
func luminance(r, g, b int32) float64 {
	channel := func(value int32) float64 {
		c := float64(value) / 255
		if c <= 0.03928 {
			return c / 12.92
		}
		return math.Pow((c+0.055)/1.055, 2.4)
	}
	return 0.2126*channel(r) + 0.7152*channel(g) + 0.0722*channel(b)
}

// contrast is the contrast ratio (WCAG) of two luminances.
func contrast(a, b float64) float64 {
	return (max(a, b) + 0.05) / (min(a, b) + 0.05)
}
