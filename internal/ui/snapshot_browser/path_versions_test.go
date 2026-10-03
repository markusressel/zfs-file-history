package snapshot_browser

import (
	"os"
	"path/filepath"
	"testing"
	"time"
	"zfs-file-history/internal/data"
	"zfs-file-history/internal/testutil"
	"zfs-file-history/internal/zfs"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The diff calculation also reports the versions of the folder and the selected entry in all snapshots, for the
// path overview, from the same file system reads.
func TestSnapshotBrowser_PathVersionsLoaded(t *testing.T) {
	root := t.TempDir()
	dataset := &zfs.Dataset{Path: root, HiddenZfsPath: filepath.Join(root, ".zfs")}
	write := func(path string, content string) {
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
	}
	var snapshots []*zfs.Snapshot
	for i, name := range []string{"s1", "s2"} {
		base := filepath.Join(root, ".zfs", "snapshot", name)
		require.NoError(t, os.MkdirAll(filepath.Join(base, "docs"), 0o755))
		snapshots = append(snapshots, &zfs.Snapshot{Name: name, Path: base, ParentDataset: dataset,
			Properties: zfs.SnapshotProperties{CreationDate: time.Date(2026, 10, i+1, 0, 0, 0, 0, time.UTC)}})
	}
	// notes.txt is only in s2
	write(filepath.Join(root, ".zfs", "snapshot", "s2", "docs", "notes.txt"), "1234")
	folderPath := filepath.Join(root, "docs")
	filePath := filepath.Join(folderPath, "notes.txt")
	write(filePath, "123456")

	app := tview.NewApplication()
	app.SetScreen(tcell.NewSimulationScreen("UTF-8"))
	browser := NewSnapshotBrowser(app)
	app.SetRoot(browser.GetLayout(), true)
	go func() { _ = app.Run() }()
	t.Cleanup(app.Stop)

	loaded := make(chan PathVersionsLoaded, 10)
	entry := &data.FileBrowserEntry{Name: "notes.txt", Type: data.File, RealFile: &data.RealFile{Name: "notes.txt", Path: filePath}}
	testutil.OnUiThread(t, app, func() {
		browser.Events.Subscribe(func(event Event) {
			if e, ok := event.(PathVersionsLoaded); ok {
				loaded <- e
			}
		})
		browser.path = folderPath
		browser.currentSnapshots = snapshots
		browser.SetFileEntry(entry)
	})

	var event PathVersionsLoaded
	select {
	case event = <-loaded:
	case <-time.After(3 * time.Second):
		t.Fatal("no PathVersionsLoaded")
	}
	assert.Equal(t, folderPath, event.FolderPath)
	assert.Equal(t, root, event.DatasetPath)
	assert.Same(t, entry, event.Entry)

	sizes := map[string]int64{}
	for _, version := range event.EntryVersions {
		sizes[version.Snapshot.Name] = -1
		if version.Info != nil {
			sizes[version.Snapshot.Name] = version.Info.Size()
		}
	}
	assert.Equal(t, map[string]int64{"s1": -1, "s2": 4}, sizes, "the file as it is in each snapshot")
	require.Len(t, event.Folder, 2)
	for _, version := range event.Folder {
		require.NotNil(t, version.Info, version.Snapshot.Name)
		assert.True(t, version.Info.IsDir())
	}

	// on the header row of the file browser: only the folder
	testutil.OnUiThread(t, app, func() { browser.SetFileEntry(nil) })
	select {
	case event = <-loaded:
	case <-time.After(3 * time.Second):
		t.Fatal("no PathVersionsLoaded")
	}
	assert.Nil(t, event.Entry)
	assert.Empty(t, event.EntryVersions)
	assert.Len(t, event.Folder, 2)
}
