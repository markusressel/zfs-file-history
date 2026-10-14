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
	// splitMinPaneWidth is the minimum width of each pane of a split side by side
	splitMinPaneWidth = 20
	// splitMinPaneHeight is the minimum height of each pane of a split one above the other
	splitMinPaneHeight = 4
)

// ResizableSplit shows two panes side by side (or one above the other, see NewVerticalResizableSplit). The boundary
// between them can be dragged with the mouse; it is highlighted while hovered or dragged. A right click on it goes
// back to the initial sizes (a double click cannot be used: tview's double clicks are disabled, see
// tview.DoubleClickInterval in root.go).
//
// Without a drag, the first pane can be sized to fit its content (see SetAutoSize), and the split can arrange the
// panes depending on the available space (see SetVerticalFunc).
//
// Register MouseCapture on the top-level layout (e.g. the dialog), not on the split itself, so a drag is not lost
// when the mouse leaves the split. All methods run on the UI thread.
type ResizableSplit struct {
	*tview.Flex
	application *tview.Application
	// left and right are the panes, top and bottom if vertical
	left     tview.Primitive
	right    tview.Primitive
	vertical bool
	// enabled returns whether the boundary reacts to the mouse, e.g. not while a dialog is shown above the split
	enabled func() bool
	// autoSize returns the size of the first pane, see SetAutoSize
	autoSize func(size int) int
	// verticalFunc returns whether the panes are arranged one above the other, see SetVerticalFunc
	verticalFunc func(width int, height int) bool
	// arrangements are the sizes of the panes side by side, and one above the other (see arrangement)
	arrangements [2]splitArrangement

	hovered    bool
	dragging   bool
	lastResize time.Time
	// trailingResize applies pendingPosition, the last position of a drag that was skipped by the throttling
	trailingResize  *time.Timer
	pendingPosition int
}

// splitArrangement are the sizes of the panes in one direction.
type splitArrangement struct {
	// firstProportion and secondProportion are the initial proportions of the panes
	firstProportion  int
	secondProportion int
	// dragged is set once the boundary was dragged: the dragged proportions are kept, instead of the auto size
	dragged                 bool
	draggedFirstProportion  int
	draggedSecondProportion int
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
	initial := splitArrangement{firstProportion: leftProportion, secondProportion: rightProportion}
	split.arrangements = [2]splitArrangement{initial, initial}
	split.AddItem(left, 0, leftProportion, true)
	split.AddItem(right, 0, rightProportion, false)
	return split
}

// NewVerticalResizableSplit creates a split with one pane above the other, with the given initial proportions.
func NewVerticalResizableSplit(application *tview.Application, top tview.Primitive, bottom tview.Primitive, topProportion int, bottomProportion int) *ResizableSplit {
	split := NewResizableSplit(application, top, bottom, topProportion, bottomProportion)
	split.vertical = true
	split.SetDirection(tview.FlexRow)
	return split
}

// SetEnabledFunc sets a function that returns whether the boundary reacts to the mouse.
func (s *ResizableSplit) SetEnabledFunc(enabled func() bool) *ResizableSplit {
	s.enabled = enabled
	return s
}

// SetAutoSize sets a function that returns the size of the first pane (columns, or rows if vertical), given the
// size of the split, e.g. the size of its content. It is called on every draw, so the pane follows its content and
// the size of the terminal. 0 uses the proportions. Once the boundary was dragged, the dragged proportions are used
// instead, until it is right-clicked.
func (s *ResizableSplit) SetAutoSize(autoSize func(size int) int) *ResizableSplit {
	s.autoSize = autoSize
	return s
}

// SetVerticalFunc sets a function that returns whether the panes are arranged one above the other (or side by
// side), given the size of the split. It is called on every draw. Both arrangements keep their own sizes, e.g. a
// drag in one of them is kept when switching to the other one and back.
func (s *ResizableSplit) SetVerticalFunc(vertical func(width int, height int) bool) *ResizableSplit {
	s.verticalFunc = vertical
	return s
}

// SetProportions sets the initial proportions of the panes one above the other (vertical), or side by side, for a
// split that switches between them (see SetVerticalFunc).
func (s *ResizableSplit) SetProportions(vertical bool, firstProportion int, secondProportion int) *ResizableSplit {
	arrangement := &s.arrangements[arrangementIndex(vertical)]
	arrangement.firstProportion, arrangement.secondProportion = firstProportion, secondProportion
	return s
}

// IsVertical returns whether the panes are currently arranged one above the other.
func (s *ResizableSplit) IsVertical() bool {
	return s.vertical
}

func arrangementIndex(vertical bool) int {
	if vertical {
		return 1
	}
	return 0
}

// arrangement returns the sizes of the panes in the current direction.
func (s *ResizableSplit) arrangement() *splitArrangement {
	return &s.arrangements[arrangementIndex(s.vertical)]
}

// minPaneSize returns the minimum size of each pane, given the size of the split.
func (s *ResizableSplit) minPaneSize(size int) int {
	minSize := splitMinPaneWidth
	if s.vertical {
		minSize = splitMinPaneHeight
	}
	return min(minSize, size/2)
}

