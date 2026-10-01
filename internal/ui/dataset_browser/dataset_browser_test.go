package dataset_browser

import (
	"errors"
	"sync"
	"testing"
	"time"
	"zfs-file-history/internal/ui/table"
	"zfs-file-history/internal/zfs"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func names(entries []*zfs.DatasetListEntry) []string {
	var result []string
	for _, entry := range entries {
		result = append(result, entry.Name)
	}
	return result
}

func TestSortDatasetEntries(t *testing.T) {
	newEntries := func() []*zfs.DatasetListEntry {
		return []*zfs.DatasetListEntry{
			{Name: "pool/b", Used: 10, Available: 5, Mountpoint: "legacy", MountPath: "/b"},
			{Name: "Pool/a", Used: 30, Available: 5, Mountpoint: "none"},
			{Name: "pool/c", Used: 20, Available: 1, Mountpoint: "/c", MountPath: "/c"},
		}
	}

	tests := []struct {
		column   *table.Column
		inverted bool
		expected []string
	}{
		{columnName, false, []string{"Pool/a", "pool/b", "pool/c"}},
		{columnName, true, []string{"pool/c", "pool/b", "Pool/a"}},
		{columnUsed, false, []string{"pool/b", "pool/c", "Pool/a"}},
		{columnUsed, true, []string{"Pool/a", "pool/c", "pool/b"}},
		// equal values fall back to the name
		{columnAvail, false, []string{"pool/c", "Pool/a", "pool/b"}},
		// the actual mount path is used if mounted
		{columnMountpoint, false, []string{"pool/b", "pool/c", "Pool/a"}},
	}
	for _, test := range tests {
		t.Run(test.column.Title, func(t *testing.T) {
			result := sortDatasetEntries(newEntries(), test.column, test.inverted)
			assert.Equal(t, test.expected, names(result))
		})
	}
}

func TestFindEntryToSelect(t *testing.T) {
	root := &zfs.DatasetListEntry{Name: "rpool/ROOT", MountPath: "/"}
	home := &zfs.DatasetListEntry{Name: "rpool/home", MountPath: "/home"}
	homeUser := &zfs.DatasetListEntry{Name: "rpool/home/user", MountPath: "/home/user"}
	unmounted := &zfs.DatasetListEntry{Name: "rpool/legacy", Mountpoint: "legacy"}
	entries := []*zfs.DatasetListEntry{unmounted, root, home, homeUser}

	assert.Nil(t, findEntryToSelect(nil, "rpool/home", "/home"))

	// previous selection wins
	assert.Same(t, unmounted, findEntryToSelect(entries, "rpool/legacy", "/home/user"))
	// longest containing mount path
	assert.Same(t, homeUser, findEntryToSelect(entries, "", "/home/user/documents"))
	assert.Same(t, home, findEntryToSelect(entries, "", "/home"))
	// must match on path boundaries
	assert.Same(t, root, findEntryToSelect(entries, "", "/homeless"))
	// vanished previous selection falls back to the path
	assert.Same(t, home, findEntryToSelect(entries, "rpool/gone", "/home/other"))
	// no path match falls back to the first entry
	assert.Same(t, unmounted, findEntryToSelect([]*zfs.DatasetListEntry{unmounted, home}, "", "/srv"))
	assert.Same(t, unmounted, findEntryToSelect([]*zfs.DatasetListEntry{unmounted, home}, "", ""))
}

type recordedEvents struct {
	mu    sync.Mutex
	paths []string
}

func (r *recordedEvents) add(path string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.paths = append(r.paths, path)
}

func (r *recordedEvents) get() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string{}, r.paths...)
}

func setListDatasets(t *testing.T, f func() ([]*zfs.DatasetListEntry, error)) {
	original := listDatasets
	listDatasets = f
	t.Cleanup(func() { listDatasets = original })
}

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

// newBrowserApp creates a browser as the root of a running application.
// The root is set before the event loop starts, like in CreateUi, to avoid racing with the first draw.
func newBrowserApp(t *testing.T) (*tview.Application, *DatasetBrowserComponent, tcell.SimulationScreen) {
	app := tview.NewApplication()
	screen := tcell.NewSimulationScreen("UTF-8")
	app.SetScreen(screen)
	browser := NewDatasetBrowser(app)
	app.SetRoot(browser.GetLayout(), true)
	go func() { _ = app.Run() }()
	t.Cleanup(app.Stop)
	return app, browser, screen
}

