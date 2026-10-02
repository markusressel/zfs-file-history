package util

import (
	"testing"
	"time"
	"zfs-file-history/internal/ui/theme"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newTestSplit draws a split of 100 columns with two bordered panes (1:1).
func newTestSplit(t *testing.T) (*ResizableSplit, tcell.SimulationScreen, *tview.Box, *tview.Box) {
	screen := tcell.NewSimulationScreen("UTF-8")
	require.NoError(t, screen.Init())
	screen.SetSize(100, 10)
	left := tview.NewBox().SetBorder(true)
	right := tview.NewBox().SetBorder(true)
	split := NewResizableSplit(tview.NewApplication(), left, right, 1, 1)
	split.SetRect(0, 0, 100, 10)
	split.Draw(screen)
	return split, screen, left, right
}

func mouse(split *ResizableSplit, action tview.MouseAction, x int, buttons tcell.ButtonMask) (tview.MouseAction, *tcell.EventMouse) {
	return split.MouseCapture(action, tcell.NewEventMouse(x, 5, buttons, tcell.ModNone))
}

func width(box *tview.Box) int {
	_, _, w, _ := box.GetRect()
	return w
}

func TestResizableSplit_Drag(t *testing.T) {
	split, screen, left, right := newTestSplit(t)
	require.Equal(t, 50, width(left))

	// clicks elsewhere are passed on
	action, event := mouse(split, tview.MouseLeftDown, 10, tcell.Button1)
	assert.Equal(t, tview.MouseLeftDown, action)
	assert.NotNil(t, event)

	// the boundary: the last column of the left pane, or the first of the right one
	_, event = mouse(split, tview.MouseLeftDown, 50, tcell.Button1)
	assert.Nil(t, event, "consumed")
	time.Sleep(splitResizeInterval)
	mouse(split, tview.MouseMove, 70, tcell.Button1)
	split.Draw(screen)
	assert.Equal(t, 70, width(left))
	assert.Equal(t, 30, width(right))

	// the minimum width
	time.Sleep(splitResizeInterval)
	mouse(split, tview.MouseMove, 95, tcell.Button1)
	split.Draw(screen)
	assert.Equal(t, 100-splitMinPaneWidth, width(left))

	mouse(split, tview.MouseLeftUp, 95, tcell.ButtonNone)
	assert.False(t, split.dragging)
}

func TestResizableSplit_HoverHighlight(t *testing.T) {
	split, screen, _, _ := newTestSplit(t)
	boundaryColor := func() tcell.Color {
		_, _, style, _ := screen.GetContent(50, 5)
		foreground, _, _ := style.Decompose()
		return foreground
	}
	assert.NotEqual(t, theme.Primary, boundaryColor())

	_, event := mouse(split, tview.MouseMove, 49, tcell.ButtonNone)
	assert.Nil(t, event, "consumed, so it is redrawn")
	split.Draw(screen)
	assert.Equal(t, theme.Primary, boundaryColor())

	mouse(split, tview.MouseMove, 10, tcell.ButtonNone)
	split.Draw(screen)
	assert.NotEqual(t, theme.Primary, boundaryColor())
}

func TestResizableSplit_Disabled(t *testing.T) {
	split, _, left, _ := newTestSplit(t)
	split.SetEnabledFunc(func() bool { return false })

	_, event := mouse(split, tview.MouseLeftDown, 50, tcell.Button1)
	assert.NotNil(t, event, "passed on")
	mouse(split, tview.MouseMove, 70, tcell.Button1)
	assert.Equal(t, 50, width(left))
	assert.False(t, split.dragging)
}
