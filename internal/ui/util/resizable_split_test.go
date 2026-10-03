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

// One pane above the other: the boundary is a row, dragged up and down.
func TestResizableSplit_Vertical(t *testing.T) {
	screen := tcell.NewSimulationScreen("UTF-8")
	require.NoError(t, screen.Init())
	screen.SetSize(40, 30)
	top := tview.NewBox().SetBorder(true)
	bottom := tview.NewBox().SetBorder(true)
	split := NewVerticalResizableSplit(tview.NewApplication(), top, bottom, 1, 2)
	split.SetRect(0, 0, 40, 30)
	split.Draw(screen)
	height := func(box *tview.Box) int {
		_, _, _, h := box.GetRect()
		return h
	}
	at := func(action tview.MouseAction, y int, buttons tcell.ButtonMask) (tview.MouseAction, *tcell.EventMouse) {
		return split.MouseCapture(action, tcell.NewEventMouse(20, y, buttons, tcell.ModNone))
	}
	require.Equal(t, 10, height(top))

	// the columns of the boundary row are not the boundary
	_, event := at(tview.MouseLeftDown, 5, tcell.Button1)
	assert.NotNil(t, event)

	// hovered: the boundary rows are highlighted across the whole width
	_, event = at(tview.MouseMove, 10, tcell.ButtonNone)
	assert.Nil(t, event)
	split.Draw(screen)
	for _, x := range []int{0, 20, 39} {
		_, _, style, _ := screen.GetContent(x, 10)
		foreground, _, _ := style.Decompose()
		assert.Equal(t, theme.Primary, foreground, "column %d", x)
	}

	// dragged down, and not below the minimum height of the bottom pane
	_, event = at(tview.MouseLeftDown, 10, tcell.Button1)
	assert.Nil(t, event)
	time.Sleep(splitResizeInterval)
	at(tview.MouseMove, 20, tcell.Button1)
	split.Draw(screen)
	assert.Equal(t, 20, height(top))
	assert.Equal(t, 10, height(bottom))
	time.Sleep(splitResizeInterval)
	at(tview.MouseMove, 29, tcell.Button1)
	split.Draw(screen)
	assert.Equal(t, 30-splitMinPaneHeight, height(top))
	at(tview.MouseLeftUp, 29, tcell.ButtonNone)
	assert.False(t, split.dragging)
}

// Regression: dropping right after a move that was skipped by the throttling lost the final position.
func TestResizableSplit_DropAppliesSkippedMove(t *testing.T) {
	split, screen, left, _ := newTestSplit(t)

	mouse(split, tview.MouseLeftDown, 50, tcell.Button1)
	// within splitResizeInterval of the click: skipped for now
	mouse(split, tview.MouseMove, 70, tcell.Button1)
	split.Draw(screen)
	require.Equal(t, 50, width(left))

	mouse(split, tview.MouseLeftUp, 70, tcell.ButtonNone)
	split.Draw(screen)
	assert.Equal(t, 70, width(left), "applied when dropped")
}
