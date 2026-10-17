package file_browser

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
	"zfs-file-history/internal/data"
	"zfs-file-history/internal/data/diff_state"
	"zfs-file-history/internal/folder_listing"
	"zfs-file-history/internal/testutil"
	"zfs-file-history/internal/zfs"

	"github.com/gdamore/tcell/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// allSnapshotsTest is a fileBrowserTest with the snapshots s1 (gone.txt, lost.txt) and s2 (gone.txt), newest last,
// known as the snapshot browser would know them.
type allSnapshotsTest struct {
	*fileBrowserTest
	s1, s2 *zfs.Snapshot
	// loads counts the EntriesLoadedEvents
	loads chan string
}

func newAllSnapshotsTest(t *testing.T) *allSnapshotsTest {
	ft := newFileBrowserTest(t, "")
	at := &allSnapshotsTest{fileBrowserTest: ft, loads: make(chan string, 100)}
	at.s1 = at.snapshot("s1", 1, "gone.txt", "lost.txt")
	at.s2 = at.snapshot("s2", 2, "gone.txt")
	testutil.Replace(t, &loadSnapshotsOfPath, func(string) ([]*zfs.Snapshot, error) {
		panic("the snapshots are known")
	})
	testutil.OnUiThread(t, ft.app, func() {
		ft.fileBrowser.SetSnapshotsFunc(func(string) (string, []*zfs.Snapshot, bool) {
			return ft.dir, []*zfs.Snapshot{at.s2, at.s1}, true // in any order
		})
	})
	ft.fileBrowser.Events.Subscribe(func(event Event) {
		if e, ok := event.(EntriesLoadedEvent); ok {
			at.loads <- e.Path
		}
	})
	return at
}

// snapshot creates the snapshot directory with the given files, created day days after fileTime.
func (at *allSnapshotsTest) snapshot(name string, day int, files ...string) *zfs.Snapshot {
	base := filepath.Join(at.dir, ".zfs", "snapshot", name)
	for _, file := range files {
		require.NoError(at.t, os.MkdirAll(base, 0o755))
		require.NoError(at.t, os.WriteFile(filepath.Join(base, file), []byte(name), 0o644))
	}
	dataset := &zfs.Dataset{Path: at.dir, HiddenZfsPath: filepath.Join(at.dir, ".zfs")}
	return &zfs.Snapshot{Name: name, Path: base, ParentDataset: dataset,
		Properties: zfs.SnapshotProperties{CreationDate: time.Date(2026, 1, day, 0, 0, 0, 0, time.UTC)}}
}

func (at *allSnapshotsTest) entry(name string) (found *data.FileBrowserEntry) {
	testutil.OnUiThread(at.t, at.app, func() {
		for _, entry := range at.fileBrowser.tableContainer.GetAllEntries() {
			if entry.Name == name {
				found = entry
			}
		}
	})
	return found
}

// loadCount returns the number of loads since the last call.
func (at *allSnapshotsTest) loadCount() int {
	count := 0
	for {
		select {
		case <-at.loads:
			count++
		default:
			return count
		}
	}
}

