package util

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"github.com/stretchr/testify/assert"
)

func TestDataLoader_LoadSuccess(t *testing.T) {
	app := tview.NewApplication()
	simScreen := tcell.NewSimulationScreen("")
	app.SetScreen(simScreen)
	go app.Run()
	defer app.Stop()

	loader := NewDataLoader[string](app)

	var startCalled bool
	var loadCalled bool
	var loadedData string
	var mu sync.Mutex

	loader.OnStart(func() {
		mu.Lock()
		defer mu.Unlock()
		startCalled = true
	})
	loader.OnLoad(func(data string) {
		mu.Lock()
		defer mu.Unlock()
		loadCalled = true
		loadedData = data
	})

	loader.Load(func(ctx context.Context) (string, error) {
		return "hello data", nil
	})

	// Wait for async task and QueueUpdateDraw to execute
	time.Sleep(100 * time.Millisecond)

	mu.Lock()
	assert.True(t, startCalled)
	assert.True(t, loadCalled)
	assert.Equal(t, "hello data", loadedData)
	mu.Unlock()
}

func TestDataLoader_LoadQuietly(t *testing.T) {
	app := tview.NewApplication()
	simScreen := tcell.NewSimulationScreen("")
	app.SetScreen(simScreen)
	go app.Run()
	defer app.Stop()

	loader := NewDataLoader[string](app)

	var startCalled bool
	var loadCalled bool
	var mu sync.Mutex

	loader.OnStart(func() {
		mu.Lock()
		defer mu.Unlock()
		startCalled = true
	})
	loader.OnLoad(func(data string) {
		mu.Lock()
		defer mu.Unlock()
		loadCalled = true
	})

	loader.LoadQuietly(func(ctx context.Context) (string, error) {
		return "quiet hello", nil
	})

	time.Sleep(100 * time.Millisecond)

	mu.Lock()
	assert.False(t, startCalled)
	assert.True(t, loadCalled)
	mu.Unlock()
}

func TestDataLoader_LoadError(t *testing.T) {
	app := tview.NewApplication()
	simScreen := tcell.NewSimulationScreen("")
	app.SetScreen(simScreen)
	go app.Run()
	defer app.Stop()

	loader := NewDataLoader[string](app)

	var errCalled bool
	var returnedErr error
	var mu sync.Mutex

	loader.OnError(func(err error) {
		mu.Lock()
		defer mu.Unlock()
		errCalled = true
		returnedErr = err
	})

	expectedErr := errors.New("loading error")
	loader.Load(func(ctx context.Context) (string, error) {
		return "", expectedErr
	})

	time.Sleep(100 * time.Millisecond)

	mu.Lock()
	assert.True(t, errCalled)
	assert.Equal(t, expectedErr, returnedErr)
	mu.Unlock()
}

func TestDataLoader_CancellationAndSequenceTracking(t *testing.T) {
	app := tview.NewApplication()
	simScreen := tcell.NewSimulationScreen("")
	app.SetScreen(simScreen)
	go app.Run()
	defer app.Stop()

	loader := NewDataLoader[string](app)

	var firstLoadCalled bool
	var secondLoadCalled bool
	var mu sync.Mutex

	loader.OnLoad(func(data string) {
		mu.Lock()
		defer mu.Unlock()
		if data == "first" {
			firstLoadCalled = true
		} else if data == "second" {
			secondLoadCalled = true
		}
	})

	// Start first load which takes a bit of time
	loader.Load(func(ctx context.Context) (string, error) {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(50 * time.Millisecond):
			return "first", nil
		}
	})

	// Start second load immediately, which should cancel the first
	loader.Load(func(ctx context.Context) (string, error) {
		return "second", nil
	})

	time.Sleep(150 * time.Millisecond)

	mu.Lock()
	assert.False(t, firstLoadCalled, "expected first load to be cancelled and ignored")
	assert.True(t, secondLoadCalled, "expected second load to complete")
	mu.Unlock()
}
