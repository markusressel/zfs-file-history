package util

import (
	"fmt"
	"time"

	"github.com/rivo/tview"
)

var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// NewLoadingView creates a new TextView that displays a loading message with a spinner.
func NewLoadingView(app *tview.Application, title string, message string) *tview.TextView {
	textView := tview.NewTextView().
		SetTextAlign(tview.AlignCenter).
		SetDynamicColors(true)
	textView.SetBorder(true)
	SetupWindow(textView, title)

	frame := 0
	go func() {
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()
		for range ticker.C {
			// We can't easily know if the textView is still "in use" without
			// extra state, but we can check if it's still attached to the app
			// if we had a reference to the main layout.
			// For now, let's just ensure we don't crash if app is nil.
			if app == nil {
				return
			}

			app.QueueUpdateDraw(func() {
				textView.SetText(fmt.Sprintf("\n\n\n[yellow]%s[-] %s", spinnerFrames[frame], message))
			})
			frame = (frame + 1) % len(spinnerFrames)
		}
	}()

	return textView
}
