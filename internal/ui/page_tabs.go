package ui

import (
	"fmt"
	"strings"
	"zfs-file-history/internal/ui/clickable"
	"zfs-file-history/internal/ui/theme"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// pageTabs shows the switchable pages in the header by their number, the shown one with its title, e.g.
// " 1 FILES     2  3  4 ": short enough for the header, and the numbers are the keys that show the pages. The numbers
// of the other pages can be clicked. The title is padded to the longest one, so the tabs have the same width on all
// pages and the numbers behind the shown one stay where they are when it is clicked.
type pageTabs struct {
	*tview.Box
	// titles are the titles of the pages, in the order of their numbers
	titles []string
	// current is the index of the shown page
	current int
	// onSelect is called on the UI thread with the index of a clicked page
	onSelect func(index int)
	// enabled returns whether the tabs react to the mouse, e.g. not while a dialog is in front (rivo/tview#926)
	enabled func() bool

	// regions are the numbers of the other pages, added by Draw. Only accessed on the UI thread.
	regions clickable.Regions
}

// newPageTabs creates the tabs of the pages with the titles, current is the index of the shown one.
func newPageTabs(titles []string, current int, onSelect func(index int)) *pageTabs {
	return &pageTabs{
		Box:      tview.NewBox(),
		titles:   titles,
		current:  current,
		onSelect: onSelect,
		enabled:  func() bool { return true },
	}
}

// pageTab is the text of a tab and whether it is the shown page.
type pageTab struct {
	text    string
	current bool
}

// tabs returns the tabs in their order.
func (tabs *pageTabs) tabs() []pageTab {
	titleWidth := 0
	for _, title := range tabs.titles {
		titleWidth = max(titleWidth, len(title))
	}
	result := make([]pageTab, len(tabs.titles))
	for index, title := range tabs.titles {
		if index == tabs.current {
			result[index] = pageTab{text: fmt.Sprintf(" %d %-*s ", index+1, titleWidth, strings.ToUpper(title)), current: true}
		} else {
			result[index] = pageTab{text: fmt.Sprintf(" %d ", index+1)}
		}
	}
	return result
}

// width returns the width of all tabs.
func (tabs *pageTabs) width() int {
	width := 0
	for _, tab := range tabs.tabs() {
		width += len(tab.text)
	}
	return width
}

func (tabs *pageTabs) Draw(screen tcell.Screen) {
	tabs.DrawForSubclass(screen, tabs)
	tabs.regions.Reset()
	x, y, width, height := tabs.GetInnerRect()
	if height <= 0 {
		return
	}
	colors := theme.Colors.Header
	column := x
	for index, tab := range tabs.tabs() {
		style := tcell.StyleDefault.Foreground(colors.PageNumber).Background(colors.PageNumberBackground)
		if tab.current {
			style = tcell.StyleDefault.Foreground(colors.PageIndicator).Background(colors.PageIndicatorBackground).Bold(true)
		}
		start := column
		for _, character := range tab.text {
			if column >= x+width {
				break
			}
			screen.SetContent(column, y, character, nil, style)
			column++
		}
		if !tab.current {
			tabs.regions.Add(clickable.Region{X: start, Y: y, Width: column - start, Key: index,
				HoverBackground: colors.PageNumberHoverBackground, OnClick: func() { tabs.onSelect(index) }})
		}
	}
	tabs.regions.DrawHover(screen)
}

// MouseHandler highlights the number below the mouse and shows its page when clicked. The tabs never take the
// focus.
func (tabs *pageTabs) MouseHandler() func(action tview.MouseAction, event *tcell.EventMouse, setFocus func(p tview.Primitive)) (consumed bool, capture tview.Primitive) {
	return tabs.WrapMouseHandler(func(action tview.MouseAction, event *tcell.EventMouse, setFocus func(p tview.Primitive)) (consumed bool, capture tview.Primitive) {
		consumed, captures := tabs.regions.HandleMouse(action, event, tabs.enabled() && tabs.InRect(event.Position()))
		if captures {
			return consumed, tabs
		}
		return consumed, nil
	})
}
