package clickable

import (
	"testing"
	"zfs-file-history/internal/testutil"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"github.com/stretchr/testify/assert"
)

var (
	hover      = tcell.ColorIsRGB | tcell.ColorValid | 0x3A434D
	keyHover   = tcell.ColorIsRGB | tcell.ColorValid | 0x44385A
	background = tcell.ColorIsRGB | tcell.ColorValid | 0x16191C
)

// mouseAt sends a mouse event at the position to the regions.
func mouseAt(regions *Regions, action tview.MouseAction, x int, y int, active bool) (consumed bool, capture bool) {
	return regions.HandleMouse(action, tcell.NewEventMouse(x, y, tcell.ButtonNone, tcell.ModNone), active)
}

// testRegions are "label" (columns 0-4) and "bar" (5-14) of the key "pool", and "key" (7-8) within the bar, with its
// own key, added last. clicked records the clicked regions.
func testRegions(clicked *[]string) *Regions {
	regions := &Regions{}
	regions.Add(Region{X: 0, Y: 1, Width: 5, Key: "pool", HoverBackground: hover, OnClick: func() { *clicked = append(*clicked, "label") }})
	regions.Add(Region{X: 5, Y: 1, Width: 10, Key: "pool", HoverBackground: tcell.ColorDefault, OnClick: func() { *clicked = append(*clicked, "bar") }})
	regions.Add(Region{X: 7, Y: 1, Width: 2, Key: "key", HoverBackground: keyHover, OnClick: func() { *clicked = append(*clicked, "key") }})
	regions.Add(Region{X: 20, Y: 1, Width: 0, Key: "empty", OnClick: func() { *clicked = append(*clicked, "empty") }})
	return regions
}

func TestRegions_Click(t *testing.T) {
	var clicked []string
	regions := testRegions(&clicked)

	mouseAt(regions, tview.MouseLeftClick, 2, 1, true)
	mouseAt(regions, tview.MouseLeftClick, 12, 1, true)
	// overlapping: the last added one
	mouseAt(regions, tview.MouseLeftClick, 8, 1, true)
	// another line, next to them, a region without width
	mouseAt(regions, tview.MouseLeftClick, 2, 0, true)
	mouseAt(regions, tview.MouseLeftClick, 15, 1, true)
	mouseAt(regions, tview.MouseLeftClick, 20, 1, true)
	// e.g. a dialog in front
	consumed, capture := mouseAt(regions, tview.MouseLeftClick, 2, 1, false)

	assert.Equal(t, []string{"label", "bar", "key"}, clicked)
	assert.False(t, consumed)
	assert.False(t, capture)
}

func TestRegions_Hover(t *testing.T) {
	var clicked []string
	regions := testRegions(&clicked)

	consumed, capture := mouseAt(regions, tview.MouseMove, 12, 1, true)
	assert.True(t, consumed, "redrawn")
	assert.True(t, capture, "captures the mouse, to know when it leaves")
	assert.True(t, regions.IsHovered("pool"))

	consumed, _ = mouseAt(regions, tview.MouseMove, 2, 1, true)
	assert.False(t, consumed, "the same key: not drawn again")
	consumed, _ = mouseAt(regions, tview.MouseMove, 8, 1, true)
	assert.True(t, consumed)
	assert.True(t, regions.IsHovered("key"))
	assert.False(t, regions.IsHovered("pool"))

	// on the primitive, but on no region
	consumed, capture = mouseAt(regions, tview.MouseMove, 15, 1, true)
	assert.True(t, consumed)
	assert.True(t, capture)
	_, hovered := regions.Hovered()
	assert.False(t, hovered)

	// drawn again: the hover stays
	mouseAt(regions, tview.MouseMove, 2, 1, true)
	regions.Reset()
	regions.Add(Region{X: 0, Y: 1, Width: 5, Key: "pool"})
	assert.True(t, regions.IsHovered("pool"))
	regions.ClearHover()
	assert.False(t, regions.IsHovered("pool"))

	assert.Empty(t, clicked)
}

func TestRegions_DrawHover(t *testing.T) {
	screen := testutil.NewScreen(t, 20, 2)
	draw := func(regions *Regions) {
		for x := 0; x < 20; x++ {
			screen.SetContent(x, 1, 'a'+rune(x), nil, tcell.StyleDefault.Background(background))
		}
		regions.DrawHover(screen)
		screen.Show()
	}
	cell := func(x int) (string, tcell.Color) {
		text, style, _ := screen.Get(x, 1)
		_, color, _ := style.Decompose()
		return text, color
	}
	var clicked []string
	regions := testRegions(&clicked)

	draw(regions)
	_, color := cell(2)
	assert.Equal(t, background, color, "nothing hovered")

	// the bar: all regions of its key, which have a hover background
	mouseAt(regions, tview.MouseMove, 12, 1, true)
	draw(regions)
	for x := 0; x < 15; x++ {
		text, color := cell(x)
		assert.Equal(t, string('a'+rune(x)), text, "the text stays at %d", x)
		if x < 5 {
			assert.Equal(t, hover, color, "the label at %d", x)
		} else {
			assert.Equal(t, background, color, "the bar at %d", x)
		}
	}

	// the key within the bar: only it
	mouseAt(regions, tview.MouseMove, 8, 1, true)
	draw(regions)
	for x := 0; x < 15; x++ {
		_, color := cell(x)
		if x == 7 || x == 8 {
			assert.Equal(t, keyHover, color, "the key at %d", x)
		} else {
			assert.Equal(t, background, color, "at %d", x)
		}
	}
}
