package pool_activity

import (
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// noLine is the hovered line while the mouse is not on the panel.
const noLine = -1

// linePanel is what the panels below the header of the pages have in common (see Bar and AlertBar): a panel with a
// border, one line per entry, and no height while there are none. A click on a line runs onClickLine.
type linePanel struct {
	*tview.Box

	// All fields below are only accessed on the UI thread.

	// lines returns the number of lines shown, e.g. from the Monitor, whose state is only accessed on the UI thread
	lines  func() int
	height int
	// onHeightChanged is called with the height (the number of lines and the border, 0 without lines) when it
	// changed
	onHeightChanged func(height int)
	// onClickLine is called with the index of the clicked line and the clicked column in it, nil: lines cannot be
	// clicked
	onClickLine func(line int, column int)
	// enabled returns whether the lines can be clicked, e.g. not while a dialog is in front
	enabled func() bool
	// hovered is the index of the line below the mouse, noLine if none
	hovered int
	// regionAt returns which part of a line is at the column (relative to the inner rect), e.g. the parts that do
	// different things when clicked, so only the part below the mouse is highlighted. Moving the mouse within a
	// part does not redraw. nil: the whole line is one part.
	regionAt func(line int, column int) int
	// hoveredRegion is the part of the hovered line below the mouse, see regionAt
	hoveredRegion int
}

// newLinePanel returns a panel showing the given number of lines. Must be called on the UI thread.
func newLinePanel(lines func() int) *linePanel {
	panel := &linePanel{
		Box:     tview.NewBox(),
		lines:   lines,
		enabled: func() bool { return true },
		hovered: noLine,
	}
	panel.SetBorder(true)
	return panel
}

// SetOnHeightChanged sets the function called with the height the panel needs (the number of lines and the
// border, 0 without lines) whenever it changed, e.g. to resize it in its layout. Must be called on the UI thread.
func (panel *linePanel) SetOnHeightChanged(onHeightChanged func(height int)) {
	panel.onHeightChanged = onHeightChanged
}

// SetEnabledFunc sets a function that returns whether the lines can be clicked, e.g. not while a dialog is in front
// (tview hands mouse events to all pages, see rivo/tview#926). Must be called on the UI thread.
func (panel *linePanel) SetEnabledFunc(enabled func() bool) {
	panel.enabled = enabled
}

// Height returns the number of lines the panel needs, including the border (0 without lines). Must be called on
// the UI thread.
func (panel *linePanel) Height() int {
	return panel.height
}

// update reports the new height after the lines changed. Runs on the UI thread.
func (panel *linePanel) update() {
	lines := panel.lines()
	height := 0
	if lines > 0 {
		// the border
		height = lines + 2
	}
	if height == panel.height {
		return
	}
	panel.height = height
	if panel.hovered >= lines {
		panel.hovered = noLine
	}
	if panel.onHeightChanged != nil {
		panel.onHeightChanged(height)
	}
}

// drawHover fills the line below the mouse with the given background, if lines can be clicked.
func (panel *linePanel) drawHover(screen tcell.Screen, background tcell.Color) {
	if panel.hovered == noLine || panel.onClickLine == nil {
		return
	}
	x, y, width, height := panel.GetInnerRect()
	if panel.hovered >= height {
		return
	}
	style := tcell.StyleDefault.Background(background)
	for column := x; column < x+width; column++ {
		screen.SetContent(column, y+panel.hovered, ' ', nil, style)
	}
}

// MouseHandler highlights the line below the mouse and reports a clicked one, see onClickLine. The panel never takes
// the focus.
func (panel *linePanel) MouseHandler() func(action tview.MouseAction, event *tcell.EventMouse, setFocus func(p tview.Primitive)) (consumed bool, capture tview.Primitive) {
	return panel.WrapMouseHandler(func(action tview.MouseAction, event *tcell.EventMouse, setFocus func(p tview.Primitive)) (consumed bool, capture tview.Primitive) {
		x, y := event.Position()
		if panel.onClickLine == nil || !panel.enabled() || !panel.InRect(x, y) {
			// e.g. moved away: tview only sends the events of other positions while the panel captures the mouse
			changed := panel.hovered != noLine
			panel.hovered = noLine
			return changed, nil
		}
		left, top, _, _ := panel.GetInnerRect()
		line := y - top
		if line < 0 || line >= panel.lines() {
			line = noLine
		}
		region := 0
		if line != noLine && panel.regionAt != nil {
			region = panel.regionAt(line, x-left)
		}
		changed := line != panel.hovered || region != panel.hoveredRegion
		panel.hovered, panel.hoveredRegion = line, region
		if action == tview.MouseLeftClick && line != noLine {
			panel.onClickLine(line, x-left)
		}
		// captures the mouse while on the panel, to know when it leaves it
		return changed || action == tview.MouseLeftDown || action == tview.MouseLeftClick, panel
	})
}
