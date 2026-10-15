package theme

import (
	"testing"

	"github.com/gdamore/tcell/v2"

	"github.com/stretchr/testify/assert"
)

func TestHeaderBackgroundIsDistinct(t *testing.T) {
	table := Colors.Layout.Table
	assert.NotEqual(t, table.HeaderBackground, Colors.FileBrowser.Table.State.Equal, "unchanged file color")
	assert.NotEqual(t, table.HeaderBackground, Colors.SnapshotBrowser.Table.State.Equal, "unchanged snapshot color")
	assert.NotEqual(t, table.HeaderBackground, table.MultiSelectionBackground)
	assert.NotEqual(t, table.HeaderBackground, table.SelectedBackground)
	assert.NotEqual(t, table.HeaderBackground, tcell.ColorBlack)
}

func TestCreateTitleText(t *testing.T) {
	title := CreateTitleText("Hello")
	assert.Equal(t, " Hello ", title)
}

// Every text color of a table stays readable in the selected row, also the dim ones (e.g. unchanged files).
func TestSelectedStyle_TextIsReadable(t *testing.T) {
	background := Colors.Layout.Table.SelectedBackground
	for _, color := range []tcell.Color{tcell.ColorGray, tcell.ColorDarkGray, tcell.ColorRed, tcell.ColorGreen, tcell.ColorYellow, tcell.ColorWhite, Accent, tcell.ColorDefault} {
		fg, bg, _ := SelectedStyle(color).Decompose()
		assert.Equal(t, background, bg)
		assert.GreaterOrEqual(t, contrast(luminance(fg.RGB()), luminance(background.RGB())), minSelectedContrast, "color %v", color)
	}
	// colors that are readable keep their color
	fg, _, _ := SelectedStyle(tcell.ColorWhite).Decompose()
	r, g, b := fg.RGB()
	wr, wg, wb := tcell.ColorWhite.RGB()
	assert.Equal(t, []int32{wr, wg, wb}, []int32{r, g, b})
}
