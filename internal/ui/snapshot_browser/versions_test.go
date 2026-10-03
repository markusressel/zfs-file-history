package snapshot_browser

import (
	"path/filepath"
	"testing"
	"time"
	"zfs-file-history/internal/data"
	"zfs-file-history/internal/folder_listing"
	"zfs-file-history/internal/state"
	"zfs-file-history/internal/testutil"
	"zfs-file-history/internal/ui/theme"
	"zfs-file-history/internal/zfs"

	"github.com/gdamore/tcell/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// What changed in a snapshot: the selected entry, else the folder, else the dataset.
func TestChangedInSnapshot(t *testing.T) {
	browser := NewSnapshotBrowser(nil)
	s1 := &zfs.Snapshot{Name: "s1", Properties: zfs.SnapshotProperties{Written: 0}}
	s2 := &zfs.Snapshot{Name: "s2", Properties: zfs.SnapshotProperties{Written: 100}}
	check := func(snapshot *zfs.Snapshot) [2]bool {
		changed, known := browser.changedInSnapshot(snapshot)
		return [2]bool{changed, known}
	}

	// the dataset: data was written
	assert.Equal(t, [2]bool{false, true}, check(s1))
	assert.Equal(t, [2]bool{true, true}, check(s2))

	// the folder, once it is compared
	browser.path = "/pool/docs"
	browser.folderChanges = &folderChanges{folderPath: "/pool/docs", bySnapshot: map[string]folder_listing.SnapshotChanges{
		"s1": {Exists: true, Initial: true},
		"s2": {Exists: true},
	}}
	assert.Equal(t, [2]bool{true, true}, check(s1), "the oldest snapshot with the folder")
	assert.Equal(t, [2]bool{false, true}, check(s2), "nothing changed in the folder, although data was written")

	// the selected entry: unknown until its versions are determined
	browser.currentFileEntry = &data.FileBrowserEntry{Name: "a.txt", RealFile: &data.RealFile{Name: "a.txt", Path: "/pool/docs/a.txt"}}
	assert.Equal(t, [2]bool{false, false}, check(s1))
	browser.entryVersions = &entryVersionStarts{path: "/pool/docs/a.txt", newVersion: map[string]bool{"s1": false, "s2": true}}
	assert.Equal(t, [2]bool{false, true}, check(s1))
	assert.Equal(t, [2]bool{true, true}, check(s2))
	// versions of another entry do not count
	browser.entryVersions.path = "/pool/docs/b.txt"
	assert.Equal(t, [2]bool{false, false}, check(s2))

	// unchanged rows are dimmed, unknown ones are not
	assert.Equal(t, theme.Colors.SnapshotBrowser.Table.Changed, browser.rowColor(&data.SnapshotBrowserEntry{Snapshot: s1}))
	browser.entryVersions.path = "/pool/docs/a.txt"
	assert.Equal(t, theme.Colors.SnapshotBrowser.Table.Unchanged, browser.rowColor(&data.SnapshotBrowserEntry{Snapshot: s1}))
	assert.Equal(t, theme.Colors.SnapshotBrowser.Table.Changed, browser.rowColor(&data.SnapshotBrowserEntry{Snapshot: s2}))
}

// v shows only the snapshots in which something changed, per page and remembered.
func TestOnlyChanges(t *testing.T) {
	store := state.Load(filepath.Join(t.TempDir(), "state.json"))
	state.Current = store
	t.Cleanup(func() {
		_ = store.Flush()
		state.Current = nil
	})
	browser, app := startBrowser(t)
	var entries []*data.SnapshotBrowserEntry
	for _, name := range []string{"s1", "s2", "s3"} {
		entries = append(entries, &data.SnapshotBrowserEntry{Snapshot: &zfs.Snapshot{Name: name}})
	}
	shown := func() []string {
		var names []string
		testutil.OnUiThread(t, app, func() { names = snapshotNames(browser.tableContainer.GetEntries()) })
		return names
	}
	testutil.OnUiThread(t, app, func() {
		browser.UseColumnLayout(FilesLayout)
		browser.currentFileEntry = &data.FileBrowserEntry{Name: "a.txt", RealFile: &data.RealFile{Name: "a.txt", Path: "/pool/a.txt"}}
		browser.tableContainer.SetData(entries)
		browser.tableContainer.SelectFirstIfExists()
	})

	// while the versions are determined, all are shown
	testutil.OnUiThread(t, app, func() { browser.toggleOnlyChanges() })
	assert.ElementsMatch(t, []string{"s1", "s2", "s3"}, shown())
	assert.True(t, store.Toggle(FilesLayout.stateKey+".onlyChanges", false), "remembered for the page")

	testutil.OnUiThread(t, app, func() {
		browser.setEntryVersions(&entryVersionStarts{path: "/pool/a.txt", newVersion: map[string]bool{"s1": true, "s2": false, "s3": true}})
	})
	assert.ElementsMatch(t, []string{"s1", "s3"}, shown())
	var footer string
	var shortcut string
	testutil.OnUiThread(t, app, func() {
		footer = browser.tableContainer.GetFooter()
		shortcut = browser.onlyChangesShortcut().Name
	})
	assert.Equal(t, "2 of 3 snapshots", footer)
	assert.Equal(t, "All snapshots", shortcut)

	// back to all
	testutil.OnUiThread(t, app, func() { browser.toggleOnlyChanges() })
	assert.ElementsMatch(t, []string{"s1", "s2", "s3"}, shown())
	assert.False(t, store.Toggle(FilesLayout.stateKey+".onlyChanges", true))

	// the setting is loaded with the layout of the page, the other page has its own
	testutil.OnUiThread(t, app, func() { browser.toggleOnlyChanges() })
	restored := NewSnapshotBrowser(app)
	restored.UseColumnLayout(FilesLayout)
	assert.True(t, restored.onlyChanges)
	other := NewSnapshotBrowser(app)
	other.UseColumnLayout(DatasetsLayout)
	assert.False(t, other.onlyChanges)
}

// The key toggles it.
func TestOnlyChangesKey(t *testing.T) {
	browser, app := startBrowser(t)
	testutil.OnUiThread(t, app, func() {
		browser.tableContainer.SetData([]*data.SnapshotBrowserEntry{{Snapshot: &zfs.Snapshot{Name: "s1"}}})
		browser.tableContainer.SelectFirstIfExists()
		app.SetFocus(browser.tableContainer.GetLayout())
	})
	app.QueueEvent(tcell.NewEventKey(tcell.KeyRune, 'v', tcell.ModNone))
	require.Eventually(t, func() bool {
		var onlyChanges bool
		testutil.OnUiThread(t, app, func() { onlyChanges = browser.onlyChanges })
		return onlyChanges
	}, 2*time.Second, 10*time.Millisecond)
}
