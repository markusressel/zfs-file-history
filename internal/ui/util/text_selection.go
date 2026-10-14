package util

import (
	"strings"
	"time"
	"unicode"
	"zfs-file-history/internal/ui/theme"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// wordClickInterval is how soon a second click on the same cell selects the word below it. tview's own double clicks
// are disabled (tview.DoubleClickInterval), they would delay every click.
const wordClickInterval = 400 * time.Millisecond

// TextSelection lets the text of every pane and dialog be selected with the mouse and copied to the clipboard: the
// application captures the mouse, so the terminal cannot select. A drag selects like a terminal (from the start to
// the end, the lines in between whole), but only within the pane it started in, so the borders and panes next to it
// are left out; a second click on the same cell selects the word below it. The selection is copied when the mouse
// is released, with the escape sequence OSC 52, which the terminal handles, so it also works over SSH. It copies the
// text as drawn, so text cut off by a column stays cut off.
//
// It stays highlighted until the next click or key. A drag that starts on a border is left to the page (e.g. the
// boundary of a split).
type TextSelection struct {
	application *tview.Application
	root        tview.Primitive
	// onCopied is called with the copied text, e.g. to say so
	onCopied func(text string)

	// All fields below are only accessed on the UI thread.

	// screen is the screen of the last draw, whose cells are copied
	screen tcell.Screen
	// pending is set while the left button is down in a pane and the mouse did not move yet: a click, unless it moves
	pending bool
	// dragging is set while the mouse is dragged with the left button
	dragging bool
	// shown is set while the selection is highlighted: while dragging and after it was copied
	shown bool
	// area is the pane the selection is in (its inner rect), the selection runs from start to end within it
	area           selectionArea
	startX, startY int
	endX, endY     int
	// lastClickAt is the time of the last click (zero after a word was selected), lastClickX and lastClickY its cell
	lastClickAt            time.Time
	lastClickX, lastClickY int
}

// selectionArea is the inner rect of a pane.
type selectionArea struct {
	x, y, width, height int
}

func (area selectionArea) contains(x int, y int) bool {
	return x >= area.x && x < area.x+area.width && y >= area.y && y < area.y+area.height
}

// clamp returns the closest position within the area.
func (area selectionArea) clamp(x int, y int) (int, int) {
	return min(max(x, area.x), area.x+area.width-1), min(max(y, area.y), area.y+area.height-1)
}

// InstallTextSelection lets the text of the panes below root be selected and copied, see TextSelection. It takes
// the mouse capture, the after draw function and wraps the input capture of the application. onCopied is called
// with the copied text. Must be called on the UI thread, after root was set.
func InstallTextSelection(application *tview.Application, root tview.Primitive, onCopied func(text string)) *TextSelection {
	selection := &TextSelection{application: application, root: root, onCopied: onCopied}
	application.SetMouseCapture(selection.capture)
	application.SetAfterDrawFunc(selection.highlight)
	inputCapture := application.GetInputCapture()
	application.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		// like a terminal: typing ends the selection
		selection.shown = false
		if inputCapture != nil {
			return inputCapture(event)
		}
		return event
	})
	return selection
}

// capture handles the mouse before the primitives do. Clicks are passed on, a drag is not. Runs on the UI thread.
func (selection *TextSelection) capture(event *tcell.EventMouse, action tview.MouseAction) (*tcell.EventMouse, tview.MouseAction) {
	if event == nil {
		return event, action
	}
	x, y := event.Position()
	switch action {
	case tview.MouseLeftDown:
		// passed on: a click selects as usual, the selection starts once the mouse moves
		selection.shown, selection.dragging = false, false
		selection.area, selection.pending = selection.areaAt(x, y)
		selection.startX, selection.startY = x, y
		selection.endX, selection.endY = x, y
	case tview.MouseMove:
		if !selection.pending && !selection.dragging {
			return event, action
		}
		if event.Buttons()&tcell.ButtonPrimary == 0 {
			// released outside of the terminal
			selection.pending, selection.dragging = false, false
			return event, action
		}
		if selection.pending && x == selection.startX && y == selection.startY {
			return event, action
		}
		selection.pending, selection.dragging, selection.shown = false, true, true
		selection.endX, selection.endY = selection.area.clamp(x, y)
		// consumed, so the page does not take it for its own drag, and the highlight is drawn
		return nil, action
	case tview.MouseLeftUp:
		selection.pending = false
		if selection.dragging {
			selection.dragging = false
			selection.endX, selection.endY = selection.area.clamp(x, y)
			selection.copy()
			// not a click either
			return nil, action
		}
	case tview.MouseLeftClick:
		if selection.isSecondClick(x, y) {
			selection.lastClickAt = time.Time{}
			if selection.selectWord(x, y) {
				// the first click did what a click does
				return nil, action
			}
			return event, action
		}
		selection.lastClickAt = time.Now()
		selection.lastClickX, selection.lastClickY = x, y
	}
	return event, action
}

// isSecondClick returns whether a click on the cell follows another one on it within wordClickInterval.
func (selection *TextSelection) isSecondClick(x int, y int) bool {
	return !selection.lastClickAt.IsZero() && time.Since(selection.lastClickAt) <= wordClickInterval &&
		x == selection.lastClickX && y == selection.lastClickY
}

