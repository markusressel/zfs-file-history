package testutil

import (
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The lines of the screen have a rune per cell, so the text lines up with the columns: a space where nothing was
// drawn, a wide character once. FindInScreen returns the column on the screen, also behind a wide character.
func TestScreenLines(t *testing.T) {
	screen := NewScreen(t, 10, 2)
	tview.Print(screen, "a界b", 2, 1, 8, tview.AlignLeft, tcell.ColorDefault)

	assert.Equal(t, []string{"          ", "  a界b    "}, ScreenLines(screen))
	x, y, found := FindInScreen(screen, "b")
	assert.True(t, found)
	assert.Equal(t, []int{5, 1}, []int{x, y})
	_, _, found = FindInScreen(screen, "c")
	assert.False(t, found)
}

// NewScreen sets the size after initializing the screen, which resets it.
func TestNewScreen(t *testing.T) {
	width, height := NewScreen(t, 30, 4).Size()
	assert.Equal(t, []int{30, 4}, []int{width, height})
}

// Click, Press and Type return once the app handled the events, the waits once the screen or the pages show it.
func TestInputAndWaits(t *testing.T) {
	app, screen := NewApp(WithSize(30, 5))
	input := tview.NewInputField().SetLabel("Name: ")
	button := tview.NewButton("Go")
	// Only accessed on the UI thread.
	clicked := 0
	button.SetSelectedFunc(func() { clicked++ })
	pages := tview.NewPages().AddPage("main", tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(input, 1, 0, true).AddItem(button, 1, 0, false), true, true)
	StartApp(t, app, pages)

	Type(t, app, screen, "tank")
	Press(t, app, screen, tcell.KeyBackspace2, 0)
	OnUiThread(t, app, func() { assert.Equal(t, "tan", input.GetText()) })
	WaitForText(t, app, screen, "Name: tan", time.Second)

	ClickText(t, app, screen, "Go")
	OnUiThread(t, app, func() { assert.Equal(t, 1, clicked) })

	assert.False(t, HasPage(t, app, pages, "dialog"))
	OnUiThread(t, app, func() { pages.AddPage("dialog", tview.NewTextView().SetText("Dialog"), true, true) })
	WaitForPage(t, app, pages, "dialog", time.Second)
	WaitForText(t, app, screen, "Dialog", time.Second)
	OnUiThread(t, app, func() { pages.RemovePage("dialog") })
	WaitForNoPage(t, app, pages, "dialog", time.Second)
	WaitForNoText(t, app, screen, "Dialog", time.Second)
	x, y, found := FindText(t, app, screen, "Name")
	require.True(t, found)
	assert.Equal(t, []int{0, 0}, []int{x, y})
}