func findByName(entries []*zfs.DatasetListEntry, name string) *zfs.DatasetListEntry {
	for _, entry := range entries {
		if entry.Name == name {
			return entry
		}
	}
	return nil
}

func TestDatasetBrowser_RefreshLoadsAndSelects(t *testing.T) {
	var mu sync.Mutex
	datasets := []*zfs.DatasetListEntry{
		{Name: "rpool/ROOT", Mountpoint: "/", MountPath: "/"},
		{Name: "rpool/home", Mountpoint: "/home", MountPath: "/home"},
		{Name: "rpool/legacy", Mountpoint: "legacy"},
	}
	setListDatasets(t, func() ([]*zfs.DatasetListEntry, error) {
		mu.Lock()
		defer mu.Unlock()
		return datasets, nil
	})

	app, browser, _ := newBrowserApp(t)

	events := &recordedEvents{}
	browser.Events.Subscribe(func(event Event) {
		if e, ok := event.(PathChangedEvent); ok {
			events.add(e.NewPath)
		}
	})

	onUiThread(t, app, func() {
		browser.SetPath("/home/user/documents", false)
		browser.Refresh(false)
	})

	assert.Eventually(t, func() bool { return len(events.get()) == 1 }, 2*time.Second, 10*time.Millisecond)
	assert.Equal(t, []string{"/home"}, events.get())

	onUiThread(t, app, func() {
		assert.Len(t, browser.tableContainer.GetEntries(), 3)
		require.NotNil(t, browser.tableContainer.GetSelectedEntry())
		assert.Equal(t, "rpool/home", browser.tableContainer.GetSelectedEntry().Name)
		assert.Equal(t, "/home", browser.GetPath())

		// selecting an unmounted dataset must not leak its mountpoint property as a path
		browser.tableContainer.Select(findByName(browser.tableContainer.GetEntries(), "rpool/legacy"))
		assert.Equal(t, "rpool/legacy", browser.tableContainer.GetSelectedEntry().Name)
	})
	assert.Equal(t, []string{"/home", ""}, events.get())

	// a reload with changed data keeps the selected dataset, even if its row index changes
	mu.Lock()
	datasets = []*zfs.DatasetListEntry{
		{Name: "rpool/a", Mountpoint: "/a", MountPath: "/a"},
		{Name: "rpool/ROOT", Mountpoint: "/", MountPath: "/"},
		{Name: "rpool/home", Mountpoint: "/home", MountPath: "/home"},
		{Name: "rpool/legacy", Mountpoint: "legacy"},
	}
	mu.Unlock()

	onUiThread(t, app, func() { browser.Refresh(false) })
	assert.Eventually(t, func() bool { return len(events.get()) == 3 }, 2*time.Second, 10*time.Millisecond)

	onUiThread(t, app, func() {
		assert.Len(t, browser.tableContainer.GetEntries(), 4)
		assert.Equal(t, "rpool/legacy", browser.tableContainer.GetSelectedEntry().Name)
	})
}

func TestDatasetBrowser_RefreshErrorKeepsEntries(t *testing.T) {
	fail := false
	var mu sync.Mutex
	setListDatasets(t, func() ([]*zfs.DatasetListEntry, error) {
		mu.Lock()
		defer mu.Unlock()
		if fail {
			return nil, errors.New("zfs list failed")
		}
		return []*zfs.DatasetListEntry{{Name: "rpool/home", MountPath: "/home"}}, nil
	})

	app, browser, _ := newBrowserApp(t)

	statusEvents := make(chan DatasetBrowserStatusEvent, 10)
	browser.Events.Subscribe(func(event Event) {
		if e, ok := event.(DatasetBrowserStatusEvent); ok {
			statusEvents <- e
		}
	})

	onUiThread(t, app, func() { browser.Refresh(false) })
	assert.Eventually(t, func() bool {
		count := 0
		onUiThread(t, app, func() { count = len(browser.tableContainer.GetEntries()) })
		return count == 1
	}, 2*time.Second, 10*time.Millisecond)

	mu.Lock()
	fail = true
	mu.Unlock()
	onUiThread(t, app, func() { browser.Refresh(false) })

	select {
	case e := <-statusEvents:
		assert.Contains(t, e.Message.Message, "zfs list failed")
	case <-time.After(2 * time.Second):
		t.Fatal("expected an error status event")
	}
	onUiThread(t, app, func() {
		assert.Len(t, browser.tableContainer.GetEntries(), 1)
	})
}

