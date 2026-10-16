package clickable

import (
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// TextLink is a part of a text that reacts to the mouse, by its position in the text (in cells, not scrolled), e.g. the
// name of a pool in the details of a disk. See Regions.AddTextLinks.
type TextLink struct {
	Line, Column, Width int
	// Key identifies it across draws, see Region
	Key any
	// OnClick is called on the UI thread when it is clicked
	OnClick func()
}

// ScrolledText is a primitive that draws a text scrolled by an offset into its inner rect, e.g. a tview.TextView.
type ScrolledText interface {
	GetInnerRect() (x int, y int, width int, height int)
	GetScrollOffset() (row int, column int)
}

// AddTextLinks adds the links where the text shows them: scrolled by its offset, cut off at its inner rect (links
// scrolled out are left out). Call it while drawing, after the text was drawn.
func (regions *Regions) AddTextLinks(text ScrolledText, links []TextLink, hoverBackground tcell.Color) {
	x, y, width, height := text.GetInnerRect()
	row, column := text.GetScrollOffset()
	for _, link := range links {
		linkY := y + link.Line - row
		if linkY < y || linkY >= y+height {
			continue
		}
		start := max(x+link.Column-column, x)
		end := min(x+link.Column-column+link.Width, x+width)
		regions.Add(Region{X: start, Y: linkY, Width: end - start, Key: link.Key, HoverBackground: hoverBackground,
			OnClick: link.OnClick})
	}
}

// MouseHandler is the mouse handler of a tview primitive.
type MouseHandler = func(action tview.MouseAction, event *tcell.EventMouse, setFocus func(p tview.Primitive)) (consumed bool, capture tview.Primitive)

// Area is a primitive the regions are drawn on.
type Area interface {
	tview.Primitive
	InRect(x int, y int) bool
}

// WrapMouseHandler returns the mouse handler of a primitive with regions (e.g. links in its text, see AddTextLinks): a
// click on a region runs it, everything else goes to handler (e.g. a press focuses, the wheel scrolls), also on a
// region. While enabled returns false (e.g. a dialog is in front, see rivo/tview#926), the regions do nothing. While
// the mouse is on a region, the primitive captures the mouse, so it learns when it leaves.
func (regions *Regions) WrapMouseHandler(area Area, enabled func() bool, handler MouseHandler) MouseHandler {
	return func(action tview.MouseAction, event *tcell.EventMouse, setFocus func(p tview.Primitive)) (consumed bool, capture tview.Primitive) {
		x, y := event.Position()
		active := enabled() && area.InRect(x, y)
		hovered, wasHovered := regions.Hovered()
		if active && action == tview.MouseLeftClick && regions.Contains(x, y) {
			// a click on a region does only that, e.g. it does not select the row of a table
			regions.HandleMouse(action, event, active)
			return true, area
		}
		regions.HandleMouse(action, event, active)
		consumed, capture = handler(action, event, setFocus)
		if capture == nil && regions.IsAnyHovered() {
			capture = area
		}
		// a hover that changed needs a redraw, also from one region to the next
		key, isHovered := regions.Hovered()
		return consumed || wasHovered != isHovered || key != hovered, capture
	}
}
