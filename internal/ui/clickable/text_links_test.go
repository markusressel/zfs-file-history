package clickable

import (
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"github.com/stretchr/testify/assert"
)

// scrolledText is a text at 10,5 of 20x3 cells, scrolled by row and column.
type scrolledText struct {
	row, column int
}

func (text scrolledText) GetInnerRect() (int, int, int, int) { return 10, 5, 20, 3 }
func (text scrolledText) GetScrollOffset() (int, int)        { return text.row, text.column }

func TestRegions_AddTextLinks(t *testing.T) {
	links := []TextLink{
		{Line: 0, Column: 2, Width: 4, Key: "a"},
		// cut off at the right edge
		{Line: 1, Column: 18, Width: 5, Key: "b"},
		// below the text
		{Line: 3, Column: 0, Width: 3, Key: "c"},
	}
	regions := &Regions{}
	regions.AddTextLinks(scrolledText{}, links, hover)
	assert.Equal(t, []Region{
		{X: 12, Y: 5, Width: 4, Key: "a", HoverBackground: hover},
		{X: 28, Y: 6, Width: 2, Key: "b", HoverBackground: hover},
	}, regions.regions)

	// scrolled down and right: the first line is out, the others move up and left, cut off at the left edge
	regions.Reset()
	regions.AddTextLinks(scrolledText{row: 1, column: 19}, links, hover)
	assert.Equal(t, []Region{{X: 10, Y: 5, Width: 4, Key: "b", HoverBackground: hover}}, regions.regions,
		"c is scrolled out to the left")
}

// area is a primitive at 0,0 of 10x1 cells.
type area struct {
	*tview.Box
}

func TestRegions_WrapMouseHandler(t *testing.T) {
	box := area{tview.NewBox()}
	box.SetRect(0, 0, 10, 1)
	var clicked, handled []string
	regions := &Regions{}
	regions.Add(Region{X: 2, Y: 0, Width: 3, Key: "link", HoverBackground: hover, OnClick: func() { clicked = append(clicked, "link") }})
	enabled := true
	handler := regions.WrapMouseHandler(box, func() bool { return enabled },
		func(action tview.MouseAction, event *tcell.EventMouse, setFocus func(p tview.Primitive)) (bool, tview.Primitive) {
			handled = append(handled, map[tview.MouseAction]string{tview.MouseLeftDown: "down", tview.MouseLeftClick: "click", tview.MouseMove: "move"}[action])
			return true, nil
		})
	mouse := func(action tview.MouseAction, x int) (bool, tview.Primitive) {
		return handler(action, tcell.NewEventMouse(x, 0, tcell.ButtonNone, tcell.ModNone), func(p tview.Primitive) {})
	}

	// a click on the link runs it, the press before (e.g. to focus) goes to the handler
	_, capture := mouse(tview.MouseMove, 3)
	assert.Equal(t, box, capture, "captured while hovered")
	mouse(tview.MouseLeftDown, 3)
	mouse(tview.MouseLeftClick, 3)
	assert.Equal(t, []string{"link"}, clicked)
	assert.Equal(t, []string{"move", "down"}, handled)

	// next to it: the handler
	_, capture = mouse(tview.MouseLeftClick, 7)
	assert.Nil(t, capture)
	assert.Len(t, clicked, 1)
	assert.Equal(t, []string{"move", "down", "click"}, handled)

	// not while disabled
	enabled = false
	mouse(tview.MouseLeftClick, 3)
	assert.Len(t, clicked, 1)
}
