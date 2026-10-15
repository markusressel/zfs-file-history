package util

import (
	"testing"
	"zfs-file-history/internal/testutil"
	"zfs-file-history/internal/ui/theme"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// selectionTest is a screen of 40x6 with two panes side by side (with borders), the selection and what it copied.
type selectionTest struct {
	t         *testing.T
	screen    tcell.SimulationScreen
	root      *tview.Pages
	selection *TextSelection
	copied    []string
}

// newSelectionTest shows the left text in the left pane (columns 1 to 18 inside its border, rows 1 to 4), "right"
// in the right one.
func newSelectionTest(t *testing.T, left string) *selectionTest {
	leftView := tview.NewTextView().SetText(left)
	leftView.SetBorder(true)
	rightView := tview.NewTextView().SetText("right")
	rightView.SetBorder(true)
	root := tview.NewPages().AddPage("page", tview.NewFlex().AddItem(leftView, 20, 0, false).AddItem(rightView, 0, 1, false), true, true)

	test := &selectionTest{t: t, root: root}
	test.screen = tcell.NewSimulationScreen("UTF-8")
	require.NoError(t, test.screen.Init())
	test.screen.SetSize(40, 6)
	test.selection = InstallTextSelection(testutil.App(), root, func(text string) {
		test.copied = append(test.copied, text)
	})
	test.draw()
	return test
}

// draw draws the panes and the selection, like the application does.
func (test *selectionTest) draw() {
	test.root.SetRect(0, 0, 40, 6)
	test.root.Draw(test.screen)
	test.selection.highlight(test.screen)
}

// mouse sends the action at the position to the selection, returns whether it was passed on.
func (test *selectionTest) mouse(action tview.MouseAction, x int, y int) bool {
	buttons := tcell.ButtonNone
	if action == tview.MouseLeftDown || action == tview.MouseMove {
		buttons = tcell.ButtonPrimary
	}
	event, _ := test.selection.capture(tcell.NewEventMouse(x, y, buttons, tcell.ModNone), action)
	test.draw()
	return event != nil
}

// drag drags from one position to the other, returns whether the move was passed on.
func (test *selectionTest) drag(fromX int, fromY int, toX int, toY int) bool {
	test.mouse(tview.MouseLeftDown, fromX, fromY)
	passed := test.mouse(tview.MouseMove, toX, toY)
	test.mouse(tview.MouseLeftUp, toX, toY)
	return passed
}

func (test *selectionTest) isHighlighted(x int, y int) bool {
	_, style, _ := test.screen.Get(x, y)
	_, background, _ := style.Decompose()
	return background == theme.Colors.Layout.SelectionBackground
}

func TestTextSelection_DragCopiesTheTextOfThePane(t *testing.T) {
	test := newSelectionTest(t, "first line\nsecond line\nthird")

	assert.False(t, test.drag(7, 1, 36, 3), "the drag is not passed on")
	require.Len(t, test.copied, 1)
	assert.Equal(t, "line\nsecond line\nthird", test.copied[0], "like a terminal, but only within the pane")
	assert.True(t, test.isHighlighted(7, 1))
	assert.True(t, test.isHighlighted(18, 2), "the lines in between whole")
	assert.False(t, test.isHighlighted(6, 1), "before the start")
	assert.False(t, test.isHighlighted(19, 2), "the border")
	assert.False(t, test.isHighlighted(21, 1), "the pane next to it")

	// the clipboard is written with OSC 52
	test.screen.GetClipboard()
	clipboard, ok := test.screen.PollEvent().(*tcell.EventClipboard)
	require.True(t, ok)
	assert.Equal(t, "line\nsecond line\nthird", string(clipboard.Data()))
}

func TestTextSelection_DragBackwards(t *testing.T) {
	test := newSelectionTest(t, "first line\nsecond line")
	test.drag(4, 2, 2, 1)
	assert.Equal(t, []string{"irst line\nseco"}, test.copied)
}

func TestTextSelection_HighlightEndsWithTheNextClickOrKey(t *testing.T) {
	test := newSelectionTest(t, "first line")
	test.drag(1, 1, 5, 1)
	require.True(t, test.isHighlighted(1, 1))

	assert.NotNil(t, test.selection.application.GetInputCapture()(tcell.NewEventKey(tcell.KeyRune, 'x', tcell.ModNone)), "the key is passed on")
	test.draw()
	assert.False(t, test.isHighlighted(1, 1), "a key ends it")

	test.drag(1, 1, 5, 1)
	assert.True(t, test.mouse(tview.MouseLeftDown, 30, 1), "a click is passed on")
	assert.False(t, test.isHighlighted(1, 1), "a click ends it")
}

func TestTextSelection_ClicksArePassedOn(t *testing.T) {
	test := newSelectionTest(t, "first line")
	assert.True(t, test.mouse(tview.MouseLeftDown, 3, 1))
	assert.True(t, test.mouse(tview.MouseMove, 3, 1), "not moved to another cell")
	assert.True(t, test.mouse(tview.MouseLeftUp, 3, 1))
	assert.True(t, test.mouse(tview.MouseLeftClick, 3, 1))
	assert.Empty(t, test.copied)
	assert.False(t, test.isHighlighted(3, 1))
}

func TestTextSelection_DragOnABorderIsPassedOn(t *testing.T) {
	// e.g. the boundary of a split
	test := newSelectionTest(t, "first line")
	assert.True(t, test.drag(19, 2, 25, 2))
	assert.Empty(t, test.copied)
}

func TestTextSelection_SecondClickSelectsTheWord(t *testing.T) {
	test := newSelectionTest(t, "file /tank/a.txt  x")
	for range 2 {
		test.mouse(tview.MouseLeftDown, 9, 1)
		test.mouse(tview.MouseLeftUp, 9, 1)
	}
	assert.True(t, test.mouse(tview.MouseLeftClick, 9, 1), "the first click is passed on")
	assert.False(t, test.mouse(tview.MouseLeftClick, 9, 1), "the second one selects the word")
	assert.Equal(t, []string{"/tank/a.txt"}, test.copied)
	assert.True(t, test.isHighlighted(6, 1))
	assert.False(t, test.isHighlighted(5, 1))

	assert.True(t, test.mouse(tview.MouseLeftClick, 9, 1), "a third click is a first one again")
	assert.True(t, test.mouse(tview.MouseLeftClick, 18, 1), "on another cell")
	assert.Len(t, test.copied, 1)
}

func TestTextSelection_WideCharacters(t *testing.T) {
	test := newSelectionTest(t, "名前 x")
	test.drag(1, 1, 6, 1)
	assert.Equal(t, []string{"名前 x"}, test.copied)

	test.mouse(tview.MouseLeftClick, 2, 1)
	test.mouse(tview.MouseLeftClick, 2, 1)
	assert.Equal(t, "名前", test.copied[1], "a click on the second cell of a wide character")
}

func TestTextSelection_OnlyTheFrontPage(t *testing.T) {
	test := newSelectionTest(t, "first line")
	// a dialog in front: a box in the middle of empty space
	dialog := tview.NewTextView().SetText("dialog text")
	dialog.SetBorder(true)
	test.root.AddPage("dialog", tview.NewFlex().AddItem(nil, 0, 1, false).AddItem(dialog, 15, 0, false).AddItem(nil, 0, 1, false), true, true)
	test.draw()

	assert.True(t, test.drag(1, 1, 5, 1), "next to the dialog, passed on to it")
	assert.Empty(t, test.copied, "not the page behind it")

	test.drag(13, 1, 18, 1)
	assert.Equal(t, []string{"dialog"}, test.copied)
}
