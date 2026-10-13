// Package clickable has the parts of primitives that react to the mouse (hover and click), see Regions.
package clickable

import (
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// Region is a part of a line of the screen that reacts to the mouse: it is highlighted while below the mouse, and
// runs OnClick when clicked. See Regions.
type Region struct {
	// X, Y and Width are the position on the screen, as drawn
	X, Y, Width int
	// Key identifies what the region stands for across draws, so the hover stays while the screen is drawn again.
	// Regions with the same key are highlighted together (e.g. a label and the bar it belongs to). It must be
	// comparable.
	Key any
	// HoverBackground is painted over the drawn cells while the region is hovered, keeping their text.
	// tcell.ColorDefault: not highlighted itself (but a click still runs OnClick).
	HoverBackground tcell.Color
	// OnClick is called on the UI thread when the region is clicked, nil: nothing
	OnClick func()
}

// contains returns whether the position is in the region.
func (region *Region) contains(x int, y int) bool {
	return y == region.Y && x >= region.X && x < region.X+region.Width
}

// Regions are the parts of a primitive that react to the mouse, so it does not have to compute again where its
// parts are when the mouse moves. The primitive adds them while drawing (where it knows their positions anyway):
// Reset at the start of Draw, Add for each part, DrawHover at the end. Its mouse handler calls HandleMouse.
//
// Regions only exist once drawn, which is fine: nothing can be hovered before. The last added region wins where
// regions overlap, e.g. a key within an entry of the shortcut bar.
//
// Only accessed on the UI thread.
type Regions struct {
	regions []Region
	// hovered is the key of the region below the mouse, valid if isHovered
	hovered   any
	isHovered bool
}

// Reset removes all regions, before they are added again while drawing. The hovered key stays.
func (regions *Regions) Reset() {
	regions.regions = regions.regions[:0]
}

// Add adds a region. Regions without width are ignored.
func (regions *Regions) Add(region Region) {
	if region.Width > 0 {
		regions.regions = append(regions.regions, region)
	}
}

// ClearHover ends the hover, e.g. when the content changed and the hovered key may stand for something else now.
func (regions *Regions) ClearHover() {
	regions.hovered, regions.isHovered = nil, false
}

// IsHovered returns whether the regions of the key are below the mouse.
func (regions *Regions) IsHovered(key any) bool {
	return regions.isHovered && regions.hovered == key
}

// Hovered returns the key of the regions below the mouse, false if none.
func (regions *Regions) Hovered() (key any, ok bool) {
	return regions.hovered, regions.isHovered
}

// DrawHover paints the background of the hovered regions over what was drawn. Call it at the end of Draw.
func (regions *Regions) DrawHover(screen tcell.Screen) {
	if !regions.isHovered {
		return
	}
	for _, region := range regions.regions {
		if region.Key != regions.hovered || region.HoverBackground == tcell.ColorDefault {
			continue
		}
		for x := region.X; x < region.X+region.Width; x++ {
			text, style, _ := screen.Get(x, region.Y)
			screen.Put(x, region.Y, text, style.Background(region.HoverBackground))
		}
	}
}

// at returns the region at the position, the last added one where they overlap, nil if none.
func (regions *Regions) at(x int, y int) *Region {
	for i := len(regions.regions) - 1; i >= 0; i-- {
		if regions.regions[i].contains(x, y) {
			return &regions.regions[i]
		}
	}
	return nil
}

// HandleMouse updates the hovered region and runs OnClick of a clicked one. active is whether the mouse is on the
// primitive and it reacts to it (e.g. not while a dialog is in front, see rivo/tview#926). It returns whether the
// event was consumed (the hover changed, which needs a redraw, or a click), and whether the primitive should
// capture the mouse: while it is on it, so it learns when the mouse leaves (tview only sends the events of other
// positions to a capturing primitive).
func (regions *Regions) HandleMouse(action tview.MouseAction, event *tcell.EventMouse, active bool) (consumed bool, capture bool) {
	if !active {
		changed := regions.isHovered
		regions.ClearHover()
		return changed, false
	}
	x, y := event.Position()
	region := regions.at(x, y)
	var changed bool
	if region == nil {
		changed = regions.isHovered
		regions.ClearHover()
	} else {
		changed = !regions.IsHovered(region.Key)
		regions.hovered, regions.isHovered = region.Key, true
	}
	if action == tview.MouseLeftClick && region != nil && region.OnClick != nil {
		region.OnClick()
	}
	return changed || action == tview.MouseLeftDown || action == tview.MouseLeftClick, true
}
