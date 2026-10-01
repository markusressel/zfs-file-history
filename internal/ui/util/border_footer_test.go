package util

import (
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newFooterTestWindow creates a bordered and padded window with a child, like the table windows.
func newFooterTestWindow() (*tview.Flex, *tview.Box) {
	child := tview.NewBox()
	window := tview.NewFlex().AddItem(child, 0, 1, false)
	window.SetBorder(true)
	window.SetBorderPadding(0, 0, 1, 1)
	return window, child
}

func drawToScreen(t *testing.T, primitive tview.Primitive, width, height int) tcell.SimulationScreen {
	screen := tcell.NewSimulationScreen("UTF-8")
	require.NoError(t, screen.Init())
	t.Cleanup(screen.Fini)
	screen.SetSize(width, height)
	primitive.SetRect(0, 0, width, height)
	primitive.Draw(screen)
	return screen
}

// bottomBorder returns the expected bottom border line of the given width, with the text right-aligned in it.
func bottomBorder(width int, text string) string {
	return "└" + strings.Repeat("─", width-2-len([]rune(text))) + text + "┘"
}

func screenLine(screen tcell.SimulationScreen, y int) string {
	width, _ := screen.Size()
	var line strings.Builder
	for x := 0; x < width; x++ {
		primary, _, _, _ := screen.GetContent(x, y)
		line.WriteRune(primary)
	}
	return line.String()
}

func TestBorderFooter_DrawsRightAlignedIntoBottomBorder(t *testing.T) {
	window, _ := newFooterTestWindow()
	window.SetTitle(" Title ")
	footer := NewBorderFooter(window.Box)
	footer.SetText("16 of 589")

	screen := drawToScreen(t, window, 30, 5)

	bottom := screenLine(screen, 4)
	// like the title, which starts right after the top left corner, the footer ends right before the bottom right corner
	assert.Equal(t, bottomBorder(30, " 16 of 589 "), bottom)
	// the title in the top border is not affected
	assert.Contains(t, screenLine(screen, 0), " Title ")
	assert.Equal(t, "16 of 589", footer.GetText())
}

func TestBorderFooter_KeepsTheDefaultInnerRect(t *testing.T) {
	withoutFooter, childWithoutFooter := newFooterTestWindow()
	drawToScreen(t, withoutFooter, 30, 6)

	withFooter, childWithFooter := newFooterTestWindow()
	NewBorderFooter(withFooter.Box).SetText("footer")
	drawToScreen(t, withFooter, 30, 6)

	// border and padding are respected, exactly like without a footer
	x, y, width, height := childWithFooter.GetRect()
	assert.Equal(t, [4]int{2, 1, 26, 4}, [4]int{x, y, width, height})
	ex, ey, ewidth, eheight := childWithoutFooter.GetRect()
	assert.Equal(t, [4]int{ex, ey, ewidth, eheight}, [4]int{x, y, width, height})

	// also after a resize
	drawToScreen(t, withFooter, 20, 10)
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

	screen := drawToScreen(t, window, 30, 5)

	assert.True(t, called)
	x, _, width, _ := child.GetRect()
	assert.Equal(t, 1, x)
	assert.Equal(t, 28, width)
	assert.Contains(t, screenLine(screen, 4), " footer ")
}

func TestBorderFooter_TruncatesLongText(t *testing.T) {
	window, _ := newFooterTestWindow()
	NewBorderFooter(window.Box).SetText("this text is way too long for the window")

	screen := drawToScreen(t, window, 20, 4)

	bottom := screenLine(screen, 3)
	// the beginning is kept, the corners stay intact
	assert.Equal(t, "└ this text is way…┘", bottom)
}

func TestBorderFooter_StyleTagsAndEmptyText(t *testing.T) {
	window, _ := newFooterTestWindow()
	footer := NewBorderFooter(window.Box)

	footer.SetText("")
	screen := drawToScreen(t, window, 20, 4)
	assert.Equal(t, bottomBorder(20, ""), screenLine(screen, 3))

	// style tags are interpreted, not printed
	footer.SetText("[#ff0000]red[-] text")
	screen = drawToScreen(t, window, 20, 4)
	bottom := screenLine(screen, 3)
	assert.Equal(t, bottomBorder(20, " red text "), bottom)
	x := strings.Index(bottom, "red")
	x = len([]rune(bottom[:x]))
	_, _, style, _ := screen.GetContent(x, 3)
	foreground, _, _ := style.Decompose()
	assert.Equal(t, tcell.NewHexColor(0xff0000), foreground)
}