// selectWord selects and copies the word at the position: the characters up to the next space (or the edge of the
// pane) on both sides, so a path or a name is selected whole. Returns false if there is none.
func (selection *TextSelection) selectWord(x int, y int) bool {
	area, ok := selection.areaAt(x, y)
	if !ok || selection.screen == nil || isBlank(selection.cell(x, y)) {
		return false
	}
	start, end := x, x
	for start > area.x && !isBlank(selection.cell(start-1, y)) {
		start--
	}
	for end < area.x+area.width-1 && !isBlank(selection.cell(end+1, y)) {
		end++
	}
	selection.area = area
	selection.startX, selection.startY, selection.endX, selection.endY = start, y, end, y
	selection.shown = true
	selection.copy()
	return true
}

// isBlank returns whether the text of a cell is white space.
func isBlank(text string) bool {
	return strings.TrimFunc(text, unicode.IsSpace) == ""
}

// isContinuation returns whether the cell is the second one of a wide character (e.g. 名), which tcell returns as a
// space.
func (selection *TextSelection) isContinuation(x int, y int) bool {
	if x <= 0 {
		return false
	}
	_, _, width := selection.screen.Get(x-1, y)
	return width == 2
}

// cell returns the text of the cell, the one of the wide character for the second cell it covers.
func (selection *TextSelection) cell(x int, y int) string {
	if selection.isContinuation(x, y) {
		x--
	}
	text, _, _ := selection.screen.Get(x, y)
	return text
}

// cells calls visit for each cell of the selection, line by line.
func (selection *TextSelection) cells(visit func(x int, y int)) {
	startX, startY, endX, endY := selection.startX, selection.startY, selection.endX, selection.endY
	if endY < startY || endY == startY && endX < startX {
		startX, startY, endX, endY = endX, endY, startX, startY
	}
	area := selection.area
	for y := startY; y <= endY; y++ {
		from, to := area.x, area.x+area.width-1
		if y == startY {
			from = startX
		}
		if y == endY {
			to = endX
		}
		for x := from; x <= to; x++ {
			visit(x, y)
		}
	}
}

// Text returns the selected text: its lines without the spaces at their end. Must be called on the UI thread.
func (selection *TextSelection) Text() string {
	if !selection.shown || selection.screen == nil {
		return ""
	}
	var lines []string
	var line strings.Builder
	lineY := -1
	selection.cells(func(x int, y int) {
		started := y != lineY
		if started {
			if lineY >= 0 {
				lines = append(lines, strings.TrimRight(line.String(), " "))
				line.Reset()
			}
			lineY = y
		}
		// a wide character once, also if the selection starts on its second cell
		if !selection.isContinuation(x, y) || started {
			line.WriteString(selection.cell(x, y))
		}
	})
	lines = append(lines, strings.TrimRight(line.String(), " "))
	return strings.TrimRight(strings.Join(lines, "\n"), "\n")
}

// copy copies the selected text to the clipboard of the terminal. An empty one is not copied, nor shown.
func (selection *TextSelection) copy() {
	text := selection.Text()
	if text == "" {
		selection.shown = false
		return
	}
	selection.screen.SetClipboard([]byte(text))
	if selection.onCopied != nil {
		selection.onCopied(text)
	}
}

// highlight draws the selection over the panes. Runs as the after draw function, also to keep the screen.
func (selection *TextSelection) highlight(screen tcell.Screen) {
	selection.screen = screen
	if !selection.shown {
		return
	}
	colors := theme.Colors.Layout
	selection.cells(func(x int, y int) {
		if selection.isContinuation(x, y) {
			// highlighted with the first one; putting it would replace the character
			return
		}
		text, style, _ := screen.Get(x, y)
		screen.Put(x, y, text, style.Foreground(colors.SelectionForeground).Background(colors.SelectionBackground))
	})
}

// areaAt returns the inner rect of the pane at the position, false if there is none or the position is on its
// border: the deepest primitive below the visible pages and flexes at the position.
func (selection *TextSelection) areaAt(x int, y int) (selectionArea, bool) {
	primitive := primitiveAt(selection.root, x, y)
	if primitive == nil {
		return selectionArea{}, false
	}
	var area selectionArea
	if box, ok := primitive.(interface {
		GetInnerRect() (int, int, int, int)
	}); ok {
		area.x, area.y, area.width, area.height = box.GetInnerRect()
	} else {
		area.x, area.y, area.width, area.height = primitive.GetRect()
	}
	return area, area.contains(x, y)
}

// primitiveAt returns the deepest primitive at the position below primitive, nil if it is in the empty space of a
// container (e.g. around a dialog, or its border). Containers are found by their methods, so components that embed a
// Flex or Pages (e.g. ResizableSplit) are searched as well. Of the pages, only the front-most one at the position.
func primitiveAt(primitive tview.Primitive, x int, y int) tview.Primitive {
	contains := func(primitive tview.Primitive) bool {
		rectX, rectY, width, height := primitive.GetRect()
		return selectionArea{rectX, rectY, width, height}.contains(x, y)
	}
	switch container := primitive.(type) {
	case interface {
		GetPageNames(visibleOnly bool) []string
		GetPage(name string) tview.Primitive
	}:
		// the front-most first
		for _, name := range container.GetPageNames(true) {
			if page := container.GetPage(name); page != nil && contains(page) {
				return primitiveAt(page, x, y)
			}
		}
		return nil
	case interface {
		GetItemCount() int
		GetItem(index int) tview.Primitive
	}:
		for i := 0; i < container.GetItemCount(); i++ {
			if item := container.GetItem(i); item != nil && contains(item) {
				return primitiveAt(item, x, y)
			}
		}
		return nil
	}
	return primitive
}
