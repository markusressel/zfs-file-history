package util

import (
	"sync"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

func TestDebouncedLoader_StartCancel(t *testing.T) {
	app := tview.NewApplication()
	simScreen := tcell.NewSimulationScreen("")
	app.SetScreen(simScreen)
	go app.Run()
	defer app.Stop()

	var showSpinnerCalled bool
	var mu sync.Mutex

	loader := NewDebouncedLoader(app, func() {
		mu.Lock()
		defer mu.Unlock()
		showSpinnerCalled = true
	})

	ctx, seq := loader.Start()
	if ctx == nil {
		t.Fatal("expected context to be non-nil")
	}
	if seq == 0 {
		t.Fatal("expected sequence to be > 0")
	}
	if !loader.IsCurrentSequence(seq) {
		t.Fatal("expected sequence to be current")
	}

	loader.Cancel()
	select {
	case <-ctx.Done():
		// expected
	default:
		t.Fatal("expected context to be cancelled")
	}

	mu.Lock()
	if showSpinnerCalled {
		t.Fatal("expected spinner to not be shown")
	}
	mu.Unlock()
}

func TestDebouncedLoader_ShowSpinner(t *testing.T) {
	app := tview.NewApplication()
	simScreen := tcell.NewSimulationScreen("")
	app.SetScreen(simScreen)
	go app.Run()
	defer app.Stop()

	var showSpinnerCalled bool
	var mu sync.Mutex

	loader := NewDebouncedLoader(app, func() {
		mu.Lock()
		defer mu.Unlock()
		showSpinnerCalled = true
	})

	_, seq := loader.Start()

	// Wait for the timer to fire and the queued update to execute
	time.Sleep(200 * time.Millisecond)

	if !loader.ShowLoadingSpinner() {
		t.Fatal("expected ShowLoadingSpinner to be true")
	}

	mu.Lock()
	if !showSpinnerCalled {
		t.Fatal("expected onShowSpinner to be called")
	}
	mu.Unlock()

	loader.Stop(seq)

	time.Sleep(50 * time.Millisecond)

	loader.mutex.Lock()
	if loader.timer != nil {
		t.Fatal("expected timer to be nil after Stop")
	}
	loader.mutex.Unlock()
}

func BenchmarkDebouncedLoader_StartCancel(b *testing.B) {
	app := tview.NewApplication()
	simScreen := tcell.NewSimulationScreen("")
	app.SetScreen(simScreen)
	go app.Run()
	defer app.Stop()

	loader := NewDebouncedLoader(app, func() {})
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		loader.Start()
		loader.Cancel()
	}
}