// Entries that exist neither now nor in the selected snapshot, but in other snapshots, are listed by default, with
// their version in the newest snapshot that contains them. Hidden (a), they are counted in the footer.
func TestFileBrowser_ShowAllSnapshots(t *testing.T) {
	at := newAllSnapshotsTest(t)
	testutil.OnUiThread(t, at.app, func() {
		at.fileBrowser.SetPath(at.dir, true)
		at.fileBrowser.SetSelectedSnapshot(&data.SnapshotBrowserEntry{Snapshot: at.s2})
	})
	s := at.waitFor("shown by default", func(s fileBrowserState) bool { return s.footer == "6 entries · 1 deleted (all snapshots)" })
	assert.Equal(t, []string{"a.txt", "b.txt", "c.log", "gone.txt", "lost.txt", "sub"}, s.visible)
	lost := at.entry("lost.txt")
	require.NotNil(t, lost)
	assert.Equal(t, []*zfs.Snapshot{at.s1}, lost.OnlyInSnapshots)
	assert.Same(t, at.s1, lost.SnapshotFiles[0].Snapshot, "restored from the snapshot that contains it")
	assert.Equal(t, filepath.Join(at.dir, "lost.txt"), lost.GetRealPath())
	assert.Nil(t, at.entry("gone.txt").OnlyInSnapshots, "in the selected snapshot: listed as before")
	require.Eventually(t, func() bool {
		var state diff_state.DiffState
		testutil.OnUiThread(t, at.app, func() { state = lost.DiffState })
		return state == diff_state.Deleted
	}, 5*time.Second, 20*time.Millisecond)
	screen := testutil.ScreenText(t, at.app, at.screen)
	assert.Contains(t, screen, "Found in")
	assert.Regexp(t, `lost\.txt +s1 `, screen, "found in s1")

	// another snapshot selected: gone.txt is newest in s2
	testutil.OnUiThread(t, at.app, func() {
		at.fileBrowser.SetSelectedSnapshot(&data.SnapshotBrowserEntry{Snapshot: at.snapshot("s0", 0)})
	})
	at.waitFor("both only in other snapshots", func(s fileBrowserState) bool {
		return s.footer == "6 entries · 2 deleted (all snapshots)"
	})
	assert.Same(t, at.s2, at.entry("gone.txt").SnapshotFiles[0].Snapshot, "the newest snapshot that contains it")
	assert.Regexp(t, `gone\.txt +s2 \+1 `, testutil.ScreenText(t, at.app, at.screen), "found in s2 and one more")

	at.pressKey(tcell.KeyRune, 'a')
	s = at.waitFor("hidden", func(s fileBrowserState) bool { return s.footer == "4 entries" })
	assert.Equal(t, []string{"a.txt", "b.txt", "c.log", "sub"}, s.visible)
	assert.NotContains(t, testutil.ScreenText(t, at.app, at.screen), "Found in", "the column is only drawn with all snapshots")
	// counted by the snapshot browser while they are hidden
	testutil.OnUiThread(t, at.app, func() {
		at.fileBrowser.SetOnlyInSnapshots(at.dir, map[string]folder_listing.SnapshotOnlyEntry{
			"gone.txt": {Snapshots: []*zfs.Snapshot{at.s2, at.s1}},
			"lost.txt": {Snapshots: []*zfs.Snapshot{at.s1}},
		})
	})
	at.waitFor("counted", func(s fileBrowserState) bool { return s.footer == "4 entries · 2 deleted hidden (all snapshots)" })

	at.pressKey(tcell.KeyRune, 'a')
	at.waitFor("shown again", func(s fileBrowserState) bool { return s.footer == "6 entries · 2 deleted (all snapshots)" })
}

// Regression: while all snapshots are shown, a folder whose snapshots are searched at once is loaded once with all its
// entries, not first with those that exist and then again with those of the other snapshots.
func TestFileBrowser_ShowAllSnapshots_LoadsOnce(t *testing.T) {
	at := newAllSnapshotsTest(t)
	// the search counts as fast, also on a busy machine
	testutil.Replace(t, &showEntriesWhileSearchingAfter, time.Minute)
	testutil.WaitForUpdates(t, at.app)
	at.loadCount()

	testutil.OnUiThread(t, at.app, func() { at.fileBrowser.SetPath(at.dir, true) })
	s := at.waitFor("loaded", func(s fileBrowserState) bool { return s.path == at.dir && s.footer != "" })
	assert.Equal(t, "6 entries · 2 deleted (all snapshots)", s.footer, "with the entries of the snapshots at once")
	assert.Equal(t, 1, at.loadCount())

	// the same result of the snapshot browser loads nothing again
	same := map[string]folder_listing.SnapshotOnlyEntry{
		"gone.txt": {Snapshots: []*zfs.Snapshot{at.s2, at.s1}},
		"lost.txt": {Snapshots: []*zfs.Snapshot{at.s1}},
	}
	testutil.OnUiThread(t, at.app, func() { at.fileBrowser.SetOnlyInSnapshots(at.dir, same) })
	testutil.WaitForUpdates(t, at.app)
	assert.Equal(t, 0, at.loadCount())

	// one that differs (s1 was destroyed) loads the folder again
	testutil.OnUiThread(t, at.app, func() {
		at.fileBrowser.SetSnapshotsFunc(func(string) (string, []*zfs.Snapshot, bool) { return at.dir, []*zfs.Snapshot{at.s2}, true })
		at.fileBrowser.SetOnlyInSnapshots(at.dir, map[string]folder_listing.SnapshotOnlyEntry{
			"gone.txt": {Snapshots: []*zfs.Snapshot{at.s2}},
		})
	})
	at.waitFor("lost.txt gone", func(s fileBrowserState) bool { return s.footer == "5 entries · 1 deleted (all snapshots)" })
}

