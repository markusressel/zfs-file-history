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
	app := tview.NewApplication()
	screen := tcell.NewSimulationScreen("UTF-8")
	app.SetScreen(screen)
	app.EnableMouse(true)
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
