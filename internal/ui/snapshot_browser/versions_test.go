package snapshot_browser

import (
	"path/filepath"
	"testing"
	"time"
	"zfs-file-history/internal/data"
	"zfs-file-history/internal/data/diff_state"
	"zfs-file-history/internal/folder_listing"
	"zfs-file-history/internal/state"
	"zfs-file-history/internal/testutil"
	"zfs-file-history/internal/ui/table"
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
	browser.entryVersions = &entryVersionStarts{path: "/pool/docs/a.txt", changes: map[string]data.VersionChange{
		"s1": {Kind: data.VersionUnchanged}, "s2": {Kind: data.VersionModified, SizeDelta: 5},
	}}
	assert.Equal(t, [2]bool{false, true}, check(s1))
	assert.Equal(t, [2]bool{true, true}, check(s2))
	// versions of another entry do not count
	browser.entryVersions.path = "/pool/docs/b.txt"
	assert.Equal(t, [2]bool{false, false}, check(s2))

	// unchanged rows are dimmed, and so are unknown ones (so the rows do not flash while moving through the files)
	assert.Equal(t, theme.Colors.SnapshotBrowser.Table.Unchanged, browser.rowColor(&data.SnapshotBrowserEntry{Snapshot: s2}))
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
		browser.setEntryVersions(&entryVersionStarts{path: "/pool/a.txt", changes: map[string]data.VersionChange{
			"s1": {Kind: data.VersionInitial}, "s2": {Kind: data.VersionUnchanged}, "s3": {Kind: data.VersionDeleted},
		}})
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

// Regression: the Diff column flashed "?" while the state of a snapshot was determined.
func TestDiffColumnWhileLoading(t *testing.T) {
	browser := NewSnapshotBrowser(nil)
	text := func(entry *data.SnapshotBrowserEntry) string {
		return browser.createSnapshotBrowserTableCells(0, []*table.Column{columnDiff}, entry)[0].Text
	}
	snapshot := &zfs.Snapshot{Name: "s1"}
	assert.Equal(t, "", text(&data.SnapshotBrowserEntry{Snapshot: snapshot, DiffState: diff_state.Unknown, IsLoading: true}))
	assert.Equal(t, "?", text(&data.SnapshotBrowserEntry{Snapshot: snapshot, DiffState: diff_state.Unknown}))
	assert.Equal(t, "≠", text(&data.SnapshotBrowserEntry{Snapshot: snapshot, DiffState: diff_state.Modified, IsLoading: true}))
}

// The Change column: what happened to the selected entry since the previous snapshot.
func TestChangeColumn(t *testing.T) {
	browser := NewSnapshotBrowser(nil)
	file := &data.FileBrowserEntry{Name: "a.txt", Type: data.File, RealFile: &data.RealFile{Name: "a.txt", Path: "/pool/a.txt"}}
	browser.currentFileEntry = file
	browser.entryVersions = &entryVersionStarts{path: "/pool/a.txt", changes: map[string]data.VersionChange{
		"initial":   {Kind: data.VersionInitial},
		"created":   {Kind: data.VersionCreated, SizeDelta: 10},
		"same":      {Kind: data.VersionUnchanged},
		"grown":     {Kind: data.VersionModified, SizeDelta: 1536},
		"shrunk":    {Kind: data.VersionModified, SizeDelta: -2048},
		"touched":   {Kind: data.VersionModified},
		"deleted":   {Kind: data.VersionDeleted, SizeDelta: -10},
		"something": {Kind: data.VersionUnchanged},
	}}
	text := func(name string) string {
		return testutil.StripTags(browser.formatEntryChange(&zfs.Snapshot{Name: name}))
	}
	assert.Equal(t, "initial", text("initial"))
	assert.Equal(t, "+", text("created"))
	assert.Equal(t, "", text("same"))
	assert.Equal(t, "≠ +1.5 KiB", text("grown"))
	assert.Equal(t, "≠ -2.0 KiB", text("shrunk"))
	assert.Equal(t, "≠", text("touched"), "same size, e.g. only the modification time")
	assert.Equal(t, "−", text("deleted"))
	assert.Equal(t, "", text("unknown"), "not determined yet")

	// a folder: the change of the number of items
	browser.currentFileEntry = &data.FileBrowserEntry{Name: "docs", Type: data.Directory, RealFile: &data.RealFile{Name: "docs", Path: "/pool/a.txt"}}
	assert.Equal(t, "≠ -2048 items", text("shrunk"))
	browser.entryVersions.changes["one"] = data.VersionChange{Kind: data.VersionModified, SizeDelta: 1}
	assert.Equal(t, "≠ +1 item", text("one"))
	browser.currentFileEntry = file

	// sorted by how much changed, the largest first; unchanged and unknown last
	var entries []*data.SnapshotBrowserEntry
	for _, name := range []string{"same", "unknown", "grown", "created", "shrunk", "initial"} {
		entries = append(entries, &data.SnapshotBrowserEntry{Snapshot: &zfs.Snapshot{Name: name}})
	}
	browser.sortEntries(entries, columnChange, false)
	assert.Equal(t, []string{"shrunk", "grown", "created", "initial", "same", "unknown"}, snapshotNames(entries))
}

// The headers say what is compared: the selected entry with now, the folder only where it says so.
func TestColumnTitles(t *testing.T) {
	assert.Equal(t, "vs now", columnDiff.Title)
	assert.Equal(t, "Change", columnChange.Title)
	assert.Equal(t, "Folder vs now", columnVsNow.Title)
	assert.Equal(t, "Folder changes", columnChanges.Title)
}
