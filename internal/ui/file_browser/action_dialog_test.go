package file_browser

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// isOnUiThread returns whether it is called on the UI thread: a queued update can only run once the UI thread
// is free again, so it cannot complete while the caller blocks the UI thread.
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

type actionDialogTest struct {
	t           *testing.T
	app         *tview.Application
	screen      tcell.SimulationScreen
	fileBrowser *FileBrowserComponent
	dir         string
}

func newActionDialogTest(t *testing.T) *actionDialogTest {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a"), 0o644))

	app := tview.NewApplication()
	screen := tcell.NewSimulationScreen("UTF-8")
	app.SetScreen(screen)
	fileBrowser := NewFileBrowser(app)
	app.SetRoot(fileBrowser.GetLayout(), true)
	go func() { _ = app.Run() }()
	t.Cleanup(app.Stop)

	ft := &actionDialogTest{t: t, app: app, screen: screen, fileBrowser: fileBrowser, dir: dir}
	onUiThread(t, app, func() { fileBrowser.SetPath(dir, true) })
	require.Eventually(t, func() bool {
		selected := ""
		onUiThread(t, app, func() {
			if selection := fileBrowser.GetSelection(); selection != nil {
				selected = selection.Name
			}
		})
		return selected == "a.txt"
	}, 5*time.Second, 20*time.Millisecond)
	return ft
}

// selectMenuOption opens the action menu of the selected file and selects the option with the given number.
func (ft *actionDialogTest) selectMenuOption(number rune) {
	ft.screen.InjectKey(tcell.KeyEnter, 0, tcell.ModNone)
	require.Eventually(ft.t, func() bool { return ft.hasDialog("ActionDialog") }, 2*time.Second, 20*time.Millisecond)
	ft.screen.InjectKey(tcell.KeyRune, number, tcell.ModNone)
	ft.screen.InjectKey(tcell.KeyEnter, 0, tcell.ModNone)
}

func (ft *actionDialogTest) hasDialog(name string) bool {
	has := false
	onUiThread(ft.t, ft.app, func() {
		if pages, ok := ft.fileBrowser.GetLayout().(*tview.Pages); ok {
			has = pages.HasPage(name)
		}
	})
	return has
}

func setCreateSnapshot(t *testing.T, f func(path string) (string, error)) {
	original := createSnapshot
	createSnapshot = f
	t.Cleanup(func() { createSnapshot = original })
}

// Regression test: creating a snapshot from the file action menu emitted its event from the dialog's background
// goroutine, which made the main page update the UI off the UI thread and crashed the application.
func TestFileBrowser_CreateSnapshot(t *testing.T) {
	var mu sync.Mutex
	var createdForPath string
	setCreateSnapshot(t, func(path string) (string, error) {
		mu.Lock()
		defer mu.Unlock()
		createdForPath = path
		return "zfh-test", nil
	})

	ft := newActionDialogTest(t)

	type receivedEvent struct {
		name         string
		onUiThread   bool
		selectedName string
	}
	events := make(chan receivedEvent, 1)
	ft.fileBrowser.Events.Subscribe(func(event Event) {
		if e, ok := event.(SnapshotCreatedEvent); ok {
			received := receivedEvent{name: e.SnapshotName, onUiThread: isOnUiThread(ft.app)}
			// listeners update the UI, like the main page does
			if selection := ft.fileBrowser.GetSelection(); selection != nil {
				received.selectedName = selection.Name
			}
			events <- received
		}
	})

	ft.selectMenuOption('1') // Create Snapshot

	select {
	case e := <-events:
		assert.Equal(t, "zfh-test", e.name)
		assert.True(t, e.onUiThread, "SnapshotCreatedEvent must be emitted on the UI thread")
		assert.Equal(t, "a.txt", e.selectedName)
	case <-time.After(5 * time.Second):
		t.Fatal("no SnapshotCreatedEvent")
	}
	mu.Lock()
	assert.Equal(t, ft.dir, createdForPath, "the snapshot is created for the current directory")
	mu.Unlock()
	assert.False(t, ft.hasDialog("ErrorDialog"))
}

func TestFileBrowser_CreateSnapshotError(t *testing.T) {
	setCreateSnapshot(t, func(path string) (string, error) {
		return "", errors.New("permission denied")
	})

	ft := newActionDialogTest(t)
	events := make(chan Event, 1)
	ft.fileBrowser.Events.Subscribe(func(event Event) {
		if _, ok := event.(SnapshotCreatedEvent); ok {
			events <- event
		}
	})

	ft.selectMenuOption('1') // Create Snapshot

	// the error is shown in a dialog (on the UI thread), no event is emitted
	require.Eventually(t, func() bool { return ft.hasDialog("ErrorDialog") }, 5*time.Second, 20*time.Millisecond)
	select {
	case <-events:
		t.Fatal("no SnapshotCreatedEvent expected")
	case <-time.After(100 * time.Millisecond):
	}
}

func TestFileBrowser_DeleteError(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root can delete files in read-only directories")
	}
	ft := newActionDialogTest(t)
	// a file in a read-only directory cannot be deleted
	require.NoError(t, os.Chmod(ft.dir, 0o555))
	t.Cleanup(func() { _ = os.Chmod(ft.dir, 0o755) })

	ft.selectMenuOption('2') // Delete

	// the error is returned to the dialog, which shows it on the UI thread
	require.Eventually(t, func() bool { return ft.hasDialog("ErrorDialog") }, 5*time.Second, 20*time.Millisecond)
	assert.FileExists(t, filepath.Join(ft.dir, "a.txt"))
}
