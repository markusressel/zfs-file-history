package util

import (
	"sync/atomic"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"github.com/stretchr/testify/assert"
)

func TestDebouncer(t *testing.T) {
	app := tview.NewApplication()
	app.SetScreen(tcell.NewSimulationScreen("UTF-8"))
	app.SetRoot(tview.NewBox(), true)
	go func() { _ = app.Run() }()
	t.Cleanup(app.Stop)
	onUiThread := func(f func()) {
		done := make(chan struct{})
		app.QueueUpdate(func() { f(); close(done) })
		<-done
	}

	const delay = 30 * time.Millisecond
	var debouncer *Debouncer
	onUiThread(func() { debouncer = NewDebouncer(app, delay) })

	// like holding an arrow key: only the last call runs, once the calls pause
	var calls atomic.Int32
	var last atomic.Int32
	for i := 1; i <= 10; i++ {
		onUiThread(func() {
			debouncer.Call(func() {
				calls.Add(1)
				last.Store(int32(i))
			})
		})
		time.Sleep(delay / 3)
	}
	assert.Eventually(t, func() bool { return calls.Load() == 1 }, 2*time.Second, 5*time.Millisecond)
	time.Sleep(3 * delay)
	assert.Equal(t, int32(1), calls.Load(), "runs once")
	assert.Equal(t, int32(10), last.Load(), "the last call")

	// cancelled
	onUiThread(func() {
		debouncer.Call(func() { calls.Add(1) })
		debouncer.Cancel()
	})
	time.Sleep(3 * delay)
	assert.Equal(t, int32(1), calls.Load())
}
