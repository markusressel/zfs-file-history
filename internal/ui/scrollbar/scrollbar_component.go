package scrollbar

import (
	"math"
	"zfs-file-history/internal/ui/theme"
	uiutil "zfs-file-history/internal/ui/util"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

type ScrollBarOrientation int

const (
	ScrollBarVertical ScrollBarOrientation = iota
	ScrollBarHorizontal

	ScrollIndicatorAtLimit = "•"
	ScrollIndicatorTop     = "▴"
	ScrollIndicatorBottom  = "▾"
	ScrollIndicatorLeft    = "◂"
	ScrollIndicatorRight   = "▸"
)

type ScrollbarRuneType int

const (
	ScrollbarRuneTypeTop ScrollbarRuneType = iota
	ScrollbarRuneTypeBottom
	ScrollbarRuneTypeLeft
	ScrollbarRuneTypeRight
)

type ScrollbarComponent struct {
	application *tview.Application

	layout       *tview.Flex
	topArrow     *tview.TextView
	scrollbarBox *tview.Box
	bottomArrow  *tview.TextView

	inputCapture func(event *tcell.EventKey) *tcell.EventKey

	orientation    ScrollBarOrientation
	scrollPosition int
	barWidth       int
	min            int
	max            int
}

// NewScrollbarComponent creates a new ScrollbarComponent.
// The application is used to redraw the component.
// The orientation is used to set the orientation of the scrollbar.
// The min is the minimum value of the scrollbar.
// The max is the maximum value of the scrollbar.
// The scrollPosition is the current position of the scrollbar.
// The barWidth is the width of the scrollbar.
func NewScrollbarComponent(
	application *tview.Application,
	orientation ScrollBarOrientation,
	min int,
	max int,
	scrollPosition int,
	barWidth int,
) *ScrollbarComponent {
	scrollbarComponent := &ScrollbarComponent{
		application: application,
		inputCapture: func(event *tcell.EventKey) *tcell.EventKey {
			return event
		},
		min:            min,
		max:            max,
		scrollPosition: scrollPosition,
		barWidth:       barWidth,
	}
	scrollbarComponent.createLayout()
	scrollbarComponent.SetOrientation(orientation)
	return scrollbarComponent
}

func (c *ScrollbarComponent) createLayout() {
	layout := tview.NewFlex()

	c.topArrow = tview.NewTextView().SetTextAlign(tview.AlignCenter)
	layout.AddItem(c.topArrow, 1, 0, false)

	c.scrollbarBox = tview.NewBox()
	c.scrollbarBox.SetDrawFunc(c.DrawFunc)
	layout.AddItem(c.scrollbarBox, 0, 1, false)

	c.bottomArrow = tview.NewTextView().SetTextAlign(tview.AlignCenter)
	layout.AddItem(c.bottomArrow, 1, 0, false)

	c.layout = layout
}

func (c *ScrollbarComponent) DrawFunc(screen tcell.Screen, x int, y int, width int, height int) (int, int, int, int) {
	if height <= 0 || width <= 0 {
		return x, y, width, height
	}

	totalUnits := height * 2
	if c.orientation == ScrollBarHorizontal {
		totalUnits = width * 2
	}

	scale := float64(totalUnits) / math.Max(1, float64(c.max-c.min))
	barStart := float64(c.scrollPosition) * scale
	barEnd := barStart + float64(c.barWidth)*scale

	for i := 0; i < height; i++ {
		if c.orientation == ScrollBarHorizontal {
			// Horizontal logic will be added below for ix
			continue
		}

		// Vertical logic
		topUnit := float64(i * 2)
		bottomUnit := float64(i*2 + 1)

		topIsBar := topUnit >= barStart && topUnit < barEnd
		bottomIsBar := bottomUnit >= barStart && bottomUnit < barEnd

		var char rune
		var fg tcell.Color
		if topIsBar && bottomIsBar {
			char = '┃' // Full bar
			fg = theme.Colors.List.Scrollbar.Bar
		} else if !topIsBar && !bottomIsBar {
			char = '│' // Full track
			fg = theme.Colors.List.Scrollbar.Background
		} else if !topIsBar && bottomIsBar {
			char = '╽' // Transition entering
			fg = theme.Colors.List.Scrollbar.Bar
		} else {
			char = '╿' // Transition exiting
			fg = theme.Colors.List.Scrollbar.Bar
		}

		style := tcell.StyleDefault.Foreground(fg)
		screen.SetContent(x, y+i, char, nil, style)
	}

	if c.orientation == ScrollBarHorizontal {
		for i := 0; i < width; i++ {
			leftUnit := float64(i * 2)
			rightUnit := float64(i*2 + 1)

			leftIsBar := leftUnit >= barStart && leftUnit < barEnd
			rightIsBar := rightUnit >= barStart && rightUnit < barEnd

			var char rune
			var fg tcell.Color
			if leftIsBar && rightIsBar {
				char = '━'
				fg = theme.Colors.List.Scrollbar.Bar
			} else if !leftIsBar && !rightIsBar {
				char = '─'
				fg = theme.Colors.List.Scrollbar.Background
			} else if !leftIsBar && rightIsBar {
				char = '╾'
				fg = theme.Colors.List.Scrollbar.Bar
			} else {
				char = '╼'
				fg = theme.Colors.List.Scrollbar.Bar
			}

			style := tcell.StyleDefault.Foreground(fg)
			screen.SetContent(x+i, y, char, nil, style)
		}
	}

	return x, y, width, height
}

func (c *ScrollbarComponent) UpdateLayout() {
	c.updateTopEndText()
	c.updateScrollbar()
	c.updateBottomEndText()

	c.application.ForceDraw()
}

func (c *ScrollbarComponent) SetOrientation(orientation ScrollBarOrientation) {
	c.orientation = orientation
	switch c.orientation {
	case ScrollBarVertical:
		c.layout.SetDirection(tview.FlexRow)
	case ScrollBarHorizontal:
		c.layout.SetDirection(tview.FlexColumn)
	}
	c.UpdateLayout()
}

func (c *ScrollbarComponent) GetLayout() *tview.Flex {
	return c.layout
}

func (c *ScrollbarComponent) SetTitle(title string) {
	uiutil.SetupWindow(c.layout, title)
}

func (c *ScrollbarComponent) GetMin() int {
	return c.min
}

func (c *ScrollbarComponent) GetMax() int {
	return c.max
}

func (c *ScrollbarComponent) GetPosition() int {
	return c.scrollPosition
}

func (c *ScrollbarComponent) SetMin(min int) {
	c.min = min
	c.UpdateLayout()
}

func (c *ScrollbarComponent) SetMax(max int) {
	c.max = max
	c.UpdateLayout()
}

func (c *ScrollbarComponent) SetPosition(position int) {
	if position < 0 {
		position = 0
	}
	c.scrollPosition = position
	c.UpdateLayout()
}

func (c *ScrollbarComponent) HasFocus() bool {
	return c.layout.HasFocus()
}

func (c *ScrollbarComponent) SetInputCapture(inputCapture func(event *tcell.EventKey) *tcell.EventKey) {
	c.inputCapture = inputCapture
}

func (c *ScrollbarComponent) scrollUp() {
	c.scroll(-1)
}

func (c *ScrollbarComponent) scrollDown() {
	c.scroll(+1)
}

// scroll moves the scrollbar to the specified position
func (c *ScrollbarComponent) scroll(amount int) {
	oldPosition := c.GetPosition()
	newPosition := oldPosition + amount
	if newPosition < c.GetMin() {
		newPosition = c.GetMin()
	}
	if newPosition > c.GetMax() {
		newPosition = c.GetMax()
	}
	c.SetPosition(newPosition)

	newBarWidth := c.calculateBarWidth()
	c.barWidth = newBarWidth

	c.UpdateLayout()
}

func (c *ScrollbarComponent) ScrollToTop() {
	c.scroll(-c.GetPosition())
}

func (c *ScrollbarComponent) calculateBarWidth() int {
	// calculate the bar width
	if c.max <= c.min {
		return 1
	}
	barWidth := int(math.Max(1, float64(c.max/(c.max-c.min))))
	return barWidth
}

func (c *ScrollbarComponent) updateTopEndText() {
	c.layout.ResizeItem(c.topArrow, 1, 0)
	isAtLimit := c.scrollPosition <= c.min
	text, textColor := c.determineRuneAndColor(ScrollbarRuneTypeTop, isAtLimit)
	c.topArrow.SetText(text)
	c.topArrow.SetTextColor(textColor)
}

func (c *ScrollbarComponent) updateBottomEndText() {
	c.layout.ResizeItem(c.bottomArrow, 1, 0)
	isAtLimit := c.scrollPosition+c.barWidth >= c.max
	text, textColor := c.determineRuneAndColor(ScrollbarRuneTypeBottom, isAtLimit)
	c.bottomArrow.SetText(text)
	c.bottomArrow.SetTextColor(textColor)
}

func (c *ScrollbarComponent) determineRuneAndColor(
	scrollbarRuneType ScrollbarRuneType,
	isAtLimit bool,
) (text string, textColor tcell.Color) {
	switch c.orientation {
	case ScrollBarVertical:
		if isAtLimit {
			text = ScrollIndicatorAtLimit
			textColor = theme.Colors.List.Scrollbar.IndicatorInactive
		} else {
			switch scrollbarRuneType {
			case ScrollbarRuneTypeBottom:
				text = ScrollIndicatorBottom
			case ScrollbarRuneTypeTop:
				fallthrough
			default:
				text = ScrollIndicatorTop
			}
			textColor = theme.Colors.List.Scrollbar.IndicatorActive
		}
	case ScrollBarHorizontal:
		if isAtLimit {
			text = ScrollIndicatorAtLimit
			textColor = theme.Colors.List.Scrollbar.IndicatorInactive
		} else {
			switch scrollbarRuneType {
			case ScrollbarRuneTypeRight:
				text = ScrollIndicatorRight
			case ScrollbarRuneTypeLeft:
				fallthrough
			default:
				text = ScrollIndicatorLeft
			}
			textColor = theme.Colors.List.Scrollbar.IndicatorActive
		}
	}
	return text, textColor
}

func (c *ScrollbarComponent) updateScrollbar() {
	c.application.ForceDraw()
}

func (c *ScrollbarComponent) SetWidth(width int) {
	c.barWidth = width
	c.UpdateLayout()
}
