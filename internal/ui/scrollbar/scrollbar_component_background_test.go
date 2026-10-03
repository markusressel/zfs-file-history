package scrollbar

import (
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The thin bar runes only cover part of a cell. With the terminal's default background, the scrollbar looked
// see-through on terminals whose background differs from the widgets'.
func TestScrollbar_HasWidgetBackground(t *testing.T) {
	screen := tcell.NewSimulationScreen("UTF-8")
	require.NoError(t, screen.Init())
	screen.SetSize(10, 10)

	tests := []struct {
		orientation   ScrollBarOrientation
		width, height int
	}{
		{ScrollBarVertical, 1, 10},
		{ScrollBarHorizontal, 10, 1},
	}
	for _, test := range tests {
		scrollbar := NewScrollbarComponent(tview.NewApplication(), test.orientation, 0, 100, 30, 20)
		screen.Clear()
		scrollbar.DrawFunc(screen, 0, 0, test.width, test.height)

		for y := 0; y < test.height; y++ {
			for x := 0; x < test.width; x++ {
				_, style, _ := screen.Get(x, y)
				_, background, _ := style.Decompose()
				assert.Equal(t, tview.Styles.PrimitiveBackgroundColor, background, "orientation %v at %d,%d", test.orientation, x, y)
			}
		}
	}
}
