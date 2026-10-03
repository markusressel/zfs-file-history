package snapshot_browser

import (
	"context"
	"io/fs"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
	"zfs-file-history/internal/data"
	"zfs-file-history/internal/folder_listing"
	"zfs-file-history/internal/state"
	"zfs-file-history/internal/ui/table"
	uiutil "zfs-file-history/internal/ui/util"
	"zfs-file-history/internal/zfs"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// folderChangesStub records the comparisons and the folders read as they are now.
type folderChangesStub struct {
	mu          sync.Mutex
	compared    []folder_listing.Listing
	readFolders []string
}

func stubFolderChanges(t *testing.T, result map[string]folder_listing.SnapshotChanges) *folderChangesStub {
	stub := &folderChangesStub{}
	originalCompare, originalRead := compareSnapshots, readWorkingCopy
	t.Cleanup(func() { compareSnapshots, readWorkingCopy = originalCompare, originalRead })
	compareSnapshots = func(ctx context.Context, folderPath string, snapshots []*zfs.Snapshot, workingCopy folder_listing.Listing) (map[string]folder_listing.SnapshotChanges, error) {
		stub.mu.Lock()
		defer stub.mu.Unlock()
		stub.compared = append(stub.compared, workingCopy)
		return result, nil
	}
	readWorkingCopy = func(path string) folder_listing.Listing {
		stub.mu.Lock()
		defer stub.mu.Unlock()
		stub.readFolders = append(stub.readFolders, path)
		return folder_listing.Listing{Exists: true, Entries: map[string]folder_listing.Entry{"read": {Name: "read"}}}
	}
	return stub
}

func (stub *folderChangesStub) counts() (compared int, read int) {
	stub.mu.Lock()
	defer stub.mu.Unlock()
	return len(stub.compared), len(stub.readFolders)
}

func startBrowser(t *testing.T) (*SnapshotBrowserComponent, *tview.Application) {
	app := tview.NewApplication()
	app.SetScreen(tcell.NewSimulationScreen("UTF-8"))
	browser := NewSnapshotBrowser(app)
	app.SetRoot(browser.GetLayout(), true)
	go func() { _ = app.Run() }()
	t.Cleanup(app.Stop)
	return browser, app
}

func TestSnapshotBrowser_FolderChanges(t *testing.T) {
	result := map[string]folder_listing.SnapshotChanges{
		"s1": {Exists: true, Initial: true, VsNow: folder_listing.Counts{Added: 2, Deleted: 1}},
		"s2": {Exists: true, VsNow: folder_listing.Counts{}, VsPrevious: folder_listing.Counts{Modified: 4}},
	}
	stub := stubFolderChanges(t, result)
	browser, app := startBrowser(t)
	loaded := make(chan FolderChangesLoaded, 10)
	snapshots := []*zfs.Snapshot{{Name: "s1"}, {Name: "s2"}}
	onUiThread(t, app, func() {
		browser.Events.Subscribe(func(event Event) {
			if e, ok := event.(FolderChangesLoaded); ok {
				loaded <- e
			}
		})
		browser.path = "/pool/docs"
		browser.currentSnapshots = snapshots
		// shown by default on the files page, computed once the folder stays the same for a moment
		browser.tableContainer.SetActiveColumns([]*table.Column{columnName, columnVsNow, columnChanges})
	})
	select {
	case event := <-loaded:
		assert.Equal(t, "/pool/docs", event.FolderPath)
		assert.Equal(t, result, event.BySnapshot)
	case <-time.After(3 * time.Second):
		t.Fatal("no FolderChangesLoaded")
	}
	compared, read := stub.counts()
	assert.Equal(t, 1, compared)
	assert.Equal(t, 1, read, "without the entries of a file browser, the folder is read as it is now")

	var cells []string
	onUiThread(t, app, func() {
		for _, snapshot := range snapshots {
			for _, column := range []*table.Column{columnVsNow, columnChanges} {
				cells = append(cells, stripTags(browser.formatFolderChanges(snapshot, column == columnVsNow)))
			}
		}
	})
	assert.Equal(t, []string{"+2 −1", "initial", "=", "~4"}, cells)

	// the entries of the file browser: compared again, without reading the folder
	now := folder_listing.Listing{Exists: true, Entries: map[string]folder_listing.Entry{"a": {Name: "a"}}}
	onUiThread(t, app, func() { browser.SetWorkingCopy("/pool/docs", now) })
	require.Eventually(t, func() bool { compared, _ := stub.counts(); return compared == 2 }, 3*time.Second, 10*time.Millisecond)
	_, read = stub.counts()
	assert.Equal(t, 1, read)

	// nothing changed (e.g. another entry was selected): not compared again
	onUiThread(t, app, func() {
		browser.SetWorkingCopy("/pool/docs", now)
		browser.updateFolderChanges()
	})
	time.Sleep(2 * folderChangesDelay)
	compared, _ = stub.counts()
	assert.Equal(t, 2, compared)
}

// Without the columns and listeners that need them (e.g. on the dataset page), the folder is not compared.
func TestSnapshotBrowser_FolderChangesOnlyWhenWanted(t *testing.T) {
	stub := stubFolderChanges(t, nil)
	browser, app := startBrowser(t)
	onUiThread(t, app, func() {
		browser.tableContainer.SetActiveColumns(DatasetsLayout.columns)
		browser.path = "/pool/docs"
		browser.currentSnapshots = []*zfs.Snapshot{{Name: "s1"}}
		browser.updateFolderChanges()
	})
	time.Sleep(2 * folderChangesDelay)
	compared, _ := stub.counts()
	assert.Zero(t, compared)

	// a listener needs them
	onUiThread(t, app, func() { browser.RequireFolderChanges() })
	require.Eventually(t, func() bool { compared, _ := stub.counts(); return compared == 1 }, 3*time.Second, 10*time.Millisecond)
}

func stripTags(text string) string {
	return tview.NewTextView().SetDynamicColors(true).SetText(text).GetText(true)
}

// fileInfo is a file as returned by os.Lstat.
type fileInfo struct {
	size    int64
	modTime time.Time
	dir     bool
}

func (f fileInfo) Name() string       { return "x" }
func (f fileInfo) Size() int64        { return f.size }
func (f fileInfo) Mode() fs.FileMode  { return 0o644 }
func (f fileInfo) ModTime() time.Time { return f.modTime }
func (f fileInfo) IsDir() bool        { return f.dir }
func (f fileInfo) Sys() any           { return nil }

// Size and Modified are the selected entry in the snapshot: which version it holds.
func TestEntryColumns(t *testing.T) {
	uiutil.InitTimeFormat()
	modified := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	file := &data.SnapshotBrowserEntry{HasEntryInfo: true, EntryInfo: fileInfo{size: 2048, modTime: modified}}
	missing := &data.SnapshotBrowserEntry{HasEntryInfo: true}
	folder := &data.SnapshotBrowserEntry{HasEntryInfo: true, EntryInfo: fileInfo{modTime: modified, dir: true}}
	none := &data.SnapshotBrowserEntry{}

	text := func(format func(*data.SnapshotBrowserEntry, tcell.Color) (string, tcell.Color), entry *data.SnapshotBrowserEntry) string {
		result, _ := format(entry, tcell.ColorWhite)
		return result
	}
	assert.Equal(t, uiutil.StableLengthHumanizedBytes(2048), text(formatEntrySize, file))
	assert.Equal(t, "—", text(formatEntrySize, missing), "not in the snapshot")
	assert.Equal(t, "", text(formatEntrySize, folder), "the size of a folder says nothing")
	assert.Equal(t, "", text(formatEntrySize, none), "no selected entry")
	assert.Equal(t, uiutil.FormatTime(modified), text(formatEntryModified, file))
	assert.Equal(t, uiutil.FormatTime(modified), text(formatEntryModified, folder))
	assert.Equal(t, "—", text(formatEntryModified, missing))

	// sorted by size, the largest first; unknown last
	entries := []*data.SnapshotBrowserEntry{
		{Snapshot: &zfs.Snapshot{Name: "missing"}, HasEntryInfo: true},
		{Snapshot: &zfs.Snapshot{Name: "small"}, HasEntryInfo: true, EntryInfo: fileInfo{size: 1}},
		{Snapshot: &zfs.Snapshot{Name: "big"}, HasEntryInfo: true, EntryInfo: fileInfo{size: 9}},
	}
	createSnapshotBrowserTableSortFunction(entries, columnSize, false)
	assert.Equal(t, []string{"big", "small", "missing"}, snapshotNames(entries))
}

func TestSortByFolderChanges(t *testing.T) {
	changes := map[string]folder_listing.SnapshotChanges{
		"few":     {Exists: true, VsPrevious: folder_listing.Counts{Added: 1}},
		"many":    {Exists: true, VsPrevious: folder_listing.Counts{Added: 5, Deleted: 2}},
		"none":    {Exists: true},
		"missing": {},
	}
	changesOf := func(snapshot *zfs.Snapshot) (folder_listing.SnapshotChanges, bool) {
		result, ok := changes[snapshot.Name]
		return result, ok
	}
	var entries []*data.SnapshotBrowserEntry
	for _, name := range []string{"few", "missing", "many", "none"} {
		entries = append(entries, &data.SnapshotBrowserEntry{Snapshot: &zfs.Snapshot{Name: name}})
	}
	sortSnapshotEntries(entries, columnChanges, true, changesOf)
	assert.Equal(t, []string{"many", "few", "none", "missing"}, snapshotNames(entries))
}

// Each page has its own columns; a layout saved before (shared by both pages) is taken over by both.
func TestUseColumnLayout(t *testing.T) {
	store := state.Load(filepath.Join(t.TempDir(), "state.json"))
	state.Current = store
	t.Cleanup(func() {
		_ = store.Flush()
		state.Current = nil
	})
	keys := func(browser *SnapshotBrowserComponent) string {
		var result []string
		for _, column := range browser.tableContainer.GetColumnSpec() {
			result = append(result, column.Key)
		}
		return strings.Join(result, ",")
	}

	files := NewSnapshotBrowser(tview.NewApplication())
	files.UseColumnLayout(FilesLayout)
	datasets := NewSnapshotBrowser(tview.NewApplication())
	datasets.UseColumnLayout(DatasetsLayout)
	assert.Equal(t, "name,diff,creation,entrySize,entryModified,folderChanges,holds", keys(files))
	assert.Equal(t, "name,creation,used,written,referenced,holds", keys(datasets))

	// a change on one page does not change the other
	files.tableContainer.SetActiveColumns([]*table.Column{columnName, columnUsed})
	datasets = NewSnapshotBrowser(tview.NewApplication())
	datasets.UseColumnLayout(DatasetsLayout)
	assert.Equal(t, "name,creation,used,written,referenced,holds", keys(datasets))

	// taken over from before the pages had their own
	legacy := state.Load(filepath.Join(t.TempDir(), "legacy.json"))
	state.Current = legacy
	legacy.SetTableLayout(legacyLayoutStateKey, state.TableLayout{Columns: []string{"name", "holds"}, SortColumn: "name"})
	for _, layout := range []ColumnLayout{FilesLayout, DatasetsLayout} {
		browser := NewSnapshotBrowser(tview.NewApplication())
		browser.UseColumnLayout(layout)
		assert.Equal(t, "name,holds", keys(browser))
	}
	_ = legacy.Flush()
}
