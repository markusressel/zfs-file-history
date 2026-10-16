package util

import (
	"zfs-file-history/internal/ui/clickable"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// MouseHandler is the mouse handler of a tview primitive.
type MouseHandler = clickable.MouseHandler

// WithoutFocus returns the mouse handler of a pane there is nothing to do in (which Tab skips, e.g. the path
// overview): it handles the mouse as before (e.g. scrolling), but a click does not focus the pane, which tview does
// for every primitive.
func WithoutFocus(handler MouseHandler) MouseHandler {
	return func(action tview.MouseAction, event *tcell.EventMouse, setFocus func(p tview.Primitive)) (bool, tview.Primitive) {
		return handler(action, event, func(tview.Primitive) {})
	}
}
