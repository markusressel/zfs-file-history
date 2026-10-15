package testutil

import (
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"github.com/stretchr/testify/require"
)

// Click clicks with the left button at the position on the screen of the running app (pressed and released, like a
// terminal reports it) and waits until the app handled it (see WaitForEvents): a test that checked the result right
// after injecting the click read it before the click was handled on a slow machine.
func Click(t testing.TB, app *tview.Application, screen tcell.SimulationScreen, x int, y int) {
	t.Helper()
	screen.InjectMouse(x, y, tcell.Button1, tcell.ModNone)
	screen.InjectMouse(x, y, tcell.ButtonNone, tcell.ModNone)
	WaitForEvents(t, app, screen)
}

// ClickText clicks on the first occurrence of text on the screen (see FindText and Click), failing the test if it is
// not shown.
func ClickText(t testing.TB, app *tview.Application, screen tcell.SimulationScreen, text string) {
	t.Helper()
	x, y, found := FindText(t, app, screen, text)
	require.True(t, found, "%q is shown", text)
	Click(t, app, screen, x, y)
}

// Press presses the key (r for tcell.KeyRune) in the running app and waits until the app handled it (see Click).
func Press(t testing.TB, app *tview.Application, screen tcell.SimulationScreen, key tcell.Key, r rune) {
	t.Helper()
	screen.InjectKey(key, r, tcell.ModNone)
	WaitForEvents(t, app, screen)
}

// Type types the text into the running app, a key per rune, and waits until the app handled them (see Click).
func Type(t testing.TB, app *tview.Application, screen tcell.SimulationScreen, text string) {
	t.Helper()
	for _, r := range text {
		screen.InjectKey(tcell.KeyRune, r, tcell.ModNone)
	}
	WaitForEvents(t, app, screen)
}

// Drag drags with the left button from one position to another on the screen of the running app (pressed, moved
// while pressed, released) and waits until the app handled it (see Click).
func Drag(t testing.TB, app *tview.Application, screen tcell.SimulationScreen, fromX int, fromY int, toX int, toY int) {
	t.Helper()
	screen.InjectMouse(fromX, fromY, tcell.Button1, tcell.ModNone)
	screen.InjectMouse(toX, toY, tcell.Button1, tcell.ModNone)
	screen.InjectMouse(toX, toY, tcell.ButtonNone, tcell.ModNone)
	WaitForEvents(t, app, screen)
}
