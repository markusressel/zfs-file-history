package util

import (
	"time"
	"zfs-file-history/internal/ui/theme"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

const (
	// splitResizeInterval throttles resizing while dragging (see the tview rules in AGENTS.md)
	splitResizeInterval = 30 * time.Millisecond
	// splitMinPaneWidth is the minimum width of each pane
	splitMinPaneWidth = 20
)

// ResizableSplit shows two panes side by side. The boundary between them can be dragged with the mouse, like the
// panes of the main page; it is highlighted while hovered or dragged.
//
// Register MouseCapture on the top-level layout (e.g. the dialog), not on the split itself, so a drag is not lost
// when the mouse leaves the split. All methods run on the UI thread.
type ResizableSplit struct {
	*tview.Flex
	application *tview.Application
	left        tview.Primitive
	right       tview.Primitive
	// enabled returns whether the boundary reacts to the mouse, e.g. not while a dialog is shown above the split
	enabled func() bool

	hovered    bool
	dragging   bool
	lastResize time.Time
	// trailingResize applies the last position of a drag that was skipped by the throttling
	trailingResize *time.Timer
}

// NewResizableSplit creates a split with the given initial proportions of the panes.
func NewResizableSplit(application *tview.Application, left tview.Primitive, right tview.Primitive, leftProportion int, rightProportion int) *ResizableSplit {
	split := &ResizableSplit{
		Flex:        tview.NewFlex(),
		application: application,
		left:        left,
		right:       right,
		enabled:     func() bool { return true },
	}
	split.AddItem(left, 0, leftProportion, true)
	split.AddItem(right, 0, rightProportion, false)
	return split
}

// SetEnabledFunc sets a function that returns whether the boundary reacts to the mouse.
func (s *ResizableSplit) SetEnabledFunc(enabled func() bool) *ResizableSplit {
	s.enabled = enabled
	return s
}

// boundaryColumns returns the two columns of the boundary: the last one of the left pane and the first one of
// the right pane (usually their borders).
func (s *ResizableSplit) boundaryColumns() (int, int) {
	rightX, _, _, _ := s.right.GetRect()
	return rightX - 1, rightX
}

// isOnBoundary returns whether the position is on the boundary.
func (s *ResizableSplit) isOnBoundary(x int, y int) bool {
	_, splitY, _, height := s.GetRect()
	first, second := s.boundaryColumns()
	return (x == first || x == second) && y >= splitY && y < splitY+height
}

// MouseCapture handles hovering and dragging the boundary. Events it does not handle are passed on.
func (s *ResizableSplit) MouseCapture(action tview.MouseAction, event *tcell.EventMouse) (tview.MouseAction, *tcell.EventMouse) {
	if event == nil {
		return action, event
	}
	if !s.enabled() {
		s.hovered, s.dragging = false, false
		return action, event
	}
	x, y := event.Position()

	if s.dragging {
		if event.Buttons() == tcell.ButtonNone || action == tview.MouseLeftUp {
			s.dragging = false
			s.hovered = s.isOnBoundary(x, y)
			if s.trailingResize != nil {
				s.trailingResize.Stop()
				s.trailingResize = nil
			}
			return tview.MouseConsumed, nil
		}
		if time.Since(s.lastResize) >= splitResizeInterval {
			s.lastResize = time.Now()
			if s.trailingResize != nil {
				s.trailingResize.Stop()
				s.trailingResize = nil
			}
			s.resizeTo(x)
			return tview.MouseConsumed, nil
		}
		// skipped: apply the final position later, without redrawing now
		if s.trailingResize != nil {
			s.trailingResize.Stop()
		}
		s.trailingResize = time.AfterFunc(splitResizeInterval, func() {
			s.application.QueueUpdateDraw(func() { s.resizeTo(x) })
		})
		return action, nil
	}

	onBoundary := s.isOnBoundary(x, y)
	if action == tview.MouseLeftDown && onBoundary {
		s.dragging = true
		s.lastResize = time.Now()
		return tview.MouseConsumed, nil
	}
	if onBoundary != s.hovered && action == tview.MouseMove {
		s.hovered = onBoundary
		// consumed, so tview redraws the highlight (a plain move has nothing else to do)
		return tview.MouseConsumed, nil
	}
	s.hovered = onBoundary
	return action, event
}

// resizeTo moves the boundary to the given column, keeping a minimum width for both panes.
func (s *ResizableSplit) resizeTo(x int) {
	splitX, _, width, _ := s.GetRect()
	minWidth := min(splitMinPaneWidth, width/2)
	leftWidth := max(minWidth, min(x-splitX, width-minWidth))
	// as proportions, so the panes keep their ratio when the terminal is resized
	s.ResizeItem(s.left, 0, leftWidth)
	s.ResizeItem(s.right, 0, width-leftWidth)
}

// Draw draws the panes, and highlights the boundary while it is hovered or dragged.
func (s *ResizableSplit) Draw(screen tcell.Screen) {
	s.Flex.Draw(screen)
	if !s.hovered && !s.dragging {
		return
	}
	_, y, _, height := s.GetRect()
	first, second := s.boundaryColumns()
	for row := y; row < y+height; row++ {
		for _, column := range []int{first, second} {
			mainc, combc, style, _ := screen.GetContent(column, row)
			screen.SetContent(column, row, mainc, combc, style.Foreground(theme.Primary))
		}
	}
}
