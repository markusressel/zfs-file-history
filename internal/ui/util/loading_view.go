package util

import (
	"context"
	"fmt"
	"sync"
	"time"
	"zfs-file-history/internal/ui/theme"
	"zfs-file-history/internal/ui/txwidgets"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// SpinnerFrames are the frames of the spinner that shows that something is loading. Each uses all four rows of dots of
// the braille cell, which spans the height of the line: frames of the upper three rows only looked raised next to text.
var SpinnerFrames = []string{"⣾", "⣽", "⣻", "⢿", "⡿", "⣟", "⣯", "⣷"}

// LargeSpinnerFrames are the frames of the spinner of a LoadingView, on a line of its own above the message, centered
// (a spinner in front of the message is off center, also with a line below): two cells, a ring of 4×4 dots of which
// half runs around.
var LargeSpinnerFrames = []string{"⣇⡀", "⣏⠀", "⡏⠁", "⠏⠉", "⠋⠙", "⠉⠹", "⠈⢹", "⠀⣹", "⢀⣸", "⣀⣰", "⣄⣠", "⣆⣀"}

// LoadingView shows that something is loading, in place of the content, centered like the empty state of a list (see
// DrawEmptyState): a spinner, below it the message, and below that the detail of how far it got (e.g. "40,123
// records · 23 MiB"), so a long message is not made longer by it.
type LoadingView struct {
	*tview.TextView
	app *tview.Application
	// mu guards message and detail, which are set from any thread
	mu      sync.RWMutex
	message string
	detail  string
	// frame is the frame of the spinner shown. Only accessed on the UI thread.
	frame  int
	cancel context.CancelFunc
}

// NewLoadingView creates a new LoadingView that displays a loading message with a spinner.
func NewLoadingView(app *tview.Application, title string, message string) *LoadingView {
	textView := tview.NewTextView()
	textView.SetBorder(true)
	SetupWindow(textView, title)

	v := &LoadingView{
		TextView: textView,
		app:      app,
		message:  message,
	}
	return v
}

// SetMessage sets what is loading, e.g. "Reading the history…". May be called on any thread.
func (v *LoadingView) SetMessage(message string) {
	v.mu.Lock()
	v.message = message
	v.mu.Unlock()
}

// SetDetail sets how far the loading got, shown in the line below the message ("" none). May be called on any thread.
func (v *LoadingView) SetDetail(detail string) {
	v.mu.Lock()
	v.detail = detail
	v.mu.Unlock()
}

// Draw draws the border (if any) and the spinner with the message and the detail, centered.
func (v *LoadingView) Draw(screen tcell.Screen) {
	v.Box.DrawForSubclass(screen, v)
	v.mu.RLock()
	message, detail := v.message, v.detail
	v.mu.RUnlock()
	colors := theme.Colors.Layout.Loading
	text := fmt.Sprintf("%s%s[-]\n\n%s", txwidgets.ColorTag(colors.Spinner), LargeSpinnerFrames[v.frame%len(LargeSpinnerFrames)], tview.Escape(message))
	if detail != "" {
		text += fmt.Sprintf("\n%s%s[-]", txwidgets.ColorTag(colors.Detail), tview.Escape(detail))
	}
	x, y, width, height := v.GetInnerRect()
	DrawEmptyState(screen, x, y, width, height, text, colors.Message)
}

// Start moves the spinner until Stop is called.
func (v *LoadingView) Start() {
	if v.cancel != nil {
		return // already running
	}
	ctx, cancel := context.WithCancel(context.Background())
	v.cancel = cancel

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
				v.app.QueueUpdateDraw(func() { v.frame++ })
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
