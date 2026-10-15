package util

import (
	"strings"
	"testing"
	"zfs-file-history/internal/testutil"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"github.com/stretchr/testify/assert"
)

// newFooterTestWindow creates a bordered and padded window with a child, like the table windows.
func newFooterTestWindow() (*tview.Flex, *tview.Box) {
	child := tview.NewBox()
	window := tview.NewFlex().AddItem(child, 0, 1, false)
	window.SetBorder(true)
	window.SetBorderPadding(0, 0, 1, 1)
	return window, child
}

// bottomBorder returns the expected bottom border line of the given width, with the text right-aligned in it.
func bottomBorder(width int, text string) string {
	return "└" + strings.Repeat("─", width-2-len([]rune(text))) + text + "┘"
}

func TestBorderFooter_DrawsRightAlignedIntoBottomBorder(t *testing.T) {
	window, _ := newFooterTestWindow()
	window.SetTitle(" Title ")
	footer := NewBorderFooter(window.Box)
	footer.SetText("16 of 589")

	screen := testutil.DrawToScreen(t, window, 30, 5)

	bottom := testutil.ScreenLines(screen)[4]
	// like the title, which starts right after the top left corner, the footer ends right before the bottom right corner
	assert.Equal(t, bottomBorder(30, " 16 of 589 "), bottom)
	// the title in the top border is not affected
	assert.Contains(t, testutil.ScreenLines(screen)[0], " Title ")
	assert.Equal(t, "16 of 589", footer.GetText())
}

func TestBorderFooter_KeepsTheDefaultInnerRect(t *testing.T) {
	withoutFooter, childWithoutFooter := newFooterTestWindow()
	testutil.DrawToScreen(t, withoutFooter, 30, 6)

	withFooter, childWithFooter := newFooterTestWindow()
	NewBorderFooter(withFooter.Box).SetText("footer")
	testutil.DrawToScreen(t, withFooter, 30, 6)

	// border and padding are respected, exactly like without a footer
	x, y, width, height := childWithFooter.GetRect()
	assert.Equal(t, [4]int{2, 1, 26, 4}, [4]int{x, y, width, height})
	ex, ey, ewidth, eheight := childWithoutFooter.GetRect()
	assert.Equal(t, [4]int{ex, ey, ewidth, eheight}, [4]int{x, y, width, height})

	// also after a resize
	testutil.DrawToScreen(t, withFooter, 20, 10)
	x, y, width, height = childWithFooter.GetRect()
	assert.Equal(t, [4]int{2, 1, 16, 8}, [4]int{x, y, width, height})
}

func TestBorderFooter_KeepsAnExistingDrawFunc(t *testing.T) {
	window, child := newFooterTestWindow()
	called := false
	window.SetDrawFunc(func(screen tcell.Screen, x, y, width, height int) (int, int, int, int) {
		called = true
		// a custom inner rect without padding
		return x + 1, y + 1, width - 2, height - 2
	})
	NewBorderFooter(window.Box).SetText("footer")

	screen := testutil.DrawToScreen(t, window, 30, 5)

	assert.True(t, called)
	x, _, width, _ := child.GetRect()
	assert.Equal(t, 1, x)
	assert.Equal(t, 28, width)
	assert.Contains(t, testutil.ScreenLines(screen)[4], " footer ")
}

func TestBorderFooter_TruncatesLongText(t *testing.T) {
	window, _ := newFooterTestWindow()
	NewBorderFooter(window.Box).SetText("this text is way too long for the window")

	screen := testutil.DrawToScreen(t, window, 20, 4)

	bottom := testutil.ScreenLines(screen)[3]
	// the beginning is kept, the corners stay intact
	assert.Equal(t, "└ this text is way…┘", bottom)
}

func TestBorderFooter_StyleTagsAndEmptyText(t *testing.T) {
	window, _ := newFooterTestWindow()
	footer := NewBorderFooter(window.Box)

	footer.SetText("")
	screen := testutil.DrawToScreen(t, window, 20, 4)
	assert.Equal(t, bottomBorder(20, ""), testutil.ScreenLines(screen)[3])

	// style tags are interpreted, not printed
	footer.SetText("[#ff0000]red[-] text")
	screen = testutil.DrawToScreen(t, window, 20, 4)
	bottom := testutil.ScreenLines(screen)[3]
	assert.Equal(t, bottomBorder(20, " red text "), bottom)
	x := strings.Index(bottom, "red")
	x = len([]rune(bottom[:x]))
	_, style, _ := screen.Get(x, 3)
	foreground, _, _ := style.Decompose()
	assert.Equal(t, tcell.NewHexColor(0xff0000), foreground)
}

func TestBorderFooter_LeftAndRight(t *testing.T) {
	window, _ := newFooterTestWindow()
	footer := NewBorderFooter(window.Box)
	footer.SetText("2 of 4")
	footer.SetLeftText("Filter: x")

	screen := testutil.DrawToScreen(t, window, 30, 4)
	assert.Equal(t, "└ Filter: x "+strings.Repeat("─", 30-2-11-8)+" 2 of 4 ┘", testutil.ScreenLines(screen)[3])
}

func TestBorderFooter_LeftGetsTheRemainingWidth(t *testing.T) {
	window, _ := newFooterTestWindow()
	footer := NewBorderFooter(window.Box)
	footer.SetText("2 of 4")

	var requestedWidths []int
	footer.SetLeftFunc(func(maxWidth int) string {
		requestedWidths = append(requestedWidths, maxWidth)
		return strings.Repeat("x", 40)
	})

	screen := testutil.DrawToScreen(t, window, 30, 4)

	// the right part keeps its width, the left part is cut off before it, with a border character in between:
	// 28 between the corners - 8 for " 2 of 4 " - 1 border character = 19 for " xxx…"
	assert.Equal(t, "└ "+strings.Repeat("x", 17)+"…─ 2 of 4 ┘", testutil.ScreenLines(screen)[3])
	// the func is asked for the actual width it gets (without padding)
	assert.Equal(t, 30-2-8-1-2, requestedWidths[len(requestedWidths)-1])
}

func TestBorderFooter_RightIsHiddenIfTooNarrow(t *testing.T) {
	window, _ := newFooterTestWindow()
	footer := NewBorderFooter(window.Box)
	footer.SetText("100 of 170 entries")
	footer.SetLeftText("Filter: *.txt")

	// 26 between the corners: the counts (20) would leave less than the minimum for the filter
	screen := testutil.DrawToScreen(t, window, 28, 4)
	assert.Equal(t, "└ Filter: *.txt ───────────┘", testutil.ScreenLines(screen)[3])
}

func TestBorderFooter_EmptyLeftTextKeepsTheRightOnlyLayout(t *testing.T) {
	window, _ := newFooterTestWindow()
	footer := NewBorderFooter(window.Box)
	footer.SetText("2 of 4")
	footer.SetLeftText("Filter: x")
	footer.SetLeftText("")

	screen := testutil.DrawToScreen(t, window, 20, 4)
	assert.Equal(t, bottomBorder(20, " 2 of 4 "), testutil.ScreenLines(screen)[3])

	footer.SetLeftFunc(func(int) string { return "" })
	screen = testutil.DrawToScreen(t, window, 20, 4)
	assert.Equal(t, bottomBorder(20, " 2 of 4 "), testutil.ScreenLines(screen)[3])
}