// The snapshots are loaded if the snapshot browser does not know them (yet), e.g. in another dataset.
func TestFileBrowser_ShowAllSnapshots_LoadsUnknownSnapshots(t *testing.T) {
	at := newAllSnapshotsTest(t)
	testutil.Replace(t, &loadSnapshotsOfPath, func(path string) ([]*zfs.Snapshot, error) {
		return []*zfs.Snapshot{at.s1}, nil
	})
	testutil.OnUiThread(t, at.app, func() {
		at.fileBrowser.SetSnapshotsFunc(func(string) (string, []*zfs.Snapshot, bool) { return "", nil, false })
		at.fileBrowser.SetPath(at.dir, true)
	})
	at.waitFor("loaded", func(s fileBrowserState) bool { return s.footer == "6 entries · 2 deleted (all snapshots)" })
}

// If searching the snapshots takes long, the entries are shown meanwhile, with the progress in the footer, and those
// found in the snapshots are added once found, keeping the selection.
func TestFileBrowser_ShowAllSnapshots_SlowSearch(t *testing.T) {
	at := newAllSnapshotsTest(t)
	testutil.Replace(t, &showEntriesWhileSearchingAfter, 0)
	release := make(chan struct{})
	testutil.Replace(t, &loadSnapshotsOfPath, func(path string) ([]*zfs.Snapshot, error) {
		<-release
		return []*zfs.Snapshot{at.s1, at.s2}, nil
	})
	testutil.OnUiThread(t, at.app, func() {
		at.fileBrowser.SetSnapshotsFunc(func(string) (string, []*zfs.Snapshot, bool) { return "", nil, false })
		at.fileBrowser.SetPath(at.dir, true)
	})
	at.waitFor("shown while searching", func(s fileBrowserState) bool {
		return strings.HasPrefix(s.footer, "4 entries · ") && strings.HasSuffix(s.footer, " reading snapshots")
	})
	testutil.OnUiThread(t, at.app, func() {
		for _, entry := range at.fileBrowser.tableContainer.GetEntries() {
			if entry.Name == "c.log" {
				at.fileBrowser.tableContainer.Select(entry)
			}
		}
	})
	// the result of the snapshot browser does not start the search again
	at.loadCount()
	testutil.OnUiThread(t, at.app, func() {
		at.fileBrowser.SetOnlyInSnapshots(at.dir, map[string]folder_listing.SnapshotOnlyEntry{
			"lost.txt": {Snapshots: []*zfs.Snapshot{at.s1}},
		})
	})
	testutil.WaitForUpdates(t, at.app)
	assert.Equal(t, 0, at.loadCount())

	close(release)
	at.waitFor("found entries added", func(s fileBrowserState) bool { return s.footer == "6 entries · 2 deleted (all snapshots)" })
	var selected string
	testutil.OnUiThread(t, at.app, func() { selected = at.fileBrowser.GetSelection().Name })
	assert.Equal(t, "c.log", selected, "the selection stays")
}