// arrange sets the direction and the sizes of the panes for the current size of the split, before it is drawn.
func (s *ResizableSplit) arrange() {
	_, _, width, height := s.GetRect()
	if s.verticalFunc != nil {
		if vertical := s.verticalFunc(width, height); vertical != s.vertical {
			s.vertical = vertical
			// the boundary moved: a drag (or its last position) does not apply anymore
			s.hovered, s.dragging = false, false
			if s.trailingResize != nil {
				s.trailingResize.Stop()
				s.trailingResize = nil
			}
			if vertical {
				s.SetDirection(tview.FlexRow)
			} else {
				s.SetDirection(tview.FlexColumn)
			}
		}
	}

	size := width
	if s.vertical {
		size = height
	}
	arrangement := s.arrangement()
	autoSize := 0
	if s.autoSize != nil && !arrangement.dragged {
		autoSize = s.autoSize(size)
	}
	switch {
	case arrangement.dragged:
		s.ResizeItem(s.left, 0, arrangement.draggedFirstProportion)
		s.ResizeItem(s.right, 0, arrangement.draggedSecondProportion)
	case autoSize > 0:
		minSize := s.minPaneSize(size)
		s.ResizeItem(s.left, max(minSize, min(autoSize, size-minSize)), 0)
		s.ResizeItem(s.right, 0, 1)
	default:
		s.ResizeItem(s.left, 0, arrangement.firstProportion)
		s.ResizeItem(s.right, 0, arrangement.secondProportion)
	}
}

// boundary returns the two columns (rows if vertical) of the boundary: the last one of the first pane and the first
// one of the second pane (usually their borders).
func (s *ResizableSplit) boundary() (int, int) {
	paneX, paneY, _, _ := s.right.GetRect()
	if s.vertical {
		return paneY - 1, paneY
	}
	return paneX - 1, paneX
}

// along returns the position across the boundary (x, or y if vertical) and the position along it.
func (s *ResizableSplit) along(x int, y int) (across int, alongBoundary int) {
	if s.vertical {
		return y, x
	}
	return x, y
}

// isOnBoundary returns whether the position is on the boundary.
func (s *ResizableSplit) isOnBoundary(x int, y int) bool {
	splitX, splitY, width, height := s.GetRect()
	start, length := splitY, height
	if s.vertical {
		start, length = splitX, width
	}
	first, second := s.boundary()
	across, position := s.along(x, y)
	return (across == first || across == second) && position >= start && position < start+length
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
	position, _ := s.along(x, y)

	if s.dragging {
		if event.Buttons() == tcell.ButtonNone || action == tview.MouseLeftUp {
			s.dragging = false
			s.hovered = s.isOnBoundary(x, y)
			if s.trailingResize != nil {
				// dropped before the skipped position was applied: apply it now, not lose it
				s.trailingResize.Stop()
				s.trailingResize = nil
				s.resizeTo(s.pendingPosition)
			}
			return tview.MouseConsumed, nil
		}
		if time.Since(s.lastResize) >= splitResizeInterval {
			s.lastResize = time.Now()
			if s.trailingResize != nil {
				s.trailingResize.Stop()
				s.trailingResize = nil
			}
			s.resizeTo(position)
			return tview.MouseConsumed, nil
		}
		// skipped: apply the final position later, without redrawing now
		if s.trailingResize != nil {
			s.trailingResize.Stop()
		}
		s.pendingPosition = position
		var trailingResize *time.Timer
		trailingResize = time.AfterFunc(splitResizeInterval, func() {
			s.application.QueueUpdateDraw(func() {
				// unless it was applied when dropped, or replaced by a newer position
				if s.trailingResize == trailingResize {
					s.trailingResize = nil
					s.resizeTo(position)
				}
			})
		})
		s.trailingResize = trailingResize
		return action, nil
	}

	onBoundary := s.isOnBoundary(x, y)
	if action == tview.MouseRightClick && onBoundary {
		// back to the initial sizes (or the auto size), applied when it is drawn
		s.arrangement().dragged = false
		return tview.MouseConsumed, nil
	}
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

// resizeTo moves the boundary to the given column (row if vertical), keeping a minimum size for both panes.
func (s *ResizableSplit) resizeTo(position int) {
	splitX, splitY, width, height := s.GetRect()
	start, size := splitX, width
	if s.vertical {
		start, size = splitY, height
	}
	minSize := s.minPaneSize(size)
	firstSize := max(minSize, min(position-start, size-minSize))
	// as proportions, so the panes keep their ratio when the terminal is resized. Applied when it is drawn.
	arrangement := s.arrangement()
	arrangement.dragged = true
	arrangement.draggedFirstProportion, arrangement.draggedSecondProportion = firstSize, size-firstSize
}

// Draw draws the panes, and highlights the boundary while it is hovered or dragged.
func (s *ResizableSplit) Draw(screen tcell.Screen) {
	before, _ := s.boundary()
	s.arrange()
	s.Flex.Draw(screen)
	if after, _ := s.boundary(); after != before && !s.dragging {
		// moved away from the mouse, e.g. by the auto size: highlighted again once the mouse is on it
		s.hovered = false
	}
	if !s.hovered && !s.dragging {
		return
	}
	x, y, width, height := s.GetRect()
	first, second := s.boundary()
	highlight := func(column int, row int) {
		str, style, _ := screen.Get(column, row)
		screen.Put(column, row, str, style.Foreground(theme.Primary))
	}
	for _, line := range []int{first, second} {
		if s.vertical {
			for column := x; column < x+width; column++ {
				highlight(column, line)
			}
		} else {
			for row := y; row < y+height; row++ {
				highlight(line, row)
			}
		}
	}
}
