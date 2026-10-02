package util

import (
	"time"

	"github.com/rivo/tview"
)

// SelectionDebounceDelay is how long the selection has to rest before expensive work for it starts (loading the
// details of the selected entry), so holding an arrow key does not start it for every entry passed.
const SelectionDebounceDelay = 50 * time.Millisecond

// Debouncer runs a function on the UI thread once calls to Call have paused for its delay; only the function of
// the last call runs. All methods must be called on the UI thread.
type Debouncer struct {
	application *tview.Application
	delay       time.Duration
	timer       *time.Timer
	// sequence identifies the last call, so a timer that fired before it was stopped does nothing
	sequence uint64
}

// NewDebouncer creates a debouncer with the given delay.
func NewDebouncer(application *tview.Application, delay time.Duration) *Debouncer {
	return &Debouncer{application: application, delay: delay}
}

// Call runs f on the UI thread after the delay, unless Call or Cancel is called again before.
func (d *Debouncer) Call(f func()) {
	d.Cancel()
	sequence := d.sequence
	d.timer = time.AfterFunc(d.delay, func() {
		// on the goroutine of the timer
		d.application.QueueUpdateDraw(func() {
			if sequence == d.sequence {
				f()
			}
		})
	})
}

// Cancel drops the pending function, if any.
func (d *Debouncer) Cancel() {
	d.sequence++
	if d.timer != nil {
		d.timer.Stop()
		d.timer = nil
	}
}
