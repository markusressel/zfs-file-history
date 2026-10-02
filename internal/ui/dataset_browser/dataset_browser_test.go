package dataset_browser

import (
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"
	"zfs-file-history/internal/state"
	"zfs-file-history/internal/ui/dialog"
	"zfs-file-history/internal/ui/table"
	uiutil "zfs-file-history/internal/ui/util"
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
			{Name: "pool/b", Used: 10, Available: 5, UsedBySnapshots: 1, UsedByDataset: 9, UsedByChildren: 0, UsedByRefreservation: 0, Mountpoint: "legacy", MountPath: "/b"},
			{Name: "Pool/a", Used: 30, Available: 5, UsedBySnapshots: 25, UsedByDataset: 1, UsedByChildren: 4, UsedByRefreservation: 0, Mountpoint: "none"},
			{Name: "pool/c", Used: 20, Available: 1, UsedBySnapshots: 0, UsedByDataset: 15, UsedByChildren: 5, UsedByRefreservation: 7, Mountpoint: "/c", MountPath: "/c"},
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
		{columnUsedBySnapshots, false, []string{"pool/c", "pool/b", "Pool/a"}},
		{columnUsedBySnapshots, true, []string{"Pool/a", "pool/b", "pool/c"}},
		{columnUsedByDataset, false, []string{"Pool/a", "pool/b", "pool/c"}},
		{columnUsedByDataset, true, []string{"pool/c", "pool/b", "Pool/a"}},
		{columnUsedByChildren, false, []string{"pool/b", "Pool/a", "pool/c"}},
		{columnUsedByChildren, true, []string{"pool/c", "Pool/a", "pool/b"}},
		// equal values fall back to the name, inverted as well
		{columnUsedByRefreservation, false, []string{"Pool/a", "pool/b", "pool/c"}},
		{columnUsedByRefreservation, true, []string{"pool/c", "pool/b", "Pool/a"}},
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
		// this test selects unmounted datasets, which are hidden by default
		browser.ToggleHideUnmounted()
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
			return []*zfs.DatasetListEntry{{Name: "stale", MountPath: "/stale"}}, nil
		}
		return []*zfs.DatasetListEntry{{Name: "fresh", MountPath: "/fresh"}}, nil
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
			{Name: "rpool/a", Used: 30, MountPath: "/a"},
			{Name: "rpool/b", Used: 10, MountPath: "/b"},
			{Name: "rpool/c", Used: 20, MountPath: "/c"},
		}, nil
	})

	app, browser, screen := newBrowserApp(t)

	onUiThread(t, app, func() { browser.Refresh(false) })
	entryNames := func() []string {
		var result []string
		onUiThread(t, app, func() { result = names(browser.tableContainer.GetEntries()) })
		return result
	}
	// default: sorted by name, ascending
	assert.Eventually(t, func() bool {
		return assert.ObjectsAreEqual([]string{"rpool/a", "rpool/b", "rpool/c"}, entryNames())
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
	assert.Equal(t, []string{"rpool/a", "rpool/b", "rpool/c"}, entryNames())

	onUiThread(t, app, func() { browser.tableContainer.SelectHeader() })

	// right: next column (used), keeping the direction (ascending)
	pressKey(tcell.KeyRight)
	assert.Equal(t, []string{"rpool/b", "rpool/c", "rpool/a"}, entryNames())

	// enter: toggle direction (descending)
	pressKey(tcell.KeyEnter)
	assert.Equal(t, []string{"rpool/a", "rpool/c", "rpool/b"}, entryNames())

	// left: previous column (name), descending
	pressKey(tcell.KeyLeft)
	assert.Equal(t, []string{"rpool/c", "rpool/b", "rpool/a"}, entryNames())
}

func TestFilterEntries(t *testing.T) {
	mounted := &zfs.DatasetListEntry{Name: "rpool/home", MountPath: "/home"}
	unmounted := &zfs.DatasetListEntry{Name: "rpool/legacy", Mountpoint: "legacy"}
	entries := []*zfs.DatasetListEntry{unmounted, mounted}

	assert.Equal(t, entries, filterEntries(entries, false))
	assert.Equal(t, []*zfs.DatasetListEntry{mounted}, filterEntries(entries, true))
	assert.Empty(t, filterEntries(nil, true))

	// a new slice is returned, so sorting the result never reorders the input
	result := filterEntries(entries, false)
	result[0], result[1] = result[1], result[0]
	assert.Same(t, unmounted, entries[0])
}

func TestFormatStatus(t *testing.T) {
	entries := []*zfs.DatasetListEntry{
		{Name: "rpool", Mountpoint: "/"},
		{Name: "rpool/home", MountPath: "/home"},
		{Name: "rpool/legacy", Mountpoint: "legacy"},
	}

	// strips the color tags
	plain := func(text string) string {
		textView := tview.NewTextView().SetDynamicColors(true).SetText(text)
		return textView.GetText(true)
	}

	assert.Equal(t, "Loading datasets...", plain(formatStatus(nil, 0, true, false)))
	assert.Equal(t, "3 datasets", plain(formatStatus(entries, 3, false, false)))
	assert.Equal(t, "1 of 3 datasets · 2 unmounted hidden", plain(formatStatus(entries, 1, true, false)))
	assert.Equal(t, "0 datasets · 0 unmounted hidden", plain(formatStatus([]*zfs.DatasetListEntry{}, 0, true, false)))
	// with an active filter, always "x of y", even if all datasets match
	assert.Equal(t, "3 of 3 datasets", plain(formatStatus(entries, 3, false, true)))
	assert.Equal(t, "1 dataset", plain(formatStatus(entries[1:2], 1, false, false)))
	assert.Equal(t, "1 of 3 datasets · 2 unmounted hidden", plain(formatStatus(entries, 1, true, true)))
}

func TestDatasetBrowser_ToggleHideUnmounted(t *testing.T) {
	var mu sync.Mutex
	datasets := []*zfs.DatasetListEntry{
		{Name: "rpool", Mountpoint: "/"},
		{Name: "rpool/ROOT", Mountpoint: "/", MountPath: "/"},
		{Name: "rpool/home", Mountpoint: "/home", MountPath: "/home"},
		{Name: "rpool/legacy", Mountpoint: "legacy"},
	}
	setListDatasets(t, func() ([]*zfs.DatasetListEntry, error) {
		mu.Lock()
		defer mu.Unlock()
		return datasets, nil
	})

	app, browser, screen := newBrowserApp(t)
	pressKey := func(r rune) {
		screen.InjectKey(tcell.KeyRune, r, tcell.ModNone)
		// wait until the event loop processed the key
		onUiThread(t, app, func() {})
		time.Sleep(20 * time.Millisecond)
	}
	state := func() (visible []string, selected string, status string, hiding bool, shortcut string) {
		onUiThread(t, app, func() {
			visible = names(browser.tableContainer.GetEntries())
			if entry := browser.tableContainer.GetSelectedEntry(); entry != nil {
				selected = entry.Name
			}
			status = tview.NewTextView().SetDynamicColors(true).SetText(browser.tableContainer.GetFooter()).GetText(true)
			hiding = browser.IsHidingUnmounted()
			for _, entry := range browser.GetShortcutMap() {
				if len(entry.KeyCombo) == 1 && entry.KeyCombo[0] == "u" {
					shortcut = entry.Name
				}
			}
		})
		return
	}

	_, _, status, hiding, _ := state()
	assert.True(t, hiding, "unmounted datasets are hidden by default")
	assert.Equal(t, "Loading datasets...", status)

	onUiThread(t, app, func() {
		browser.SetPath("/home/user", false)
		browser.Refresh(false)
	})
	assert.Eventually(t, func() bool {
		visible, _, _, _, _ := state()
		return len(visible) == 2
	}, 2*time.Second, 10*time.Millisecond)

	visible, selected, status, hiding, shortcut := state()
	assert.ElementsMatch(t, []string{"rpool/ROOT", "rpool/home"}, visible)
	assert.Equal(t, "rpool/home", selected)
	assert.Equal(t, "2 of 4 datasets · 2 unmounted hidden", status)
	assert.Equal(t, "Show unmounted", shortcut)

	// show: the selected dataset stays selected
	pressKey('u')
	visible, selected, status, hiding, shortcut = state()
	assert.False(t, hiding)
	assert.Len(t, visible, 4)
	assert.Equal(t, "rpool/home", selected)
	assert.Equal(t, "4 datasets", status)
	assert.Equal(t, "Hide unmounted", shortcut)

	// select an unmounted dataset, then hide: the selection falls back to a visible dataset
	onUiThread(t, app, func() {
		for _, entry := range browser.tableContainer.GetEntries() {
			if entry.Name == "rpool/legacy" {
				browser.tableContainer.Select(entry)
			}
		}
	})
	_, selected, _, _, _ = state()
	assert.Equal(t, "rpool/legacy", selected)
	pressKey('u')
	visible, selected, _, _, _ = state()
	assert.ElementsMatch(t, []string{"rpool/ROOT", "rpool/home"}, visible)
	assert.Contains(t, visible, selected)

	// a reload keeps the filter
	mu.Lock()
	datasets = append(datasets, &zfs.DatasetListEntry{Name: "rpool/new", MountPath: "/new"}, &zfs.DatasetListEntry{Name: "rpool/new-unmounted"})
	mu.Unlock()
	onUiThread(t, app, func() { browser.Refresh(false) })
	assert.Eventually(t, func() bool {
		visible, _, status, _, _ := state()
		return len(visible) == 3 && status == "3 of 6 datasets · 3 unmounted hidden"
	}, 2*time.Second, 10*time.Millisecond)

	// toggling on the header row keeps the header row selected (for sorting)
	onUiThread(t, app, func() { browser.tableContainer.SelectHeader() })
	pressKey('u')
	visible, selected, status, hiding, _ = state()
	assert.False(t, hiding)
	assert.Len(t, visible, 6)
	assert.Empty(t, selected)
	assert.Equal(t, "6 datasets", status)
}

func TestDatasetMatchesFilter(t *testing.T) {
	home := &zfs.DatasetListEntry{Name: "rpool/arch/DATA/default/home", Mountpoint: "/home", MountPath: "/home"}
	boot := &zfs.DatasetListEntry{Name: "bpool/arch/BOOT/pac-4khx6p", Mountpoint: "legacy", MountPath: "/boot"}
	unmounted := &zfs.DatasetListEntry{Name: "rpool/arch/ROOT/old", Mountpoint: "legacy"}

	// name
	assert.True(t, datasetMatchesFilter(home, "default"))
	// displayed mountpoint, i.e. the mount path for legacy mounts
	assert.True(t, datasetMatchesFilter(boot, "/boot"))
	assert.True(t, datasetMatchesFilter(unmounted, "legacy"))
	// glob, '*' also matches across '/'
	assert.True(t, datasetMatchesFilter(home, "rpool*home"))
	assert.True(t, datasetMatchesFilter(home, "rpool/*/home"))
	assert.False(t, datasetMatchesFilter(boot, "rpool*"))
}

func TestDatasetBrowser_Filter(t *testing.T) {
	setListDatasets(t, func() ([]*zfs.DatasetListEntry, error) {
		return []*zfs.DatasetListEntry{
			{Name: "rpool/ROOT", Mountpoint: "/", MountPath: "/"},
			{Name: "rpool/home", Mountpoint: "/home", MountPath: "/home"},
			{Name: "rpool/var/log", Mountpoint: "/var/log", MountPath: "/var/log"},
			{Name: "rpool/var/lib/docker", Mountpoint: "/var/lib/docker", MountPath: "/var/lib/docker"},
			{Name: "rpool/var/unmounted", Mountpoint: "legacy"},
		}, nil
	})

	app, browser, screen := newBrowserApp(t)
	pressKey := func(key tcell.Key, r rune) {
		screen.InjectKey(key, r, tcell.ModNone)
	}
	state := func() (visible []string, selected string, footer string, hiding bool) {
		onUiThread(t, app, func() {
			visible = names(browser.tableContainer.GetEntries())
			if entry := browser.tableContainer.GetSelectedEntry(); entry != nil {
				selected = entry.Name
			}
			footer = tview.NewTextView().SetDynamicColors(true).SetText(browser.tableContainer.GetFooter()).GetText(true)
			hiding = browser.IsHidingUnmounted()
		})
		return
	}
	waitForFooter := func(expected string) {
		assert.Eventually(t, func() bool {
			_, _, footer, _ := state()
			return footer == expected
		}, 2*time.Second, 10*time.Millisecond, "expected footer %q", expected)
	}

	onUiThread(t, app, func() {
		browser.SetPath("/home", false)
		browser.Refresh(false)
	})
	waitForFooter("4 of 5 datasets · 1 unmounted hidden")

	// type a filter: matches names and mountpoints, '*' across '/'
	pressKey(tcell.KeyCtrlF, 0)
	for _, r := range "*/var/*" {
		pressKey(tcell.KeyRune, r)
	}
	waitForFooter("2 of 5 datasets · 1 unmounted hidden")
	visible, selected, _, _ := state()
	assert.ElementsMatch(t, []string{"rpool/var/log", "rpool/var/lib/docker"}, visible)
	// the previously selected dataset is hidden, so the first match is selected
	assert.Contains(t, visible, selected)

	// clear the line (ctrl+u), then 'u' is typed into the filter instead of toggling unmounted datasets.
	// It only matches the (hidden) unmounted dataset.
	pressKey(tcell.KeyCtrlU, 0)
	pressKey(tcell.KeyRune, 'u')
	waitForFooter("0 of 5 datasets · 1 unmounted hidden")
	_, _, _, hiding := state()
	assert.True(t, hiding)

	// keep the filter, then show unmounted datasets: the filter applies to them as well
	pressKey(tcell.KeyEnter, 0)
	pressKey(tcell.KeyRune, 'u')
	waitForFooter("1 of 5 datasets")
	visible, _, _, _ = state()
	assert.Equal(t, []string{"rpool/var/unmounted"}, visible)

	// esc clears the filter
	pressKey(tcell.KeyEscape, 0)
	waitForFooter("5 datasets")
}

func TestDatasetBrowser_F2ConfiguresColumns(t *testing.T) {
	setListDatasets(t, func() ([]*zfs.DatasetListEntry, error) {
		return []*zfs.DatasetListEntry{
			{Name: "rpool/a", Used: 30, MountPath: "/a"},
			{Name: "rpool/b", Used: 10, MountPath: "/b"},
		}, nil
	})

	app, browser, screen := newBrowserApp(t)
	onUiThread(t, app, func() {
		browser.Refresh(false)
		browser.Focus()
	})
	assert.Eventually(t, func() bool {
		var count int
		onUiThread(t, app, func() { count = len(browser.tableContainer.GetEntries()) })
		return count == 2
	}, 2*time.Second, 10*time.Millisecond)

	pressKey := func(key tcell.Key) {
		screen.InjectKey(key, 0, tcell.ModNone)
		onUiThread(t, app, func() {})
		time.Sleep(20 * time.Millisecond)
	}
	dialogOpen := func() bool {
		var open bool
		onUiThread(t, app, func() { open = browser.layout.HasPage(string(dialog.ColumnSelectionDialogPage)) })
		return open
	}

	pressKey(tcell.KeyF2)
	require.True(t, dialogOpen())

	// the first active column (name) is selected; removing it applies immediately
	pressKey(tcell.KeyDelete)
	var columns []*table.Column
	onUiThread(t, app, func() { columns = browser.tableContainer.GetColumnSpec() })
	assert.Equal(t, tableColumns[1:], columns)

	pressKey(tcell.KeyEscape)
	assert.False(t, dialogOpen())
	onUiThread(t, app, func() { columns = browser.tableContainer.GetColumnSpec() })
	assert.Equal(t, tableColumns[1:], columns)
}

func TestDatasetBrowser_ShortcutMapContainsColumns(t *testing.T) {
	browser := NewDatasetBrowser(tview.NewApplication())
	assert.Contains(t, browser.GetShortcutMap(), uiutil.TableComponentShortcutColumns)
}

func TestDatasetBrowser_ColumnLayoutIsSavedAndRestored(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	store := state.Load(path)
	state.Current = store
	t.Cleanup(func() {
		_ = store.Flush()
		state.Current = nil
	})
	setListDatasets(t, func() ([]*zfs.DatasetListEntry, error) {
		return []*zfs.DatasetListEntry{{Name: "rpool/a", MountPath: "/a"}}, nil
	})

	app, browser, screen := newBrowserApp(t)
	onUiThread(t, app, func() { browser.Focus() })
	pressKey := func(key tcell.Key, r rune) {
		screen.InjectKey(key, r, tcell.ModNone)
		onUiThread(t, app, func() {})
		time.Sleep(20 * time.Millisecond)
	}

	// remove the first column (name)
	pressKey(tcell.KeyF2, 0)
	pressKey(tcell.KeyDelete, 0)
	pressKey(tcell.KeyEscape, 0)
	require.NoError(t, store.Flush())

	// a new browser (e.g. after a restart) restores the layout from the file
	state.Current = state.Load(path)
	restored := NewDatasetBrowser(tview.NewApplication())
	assert.Equal(t, tableColumns[1:], restored.tableContainer.GetColumnSpec())
	state.Current = store

	// reset in the dialog restores the default columns and removes the saved layout
	pressKey(tcell.KeyF2, 0)
	pressKey(tcell.KeyRune, 'r')
	pressKey(tcell.KeyEscape, 0)
	var columns []*table.Column
	onUiThread(t, app, func() { columns = browser.tableContainer.GetColumnSpec() })
	assert.Equal(t, tableColumns, columns)
	_, saved := store.TableLayout("datasetBrowser")
	assert.False(t, saved)
}
