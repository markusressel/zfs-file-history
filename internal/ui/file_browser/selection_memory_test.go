package file_browser

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
	"zfs-file-history/internal/state"
	"zfs-file-history/internal/testutil"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"github.com/stretchr/testify/require"
)

// Regression test: entering a directory restored the remembered selection while the table still showed the
// entries of the parent directory, which overwrote the remembered selection with a (clamped) parent index.
func TestFileBrowser_RemembersSelectionWhenEnteringADirectoryAgain(t *testing.T) {
	parent := t.TempDir()
	for _, name := range []string{"a", "b", "c"} {
		require.NoError(t, os.WriteFile(filepath.Join(parent, name), []byte(name), 0o644))
	}
	child := filepath.Join(parent, "child")
	require.NoError(t, os.Mkdir(child, 0o755))
	for i := 0; i < 20; i++ {
		require.NoError(t, os.WriteFile(filepath.Join(child, fmt.Sprintf("f%02d", i)), []byte("x"), 0o644))
	}

	app := tview.NewApplication()
	screen := tcell.NewSimulationScreen("UTF-8")
	app.SetScreen(screen)
	fileBrowser := NewFileBrowser(app)
	app.SetRoot(fileBrowser.GetLayout(), true)
	go func() { _ = app.Run() }()
	t.Cleanup(app.Stop)

	type state struct {
		path     string
		count    int
		selected string
		index    int
	}
	getState := func() (s state) {
		testutil.OnUiThread(t, app, func() {
			s.path = fileBrowser.GetPath()
			entries := fileBrowser.tableContainer.GetEntries()
			s.count = len(entries)
			s.index = -1
			if selection := fileBrowser.GetSelection(); selection != nil {
				s.selected = selection.Name
				for i, entry := range entries {
					if entry == selection {
						s.index = i
					}
				}
			}
		})
		return s
	}
	waitFor := func(message string, condition func(s state) bool) state {
		var last state
		require.Eventually(t, func() bool {
			last = getState()
			return condition(last)
		}, 5*time.Second, 20*time.Millisecond, "%s (last state: %+v)", message, last)
		return last
	}
	pressKey := func(key tcell.Key) {
		screen.InjectKey(key, 0, tcell.ModNone)
	}

	testutil.OnUiThread(t, app, func() { fileBrowser.SetPath(child, true) })
	waitFor("child loaded", func(s state) bool { return s.path == child && s.count == 20 && s.index == 0 })

	// select the entry at index 10, which is larger than the number of entries in the parent directory
	for i := 0; i < 10; i++ {
		pressKey(tcell.KeyDown)
	}
	selectedInChild := waitFor("index 10 selected", func(s state) bool { return s.index == 10 }).selected

	// go up: the child directory is selected in the parent
	pressKey(tcell.KeyLeft)
	waitFor("parent loaded", func(s state) bool { return s.path == parent && s.count == 4 && s.selected == "child" })

	// enter the child directory again: the previously selected entry is selected again
	pressKey(tcell.KeyRight)
	s := waitFor("child loaded again", func(s state) bool { return s.path == child && s.count == 20 })
	time.Sleep(200 * time.Millisecond)
	s = getState()
	require.Equal(t, selectedInChild, s.selected, "remembered selection restored (state: %+v)", s)
	require.Equal(t, 10, s.index)
}

// The selections of the folders are remembered between runs.
func TestFileBrowser_RemembersSelectionBetweenRuns(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"a", "b", "c"} {
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(name), 0o644))
	}
	statePath := filepath.Join(t.TempDir(), "state.json")
	t.Cleanup(func() { state.Current = nil })

	// opens dir in a new browser, like after a restart, and returns it once its entries are shown
	open := func() (*tview.Application, *FileBrowserComponent, tcell.SimulationScreen) {
		state.Current = state.Load(statePath)
		app := tview.NewApplication()
		screen := tcell.NewSimulationScreen("UTF-8")
		app.SetScreen(screen)
		fileBrowser := NewFileBrowser(app)
		app.SetRoot(fileBrowser.GetLayout(), true)
		go func() { _ = app.Run() }()
		t.Cleanup(app.Stop)
		testutil.OnUiThread(t, app, func() { fileBrowser.SetPath(dir, true) })
		require.Eventually(t, func() bool {
			var count int
			testutil.OnUiThread(t, app, func() { count = len(fileBrowser.tableContainer.GetEntries()) })
			return count == 3
		}, 5*time.Second, 20*time.Millisecond)
		return app, fileBrowser, screen
	}
	selected := func(app *tview.Application, fileBrowser *FileBrowserComponent) string {
		var name string
		testutil.OnUiThread(t, app, func() {
			if selection := fileBrowser.GetSelection(); selection != nil {
				name = selection.Name
			}
		})
		return name
	}

	app, fileBrowser, screen := open()
	require.Eventually(t, func() bool { return selected(app, fileBrowser) == "a" }, 5*time.Second, 20*time.Millisecond)
	screen.InjectKey(tcell.KeyDown, 0, tcell.ModNone)
	screen.InjectKey(tcell.KeyDown, 0, tcell.ModNone)
	require.Eventually(t, func() bool { return selected(app, fileBrowser) == "c" }, 5*time.Second, 20*time.Millisecond)
	app.Stop()
	require.NoError(t, state.Current.Flush())

	app, fileBrowser, _ = open()
	require.Eventually(t, func() bool { return selected(app, fileBrowser) == "c" }, 5*time.Second, 20*time.Millisecond)
}
