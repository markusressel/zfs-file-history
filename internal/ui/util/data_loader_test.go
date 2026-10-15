package util

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
	"zfs-file-history/internal/testutil"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDataLoader_LoadSuccess(t *testing.T) {
	app, _ := testutil.NewApp()
	testutil.RunApp(t, app)

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

	waitForDelivered(t, loader)

	mu.Lock()
	assert.True(t, startCalled)
	assert.True(t, loadCalled)
	assert.Equal(t, "hello data", loadedData)
	mu.Unlock()
}

func TestDataLoader_LoadQuietly(t *testing.T) {
	app, _ := testutil.NewApp()
	testutil.RunApp(t, app)

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

	waitForDelivered(t, loader)

	mu.Lock()
	assert.False(t, startCalled)
	assert.True(t, loadCalled)
	mu.Unlock()
}

func TestDataLoader_LoadError(t *testing.T) {
	app, _ := testutil.NewApp()
	testutil.RunApp(t, app)

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

	waitForDelivered(t, loader)

	mu.Lock()
	assert.True(t, errCalled)
	assert.Equal(t, expectedErr, returnedErr)
	mu.Unlock()
}

func TestDataLoader_CancellationAndSequenceTracking(t *testing.T) {
	app, _ := testutil.NewApp()
	testutil.RunApp(t, app)

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

	// Once the second one was delivered, the first one cannot be anymore: it is outdated.
	waitForDelivered(t, loader)

	mu.Lock()
	assert.False(t, firstLoadCalled, "expected first load to be cancelled and ignored")
	assert.True(t, secondLoadCalled, "expected second load to complete")
	mu.Unlock()
}

// waitForDelivered waits until the result of the last load was handed to OnLoad or OnError, instead of a time that is
// too short on a busy machine.
func waitForDelivered[T any](t *testing.T, loader *DataLoader[T]) {
	t.Helper()
	require.Eventually(t, func() bool { return !loader.IsLoading() }, 3*time.Second, 5*time.Millisecond)
}
