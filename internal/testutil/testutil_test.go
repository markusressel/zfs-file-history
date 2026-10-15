package testutil

import (
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"github.com/stretchr/testify/assert"
)

// Once WaitForEvents returns, the events injected before were handled, and the input capture of the app is the one
// of before.
func TestWaitForEvents(t *testing.T) {
	app, screen := NewApp()
	// Only accessed on the UI thread.
	keys, clicks, captured := 0, 0, 0
	box := tview.NewBox()
	box.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		keys++
		return nil
	})
	box.SetMouseCapture(func(action tview.MouseAction, event *tcell.EventMouse) (tview.MouseAction, *tcell.EventMouse) {
		if action == tview.MouseLeftDown {
			clicks++
		}
		return action, event
	})
	app.SetRoot(box, true)
	app.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		captured++
		return event
	})
	RunApp(t, app)

	for range 50 {
		screen.InjectKey(tcell.KeyRune, 'a', tcell.ModNone)
		screen.InjectMouse(1, 1, tcell.Button1, tcell.ModNone)
		screen.InjectMouse(1, 1, tcell.ButtonNone, tcell.ModNone)
	}
	WaitForEvents(t, app, screen)
	OnUiThread(t, app, func() {
		assert.Equal(t, 50, keys)
		assert.Equal(t, 50, clicks)
		assert.Equal(t, 50, captured, "the barrier is not passed on")
	})

	screen.InjectKey(tcell.KeyRune, 'b', tcell.ModNone)
	WaitForEvents(t, app, screen)
	OnUiThread(t, app, func() { assert.Equal(t, 51, captured, "the input capture of before") })
}

// The apps of NewApp are set up like the application: clicks reach the primitives (the mouse is enabled), and two
// clicks right after each other are two clicks, not a double click.
func TestNewApp(t *testing.T) {
	app, screen := NewApp(WithSize(40, 10))
	width, height := screen.Size()
	assert.Equal(t, []int{40, 10}, []int{width, height})

	// Only accessed on the UI thread.
	clicks, doubleClicks := 0, 0
	box := tview.NewBox()
	box.SetMouseCapture(func(action tview.MouseAction, event *tcell.EventMouse) (tview.MouseAction, *tcell.EventMouse) {
		switch action {
		case tview.MouseLeftClick:
			clicks++
		case tview.MouseLeftDoubleClick:
			doubleClicks++
		}
		return action, event
	})
	StartApp(t, app, box)

	for range 2 {
		screen.InjectMouse(1, 1, tcell.Button1, tcell.ModNone)
		screen.InjectMouse(1, 1, tcell.ButtonNone, tcell.ModNone)
	}
	WaitForEvents(t, app, screen)
	OnUiThread(t, app, func() {
		assert.Equal(t, 2, clicks)
		assert.Zero(t, doubleClicks)
	})
}
