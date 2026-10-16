package theme

import (
	"fmt"
	"os"
	"testing"

	"github.com/gdamore/tcell/v2"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

// Regression: the colors of states are RGB values. The green of the terminal palette was drawn in its own shade in
// tables, but as its RGB value (#008000, darker) in texts with style tags, so "ONLINE" had two greens.
func TestStateColorsAreRGB(t *testing.T) {
	for name, color := range map[string]tcell.Color{
		"ok": Colors.Info.Ok, "warning": Colors.Info.Warning, "error": Colors.Info.Error,
		"online": Colors.Pool.Online, "degraded": Colors.Pool.Degraded, "faulted": Colors.Pool.Faulted,
		"added": Colors.FileBrowser.Table.State.Added, "deleted": Colors.FileBrowser.Table.State.Deleted,
		"modified": Colors.FileBrowser.Table.State.Modified, "danger": Colors.Dialog.Danger,
	} {
		assert.True(t, color.IsRGB(), name)
	}
}

// The demo finds the selected row of a table by its background (scripts/demo/driver.py, SELECTED_BACKGROUND): with
// another color, it would no longer find the entries it moves to.
func TestSelectedBackgroundOfTheDemo(t *testing.T) {
	source, err := os.ReadFile("../../../scripts/demo/driver.py")
	require.NoError(t, err)
	r, g, b := Colors.Layout.Table.SelectedBackground.RGB()
	assert.Contains(t, string(source), fmt.Sprintf("SELECTED_BACKGROUND = %q", fmt.Sprintf("%02x%02x%02x", r, g, b)))
}
