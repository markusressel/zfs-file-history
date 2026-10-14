package scrollbar

import (
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// TextView is a tview.TextView with a vertical scrollbar while its text does not fit, like the tables have. While
// the bar is shown, the view has a column of right padding more, where the bar is drawn: the text never runs below
// it, and a selection of the text (see uiutil.TextSelection, which selects within the inner rect) leaves it out.
type TextView struct {
	*tview.TextView
	application *tview.Application
	bar         *ScrollbarComponent

	// All fields below are only accessed on the UI thread.

	// paddingTop, paddingBottom, paddingLeft and paddingRight are the padding set with SetBorderPadding, without the
	// column of the bar
	paddingTop, paddingBottom, paddingLeft, paddingRight int
	// barShown is set while the bar is shown, see BarWidth
	barShown bool
}

// NewTextView creates a text view with a scrollbar. The application is drawn again when the bar comes or goes (see
// BarWidth); nil if nothing is sized by the content of the view. Must be called on the UI thread.
func NewTextView(application *tview.Application) *TextView {
	return &TextView{
		TextView:    tview.NewTextView(),
		application: application,
		bar:         NewScrollbarComponent(application, ScrollBarVertical, 0, 0, 0, 0),
	}
}

// SetBorderPadding sets the padding around the text. The column of the bar is added to the right one while it is
// shown.
func (view *TextView) SetBorderPadding(top int, bottom int, left int, right int) *TextView {
	view.paddingTop, view.paddingBottom, view.paddingLeft, view.paddingRight = top, bottom, left, right
	view.applyPadding()
	return view
}

func (view *TextView) applyPadding() {
	view.TextView.SetBorderPadding(view.paddingTop, view.paddingBottom, view.paddingLeft, view.paddingRight+view.BarWidth())
}

// BarWidth returns the width the bar takes while it is shown (1), 0 otherwise. Panes sized by their content add it
// to their width, so the bar does not cut off their text.
func (view *TextView) BarWidth() int {
	if view.barShown {
		return 1
	}
	return 0
}

// overflows returns whether the text has more lines than the view, as last drawn.
func (view *TextView) overflows() bool {
	_, _, _, height := view.GetInnerRect()
	return height > 0 && view.GetWrappedLineCount() > height
}

// Draw draws the text, then the scrollbar from the scroll offset tview just set (it keeps the offset within the
// text while drawing).
func (view *TextView) Draw(screen tcell.Screen) {
	view.TextView.Draw(screen)
	// wrapped lines are only known after drawing at the width without or with the bar
	if overflows := view.overflows(); overflows != view.barShown {
		view.barShown = overflows
		view.applyPadding()
		view.TextView.Draw(screen)
		// e.g. a pane sized by its content takes the column of the bar (see BarWidth); in the background: queued
		// from the UI thread, it would wait for itself
		if view.application != nil {
			go view.application.QueueUpdateDraw(func() {})
		}
	}
	if !view.barShown {
		return
	}
	x, y, width, height := view.GetInnerRect()
	if width <= 0 || height <= 0 {
		return
	}
	offset, _ := view.GetScrollOffset()
	view.bar.SetMax(view.GetWrappedLineCount())
	view.bar.SetPosition(offset)
	view.bar.SetWidth(height)
	layout := view.bar.GetLayout()
	// the padding column right of the text
	layout.SetRect(x+width, y, 1, height)
	layout.Draw(screen)
}
