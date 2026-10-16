package file_browser

import (
	"os"
	"path/filepath"
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

// Entries that exist neither now nor in the selected snapshot, but in other snapshots, are counted in the footer, and
// listed while all snapshots are shown (a), with their version in the newest snapshot that contains them.
func TestFileBrowser_ShowAllSnapshots(t *testing.T) {
	ft := newFileBrowserTest(t, "")
	dataset := &zfs.Dataset{Path: ft.dir, HiddenZfsPath: filepath.Join(ft.dir, ".zfs")}
	snapshot := func(name string, files ...string) *zfs.Snapshot {
		base := filepath.Join(ft.dir, ".zfs", "snapshot", name)
		for _, file := range files {
			require.NoError(t, os.MkdirAll(base, 0o755))
			require.NoError(t, os.WriteFile(filepath.Join(base, file), []byte(name), 0o644))
		}
		return &zfs.Snapshot{Name: name, Path: base, ParentDataset: dataset}
	}
	// gone.txt is in both snapshots, lost.txt only in the older one
	s1 := snapshot("s1", "gone.txt", "lost.txt")
	s2 := snapshot("s2", "gone.txt")
	file := folder_listing.Entry{Type: folder_listing.File}
	onlyInSnapshots := map[string]folder_listing.SnapshotOnlyEntry{
		"gone.txt": {Entry: file, Snapshots: []*zfs.Snapshot{s2, s1}},
		"lost.txt": {Entry: file, Snapshots: []*zfs.Snapshot{s1}},
	}
	entry := func(name string) (found *data.FileBrowserEntry) {
		testutil.OnUiThread(t, ft.app, func() {
			for _, entry := range ft.fileBrowser.tableContainer.GetAllEntries() {
				if entry.Name == name {
					found = entry
				}
			}
		})
		return found
	}

	testutil.OnUiThread(t, ft.app, func() {
		ft.fileBrowser.SetPath(ft.dir, true)
		ft.fileBrowser.SetSelectedSnapshot(&data.SnapshotBrowserEntry{Snapshot: s2})
	})
	ft.waitFor("loaded", func(s fileBrowserState) bool { return s.footer == "5 entries" })
	testutil.OnUiThread(t, ft.app, func() { ft.fileBrowser.SetOnlyInSnapshots(ft.dir, onlyInSnapshots) })
	ft.waitFor("lost.txt is only in another snapshot than s2", func(s fileBrowserState) bool {
		return s.footer == "5 entries · 1 only in other snapshots hidden"
	})

	ft.pressKey(tcell.KeyRune, 'a')
	s := ft.waitFor("shown", func(s fileBrowserState) bool { return s.footer == "6 entries · 1 only in other snapshots shown" })
	assert.Equal(t, []string{"a.txt", "b.txt", "c.log", "gone.txt", "lost.txt", "sub"}, s.visible)
	lost := entry("lost.txt")
	require.NotNil(t, lost)
	assert.Equal(t, []*zfs.Snapshot{s1}, lost.OnlyInSnapshots)
	assert.Same(t, s1, lost.SnapshotFiles[0].Snapshot, "restored from the snapshot that contains it")
	assert.Equal(t, filepath.Join(ft.dir, "lost.txt"), lost.GetRealPath())
	assert.Nil(t, entry("gone.txt").OnlyInSnapshots, "in the selected snapshot: listed as before")
	require.Eventually(t, func() bool {
		var state diff_state.DiffState
		testutil.OnUiThread(t, ft.app, func() { state = lost.DiffState })
		return state == diff_state.Deleted
	}, 5*time.Second, 20*time.Millisecond)
	assert.Contains(t, testutil.ScreenText(t, ft.app, ft.screen), "lost.txt  only in s1")

	// another snapshot selected: gone.txt is newest in s2
	testutil.OnUiThread(t, ft.app, func() {
		ft.fileBrowser.SetSelectedSnapshot(&data.SnapshotBrowserEntry{Snapshot: snapshot("s0")})
	})
	ft.waitFor("both only in other snapshots", func(s fileBrowserState) bool {
		return s.footer == "6 entries · 2 only in other snapshots shown"
	})
	assert.Same(t, s2, entry("gone.txt").SnapshotFiles[0].Snapshot, "the newest snapshot that contains it")
	assert.Contains(t, testutil.ScreenText(t, ft.app, ft.screen), "gone.txt  in 2 snapshots, newest s2")

	ft.pressKey(tcell.KeyRune, 'a')
	s = ft.waitFor("hidden again", func(s fileBrowserState) bool { return s.footer == "4 entries · 2 only in other snapshots hidden" })
	assert.Equal(t, []string{"a.txt", "b.txt", "c.log", "sub"}, s.visible)
}

// The entries of another folder are neither counted nor listed.
func TestFileBrowser_ShowAllSnapshots_OtherFolder(t *testing.T) {
	ft := newFileBrowserTest(t, "")
	testutil.OnUiThread(t, ft.app, func() {
		ft.fileBrowser.SetPath(ft.dir, true)
		ft.fileBrowser.SetOnlyInSnapshots(ft.sub, map[string]folder_listing.SnapshotOnlyEntry{
			"gone.txt": {Snapshots: []*zfs.Snapshot{{Name: "s1"}}},
		})
	})
	ft.pressKey(tcell.KeyRune, 'a')
	testutil.WaitForUpdates(t, ft.app)
	s := ft.waitFor("loaded", func(s fileBrowserState) bool { return s.footer == "4 entries" })
	assert.Equal(t, []string{"a.txt", "b.txt", "c.log", "sub"}, s.visible)
}