// Regression: the remembered selection (e.g. a deleted folder after going up from it) was replaced by the entry
// selected in its place while the snapshots were searched, and not selected once it was found.
func TestFileBrowser_ShowAllSnapshots_SlowSearchRestoresSelection(t *testing.T) {
	at := newAllSnapshotsTest(t)
	testutil.Replace(t, &showEntriesWhileSearchingAfter, 0)
	release := make(chan struct{})
	testutil.Replace(t, &loadSnapshotsOfPath, func(path string) ([]*zfs.Snapshot, error) {
		<-release
		return []*zfs.Snapshot{at.s1, at.s2}, nil
	})
	testutil.OnUiThread(t, at.app, func() {
		at.fileBrowser.SetSnapshotsFunc(func(string) (string, []*zfs.Snapshot, bool) { return "", nil, false })
		at.fileBrowser.SetPathWithSelection(at.dir, filepath.Join(at.dir, "lost.txt"))
	})
	at.waitFor("shown while searching", func(s fileBrowserState) bool {
		return strings.HasSuffix(s.footer, " reading snapshots")
	})

	close(release)
	at.waitFor("found entries added", func(s fileBrowserState) bool { return s.footer == "6 entries · 2 deleted (all snapshots)" })
	var selected string
	testutil.OnUiThread(t, at.app, func() { selected = at.fileBrowser.GetSelection().Name })
	assert.Equal(t, "lost.txt", selected)
}

// Regression: right after entering a child dataset, the snapshots of its parent (still shown by the snapshot
// browser) were read in place of its own.
func TestFileBrowser_ShowAllSnapshots_ChildDataset(t *testing.T) {
	at := newAllSnapshotsTest(t)
	require.NoError(t, os.MkdirAll(filepath.Join(at.sub, ".zfs"), 0o755))
	loaded := make(chan string, 10)
	testutil.Replace(t, &loadSnapshotsOfPath, func(path string) ([]*zfs.Snapshot, error) {
		loaded <- path
		return nil, nil
	})
	testutil.OnUiThread(t, at.app, func() { at.fileBrowser.SetPath(at.sub, true) })
	select {
	case path := <-loaded:
		assert.Equal(t, at.sub, path)
	case <-time.After(5 * time.Second):
		t.Fatal("the snapshots of the child dataset were not loaded")
	}
	at.waitFor("loaded", func(s fileBrowserState) bool { return s.footer == "1 entry" })
}

// isInDataset tells a folder of a dataset from one of a child dataset mounted below it.
func TestIsInDataset(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "child", ".zfs"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "child", "folder"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "plain", "folder"), 0o755))

	assert.True(t, isInDataset(root, root))
	assert.True(t, isInDataset(filepath.Join(root, "plain", "folder"), root))
	assert.False(t, isInDataset(filepath.Join(root, "child"), root), "the root of a child dataset")
	assert.False(t, isInDataset(filepath.Join(root, "child", "folder"), root), "in a child dataset")
	assert.True(t, isInDataset(filepath.Join(root, "child", "folder"), filepath.Join(root, "child")))
	assert.False(t, isInDataset(root+"-other", root), "another folder with the same prefix")
}

func TestFormatSearchProgress(t *testing.T) {
	assert.Equal(t, "⣾ reading snapshots 40/240", testutil.StripTags(formatSearchProgress("⣾", &snapshotSearchProgress{done: 40, total: 240}).Text))
	assert.Equal(t, "⣾ reading snapshots", testutil.StripTags(formatSearchProgress("⣾", &snapshotSearchProgress{}).Text), "while the snapshots are loaded")
}

