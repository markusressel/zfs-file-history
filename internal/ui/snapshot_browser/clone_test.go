package snapshot_browser

import (
	"errors"
	"sync"
	"testing"
	"time"
	"zfs-file-history/internal/data"
	"zfs-file-history/internal/zfs"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// onUiThread runs f on the UI thread and waits for it, failing the test instead of hanging on a deadlock.
func onUiThread(t *testing.T, app *tview.Application, f func()) {
	done := make(chan struct{})
	go func() {
		app.QueueUpdate(f)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for the UI thread (deadlock?)")
	}
}

type cloneCall struct {
	snapshot   string
	target     string
	onUiThread bool
}

// isOnUiThread returns whether it is called on the UI thread (a queued update cannot complete meanwhile).
func isOnUiThread(app *tview.Application) bool {
	done := make(chan struct{})
	go func() {
		app.QueueUpdate(func() {})
		close(done)
	}()
	select {
	case <-done:
		return false
	case <-time.After(200 * time.Millisecond):
		return true
	}
}

func runCloneFlow(t *testing.T, cloneErr error) (tview.Primitive, *tview.Application, []cloneCall, func(name string) bool) {
	var mu sync.Mutex
	var calls []cloneCall
	var app *tview.Application
	original := cloneSnapshot
	cloneSnapshot = func(snapshot *zfs.Snapshot, targetName string) error {
		call := cloneCall{snapshot: snapshot.FullName, target: targetName, onUiThread: isOnUiThread(app)}
		mu.Lock()
		calls = append(calls, call)
		mu.Unlock()
		return cloneErr
	}
	t.Cleanup(func() { cloneSnapshot = original })

	app = tview.NewApplication()
	screen := tcell.NewSimulationScreen("UTF-8")
	app.SetScreen(screen)
	browser := NewSnapshotBrowser(app)
	app.SetRoot(browser.GetLayout(), true)
	go func() { _ = app.Run() }()
	t.Cleanup(app.Stop)

	entry := &data.SnapshotBrowserEntry{Snapshot: &zfs.Snapshot{Name: "daily-1", FullName: "pool/data@daily-1"}}
	onUiThread(t, app, func() {
		browser.tableContainer.SetData([]*data.SnapshotBrowserEntry{entry})
		browser.tableContainer.Select(entry)
	})

	hasDialog := func(name string) bool {
		has := false
		onUiThread(t, app, func() { has = browser.container.Pages.HasPage(name) })
		return has
	}
	press := func(key tcell.Key, r rune) { screen.InjectKey(key, r, tcell.ModNone) }

	// Enter: snapshot menu, option 2: Clone
	press(tcell.KeyEnter, 0)
	require.Eventually(t, func() bool { return hasDialog("SnapshotActionDialog") }, 2*time.Second, 10*time.Millisecond)
	press(tcell.KeyRune, '2')
	press(tcell.KeyEnter, 0)
	require.Eventually(t, func() bool { return hasDialog("CloneSnapshotDialog") }, 2*time.Second, 10*time.Millisecond)

	// the suggested name is prefilled, replace it
	press(tcell.KeyCtrlU, 0)
	for _, r := range "pool/clone" {
		press(tcell.KeyRune, r)
	}
	press(tcell.KeyEnter, 0)

	require.Eventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(calls) == 1
	}, 5*time.Second, 10*time.Millisecond)
	mu.Lock()
	result := append([]cloneCall{}, calls...)
	mu.Unlock()
	return browser.GetLayout(), app, result, hasDialog
}

func TestSnapshotBrowser_Clone(t *testing.T) {
	_, _, calls, hasDialog := runCloneFlow(t, nil)

	assert.Equal(t, "pool/data@daily-1", calls[0].snapshot)
	assert.Equal(t, "pool/clone", calls[0].target)
	assert.False(t, calls[0].onUiThread, "the clone must be created in the background")
	require.Eventually(t, func() bool { return hasDialog("SuccessDialog") }, 5*time.Second, 10*time.Millisecond)
}

func TestSnapshotBrowser_CloneError(t *testing.T) {
	_, _, calls, hasDialog := runCloneFlow(t, errors.New("permission denied"))

	assert.Len(t, calls, 1)
	require.Eventually(t, func() bool { return hasDialog("ErrorDialog") }, 5*time.Second, 10*time.Millisecond)
	assert.False(t, hasDialog("SuccessDialog"))
}
