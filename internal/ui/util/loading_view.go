package util

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/rivo/tview"
)

var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

type LoadingView struct {
	*tview.TextView
	app     *tview.Application
	mu      sync.RWMutex
	message string
	cancel  context.CancelFunc
}

// NewLoadingView creates a new LoadingView that displays a loading message with a spinner.
func NewLoadingView(app *tview.Application, title string, message string) *LoadingView {
	textView := tview.NewTextView().
		SetTextAlign(tview.AlignCenter).
		SetDynamicColors(true)
	textView.SetBorder(true)
	SetupWindow(textView, title)

	v := &LoadingView{
		TextView: textView,
		app:      app,
		message:  message,
	}
	return v
}

func (v *LoadingView) SetMessage(message string) {
	v.mu.Lock()
	v.message = message
	v.mu.Unlock()
}

func (v *LoadingView) Start() {
	if v.cancel != nil {
		return // already running
	}
	ctx, cancel := context.WithCancel(context.Background())
	v.cancel = cancel

	frame := 0
	go func() {
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if v.app == nil {
					return
				}
				v.mu.RLock()
				msg := v.message
				v.mu.RUnlock()
				v.app.QueueUpdateDraw(func() {
					v.TextView.SetText(fmt.Sprintf("\n\n\n[yellow]%s[-] %s", spinnerFrames[frame], msg))
				})
				frame = (frame + 1) % len(spinnerFrames)
			}
		}
	}()
}

func (v *LoadingView) Stop() {
	if v.cancel != nil {
		v.cancel()
		v.cancel = nil
	}
}