// The entries counted by the snapshot browser for another folder are not counted.
func TestFileBrowser_ShowAllSnapshots_OtherFolder(t *testing.T) {
	ft := newFileBrowserTest(t, "")
	testutil.OnUiThread(t, ft.app, func() {
		ft.fileBrowser.SetPath(ft.dir, true)
		ft.fileBrowser.SetOnlyInSnapshots(ft.sub, map[string]folder_listing.SnapshotOnlyEntry{
			"gone.txt": {Snapshots: []*zfs.Snapshot{{Name: "s1"}}},
		})
	})
	ft.waitFor("loaded", func(s fileBrowserState) bool { return s.footer == "4 entries" })
	testutil.WaitForUpdates(t, ft.app)
	assert.Equal(t, "4 entries", ft.state().footer)
}

// Snapshots never change: a folder is read once in each, not again for each snapshot selected.
func TestSnapshotListingCache(t *testing.T) {
	at := newAllSnapshotsTest(t)
	var cache snapshotListingCache
	listings, err := cache.read(context.Background(), at.dir, []*zfs.Snapshot{at.s1, at.s2}, nil)
	require.NoError(t, err)
	assert.Len(t, listings[0].Entries, 2)
	assert.Len(t, listings[1].Entries, 1)

	require.NoError(t, os.RemoveAll(filepath.Join(at.dir, ".zfs")))
	listings, err = cache.read(context.Background(), at.dir, []*zfs.Snapshot{at.s2, at.s1}, nil)
	require.NoError(t, err)
	assert.Len(t, listings[0].Entries, 1, "from the cache, in the order asked for")
	assert.Len(t, listings[1].Entries, 2)

	listings, err = cache.read(context.Background(), at.sub, []*zfs.Snapshot{at.s1}, nil)
	require.NoError(t, err)
	assert.False(t, listings[0].Exists, "another folder is read")
}

// Regression: a read of another folder while a folder was read replaced the cache, and the snapshots the first read
// had taken from the cache were looked up in the listings of the other folder.
func TestSnapshotListingCache_OtherFolderReadMeanwhile(t *testing.T) {
	at := newAllSnapshotsTest(t)
	var cache snapshotListingCache
	_, err := cache.read(context.Background(), at.dir, []*zfs.Snapshot{at.s1}, nil)
	require.NoError(t, err)

	readOther := func(int) {
		if _, err := cache.read(context.Background(), at.sub, []*zfs.Snapshot{at.s1}, nil); err != nil {
			t.Error(err)
		}
	}
	// the first call comes before the read, the others from the readers
	var once sync.Once
	listings, err := cache.read(context.Background(), at.dir, []*zfs.Snapshot{at.s1, at.s2}, func(done int) {
		once.Do(func() { readOther(done) })
	})
	require.NoError(t, err)
	assert.Len(t, listings[0].Entries, 2, "s1 from the cache, of this folder")
	assert.Len(t, listings[1].Entries, 1)
}

func TestFormatAndCompareFoundIn(t *testing.T) {
	older := &zfs.Snapshot{Name: "daily-10-01", Properties: zfs.SnapshotProperties{CreationDate: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)}}
	newer := &zfs.Snapshot{Name: "daily-10-03", Properties: zfs.SnapshotProperties{CreationDate: time.Date(2026, 10, 3, 0, 0, 0, 0, time.UTC)}}
	existing := &data.FileBrowserEntry{Name: "existing"}
	inOlder := &data.FileBrowserEntry{Name: "a", OnlyInSnapshots: []*zfs.Snapshot{older}}
	inBoth := &data.FileBrowserEntry{Name: "b", OnlyInSnapshots: []*zfs.Snapshot{newer, older}}

	assert.Equal(t, "", formatFoundIn(existing))
	assert.Equal(t, "daily-10-01", formatFoundIn(inOlder))
	assert.Equal(t, "daily-10-03 +1", formatFoundIn(inBoth))

	assert.Equal(t, -1, compareFoundIn(existing, inOlder), "found in no other snapshot first")
	assert.Equal(t, -1, compareFoundIn(inOlder, inBoth), "by the newest snapshot")
	assert.Equal(t, 1, compareFoundIn(inBoth, inOlder))
	assert.Equal(t, 0, compareFoundIn(existing, existing))
}
