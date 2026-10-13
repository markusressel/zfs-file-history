package pool_activity

import (
	"zfs-file-history/internal/ui/clickable"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// linePanel is what the panels below the header of the pages have in common (see Bar and AlertBar): a panel with a
// border, one line per entry, and no height while there are none. The parts of the lines that can be clicked are
// added to regions while drawing.
type linePanel struct {
	*tview.Box

	// All fields below are only accessed on the UI thread.

	// lines returns the number of lines shown, e.g. from the Monitor, whose state is only accessed on the UI thread
	lines  func() int
	height int
	// onHeightChanged is called with the height (the number of lines and the border, 0 without lines) when it
	// changed
	onHeightChanged func(height int)
	// enabled returns whether the lines can be clicked, e.g. not while a dialog is in front
	enabled func() bool
	// regions are the parts of the lines that can be clicked, added by Draw
	regions clickable.Regions
}

// newLinePanel returns a panel showing the given number of lines. Must be called on the UI thread.
func newLinePanel(lines func() int) *linePanel {
	panel := &linePanel{
		Box:     tview.NewBox(),
		lines:   lines,
		enabled: func() bool { return true },
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
	if panel.onHeightChanged != nil {
		panel.onHeightChanged(height)
	}
}

// MouseHandler highlights the part of a line below the mouse and runs a clicked one, see regions. The panel never
// takes the focus.
func (panel *linePanel) MouseHandler() func(action tview.MouseAction, event *tcell.EventMouse, setFocus func(p tview.Primitive)) (consumed bool, capture tview.Primitive) {
	return panel.WrapMouseHandler(func(action tview.MouseAction, event *tcell.EventMouse, setFocus func(p tview.Primitive)) (consumed bool, capture tview.Primitive) {
		consumed, captures := panel.regions.HandleMouse(action, event, panel.enabled() && panel.InRect(event.Position()))
		if captures {
			return consumed, panel
		}
		return consumed, nil
	})
}
