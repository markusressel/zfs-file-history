package table

import (
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func key(k tcell.Key) *tcell.EventKey {
	return tcell.NewEventKey(k, 0, tcell.ModNone)
}

func ctrlKey(k tcell.Key) *tcell.EventKey {
	return tcell.NewEventKey(k, 0, tcell.ModCtrl)
}

func typeRunes(e *lineEditor, text string) {
	for _, r := range text {
		e.HandleKey(tcell.NewEventKey(tcell.KeyRune, r, tcell.ModNone))
	}
}

func TestLineEditor(t *testing.T) {
	e := &lineEditor{}
	typeRunes(e, "daly")
	assert.Equal(t, "daly", e.Text())
	assert.Equal(t, 4, e.cursor)

	// insert in the middle
	e.HandleKey(key(tcell.KeyLeft))
	e.HandleKey(key(tcell.KeyLeft))
	typeRunes(e, "i")
	assert.Equal(t, "daily", e.Text())
	assert.Equal(t, 3, e.cursor)

	// backspace and delete around the cursor
	e.HandleKey(key(tcell.KeyBackspace2))
	assert.Equal(t, "daly", e.Text())
	e.HandleKey(key(tcell.KeyDelete))
	assert.Equal(t, "day", e.Text())
	assert.Equal(t, 2, e.cursor)

	// home / end, also ctrl+a / ctrl+e
	e.HandleKey(key(tcell.KeyHome))
	assert.Equal(t, 0, e.cursor)
	e.HandleKey(key(tcell.KeyBackspace2)) // nothing before the cursor
	assert.Equal(t, "day", e.Text())
	e.HandleKey(key(tcell.KeyLeft)) // stays at the start
	assert.Equal(t, 0, e.cursor)
	e.HandleKey(key(tcell.KeyEnd))
	assert.Equal(t, 3, e.cursor)
	e.HandleKey(key(tcell.KeyRight))  // stays at the end
	e.HandleKey(key(tcell.KeyDelete)) // nothing at the cursor
	assert.Equal(t, 3, e.cursor)
	assert.Equal(t, "day", e.Text())
	e.HandleKey(key(tcell.KeyCtrlA))
	assert.Equal(t, 0, e.cursor)
	e.HandleKey(key(tcell.KeyCtrlE))
	assert.Equal(t, 3, e.cursor)

	// not an editing key
	assert.False(t, e.HandleKey(key(tcell.KeyF5)))
}

func TestLineEditor_Words(t *testing.T) {
	e := &lineEditor{}
	e.Reset("zfs-auto-snap_daily")
	assert.Equal(t, 19, e.cursor)

	// ctrl+left / ctrl+right jump between words
	e.HandleKey(ctrlKey(tcell.KeyLeft))
	assert.Equal(t, 14, e.cursor) // before "daily"
	e.HandleKey(ctrlKey(tcell.KeyLeft))
	assert.Equal(t, 9, e.cursor) // before "snap"
	e.HandleKey(ctrlKey(tcell.KeyRight))
	assert.Equal(t, 13, e.cursor) // after "snap"

	// ctrl+w deletes the word before the cursor
	e.HandleKey(key(tcell.KeyCtrlW))
	assert.Equal(t, "zfs-auto-_daily", e.Text())
	assert.Equal(t, 9, e.cursor)

	// ctrl+k deletes to the end, ctrl+u to the start
	e.HandleKey(key(tcell.KeyCtrlK))
	assert.Equal(t, "zfs-auto-", e.Text())
	e.HandleKey(key(tcell.KeyLeft))
	e.HandleKey(key(tcell.KeyCtrlU))
	assert.Equal(t, "-", e.Text())
	assert.Equal(t, 0, e.cursor)
}

func TestLineEditor_MultiByteRunes(t *testing.T) {
	e := &lineEditor{}
	typeRunes(e, "äöü")
	e.HandleKey(key(tcell.KeyLeft))
	e.HandleKey(key(tcell.KeyBackspace2))
	assert.Equal(t, "äü", e.Text())
}

// renderToScreen prints the rendered editor like a window title and returns the visible text
// and the index of the cell in reverse video (the cursor), -1 if none.
func renderToScreen(t *testing.T, e *lineEditor) (string, int) {
	screen := tcell.NewSimulationScreen("UTF-8")
	require.NoError(t, screen.Init())
	defer screen.Fini()
	screen.SetSize(40, 1)
	tview.Print(screen, e.Render(), 0, 0, 40, tview.AlignLeft, tcell.ColorWhite)

	var text strings.Builder
	cursor := -1
	for x := 0; x < 40; x++ {
		primary, _, style, _ := screen.GetContent(x, 0)
		text.WriteRune(primary)
		_, _, attributes := style.Decompose()
		if attributes&tcell.AttrReverse != 0 {
			require.Equal(t, -1, cursor, "only one cell must be reversed")
			cursor = x
		}
	}
	return strings.TrimRight(text.String(), " "), cursor
}

func TestLineEditor_Render(t *testing.T) {
	tests := []struct {
		text   string
		cursor int
	}{
		{"", 0},
		{"daily", 5},
		{"daily", 0},
		{"daily", 2},
		// brackets (glob character classes) must not be parsed as style tags
		{"daily-[0-9]", 11},
		{"daily-[0-9]", 6},
		{"daily-[0-9]", 7},
		{"daily-[0-9]", 10},
		{"[a]", 0},
		{"[a]", 1},
		{"[a]", 2},
		{"[::r]", 3},
		{"[", 0},
		{"]", 1},
	}
	for _, test := range tests {
		t.Run(test.text, func(t *testing.T) {
			e := &lineEditor{text: []rune(test.text), cursor: test.cursor}
			text, cursor := renderToScreen(t, e)
			assert.Equal(t, test.text, text)
			assert.Equal(t, test.cursor, cursor)
		})
	}
}

// renderWindowToScreen prints RenderWindow(maxWidth) and returns the visible text and the cursor column.
func renderWindowToScreen(t *testing.T, e *lineEditor, maxWidth int) (string, int) {
	screen := tcell.NewSimulationScreen("UTF-8")
	require.NoError(t, screen.Init())
	defer screen.Fini()
	screen.SetSize(40, 1)
	rendered := e.RenderWindow(maxWidth)
	tview.Print(screen, rendered, 0, 0, 40, tview.AlignLeft, tcell.ColorWhite)
	assert.LessOrEqual(t, tview.TaggedStringWidth(rendered), maxWidth)

	var text strings.Builder
	cursor := -1
	for x := 0; x < maxWidth; x++ {
		primary, _, style, _ := screen.GetContent(x, 0)
		text.WriteRune(primary)
		_, _, attributes := style.Decompose()
		if attributes&tcell.AttrReverse != 0 {
			cursor = x
		}
	}
	return text.String(), cursor
}

func TestLineEditor_RenderWindow(t *testing.T) {
	e := &lineEditor{}

	// fits: like Render
	e.Reset("daily")
	text, cursor := renderWindowToScreen(t, e, 10)
	assert.Equal(t, "daily     ", text)
	assert.Equal(t, 5, cursor)

	// cursor at the end of a long text: the beginning is cut off
	e.Reset("zfs-auto-snap_daily")
	text, cursor = renderWindowToScreen(t, e, 8)
	assert.Equal(t, "…_daily ", text)
	assert.Equal(t, 7, cursor)

	// cursor at the start: the end is cut off
	e.HandleKey(key(tcell.KeyHome))
	text, cursor = renderWindowToScreen(t, e, 8)
	assert.Equal(t, "zfs-aut…", text)
	assert.Equal(t, 0, cursor)

	// cursor in the middle, window scrolled to it: both sides cut off
	e.Reset("zfs-auto-snap_daily")
	for i := 0; i < 8; i++ {
		e.HandleKey(key(tcell.KeyLeft))
	}
	text, cursor = renderWindowToScreen(t, e, 6)
	assert.Equal(t, 5, cursor)
	assert.True(t, strings.HasPrefix(text, "…"))
	assert.Equal(t, "…o-sna", text)
	assert.Equal(t, "a", string([]rune(text)[5]), "the character at the cursor is shown")

	// brackets are not parsed as style tags
	e.Reset("daily-[0-9][a-z]")
	text, cursor = renderWindowToScreen(t, e, 8)
	assert.Equal(t, "…][a-z]", strings.TrimRight(text, " "))
	assert.Equal(t, 7, cursor)
}