func TestDatasetBrowser_StaleLoadIsDiscarded(t *testing.T) {
	release := make(chan struct{})
	firstStarted := make(chan struct{})
	var mu sync.Mutex
	calls := 0
	setListDatasets(t, func() ([]*zfs.DatasetListEntry, error) {
		mu.Lock()
		calls++
		call := calls
		mu.Unlock()
		if call == 1 {
			close(firstStarted)
			<-release // the first (stale) load finishes last
			return []*zfs.DatasetListEntry{{Name: "stale"}}, nil
		}
		return []*zfs.DatasetListEntry{{Name: "fresh"}}, nil
	})

	app, browser, _ := newBrowserApp(t)

	onUiThread(t, app, func() { browser.Refresh(false) })
	select {
	case <-firstStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("first load did not start")
	}
	onUiThread(t, app, func() { browser.Refresh(false) })
	assert.Eventually(t, func() bool {
		name := ""
		onUiThread(t, app, func() {
			if entries := browser.tableContainer.GetEntries(); len(entries) == 1 {
				name = entries[0].Name
			}
		})
		return name == "fresh"
	}, 2*time.Second, 10*time.Millisecond)

	close(release)
	time.Sleep(100 * time.Millisecond)
	onUiThread(t, app, func() {
		assert.Equal(t, "fresh", browser.tableContainer.GetEntries()[0].Name)
	})
}

func TestDatasetBrowser_HeaderRowKeysChangeSortOrder(t *testing.T) {
	setListDatasets(t, func() ([]*zfs.DatasetListEntry, error) {
		return []*zfs.DatasetListEntry{
			{Name: "rpool/a", Used: 30},
			{Name: "rpool/b", Used: 10},
			{Name: "rpool/c", Used: 20},
		}, nil
	})

	app, browser, screen := newBrowserApp(t)

	onUiThread(t, app, func() { browser.Refresh(false) })
	entryNames := func() []string {
		var result []string
		onUiThread(t, app, func() { result = names(browser.tableContainer.GetEntries()) })
		return result
	}
	// default: sorted by name, descending
	assert.Eventually(t, func() bool {
		return assert.ObjectsAreEqual([]string{"rpool/c", "rpool/b", "rpool/a"}, entryNames())
	}, 2*time.Second, 10*time.Millisecond)

	pressKey := func(key tcell.Key) {
		screen.InjectKey(key, 0, tcell.ModNone)
		// wait until the event loop processed the key
		onUiThread(t, app, func() {})
		time.Sleep(20 * time.Millisecond)
	}

	// on a data row, right and enter are consumed by the browser and don't change the sort order
	onUiThread(t, app, func() { browser.tableContainer.SelectFirstIfExists() })
	pressKey(tcell.KeyRight)
	pressKey(tcell.KeyEnter)
	assert.Equal(t, []string{"rpool/c", "rpool/b", "rpool/a"}, entryNames())

	onUiThread(t, app, func() { browser.tableContainer.SelectHeader() })

	// right: next column (used), keeping the direction (descending)
	pressKey(tcell.KeyRight)
	assert.Equal(t, []string{"rpool/a", "rpool/c", "rpool/b"}, entryNames())

	// enter: toggle direction (ascending)
	pressKey(tcell.KeyEnter)
	assert.Equal(t, []string{"rpool/b", "rpool/c", "rpool/a"}, entryNames())

	// left: previous column (name), ascending
	pressKey(tcell.KeyLeft)
	assert.Equal(t, []string{"rpool/a", "rpool/b", "rpool/c"}, entryNames())
}
