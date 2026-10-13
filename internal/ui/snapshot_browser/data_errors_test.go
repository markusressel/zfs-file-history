package snapshot_browser

import (
	"testing"
	"zfs-file-history/internal/data"
	"zfs-file-history/internal/zfs"

	"github.com/stretchr/testify/assert"
)

func TestFormatDamaged(t *testing.T) {
	browser := &SnapshotBrowserComponent{dataErrors: zfs.NewDataErrorIndex([]zfs.DataError{
		{Entry: "tank@a:/file", LivePath: "/data/file", Path: "/file", Dataset: "tank", Snapshot: "a"},
		{Entry: "tank@a:<0x1>", Dataset: "tank", Snapshot: "a"},
		{Entry: "tank@b:/other", LivePath: "/data/other", Path: "/other", Dataset: "tank", Snapshot: "b"},
	}, nil)}
	snapshot := func(name string) *zfs.Snapshot { return &zfs.Snapshot{Name: name, FullName: "tank@" + name} }

	assert.Equal(t, "2", browser.formatDamaged(snapshot("a")))
	assert.Equal(t, "1", browser.formatDamaged(snapshot("b")))
	assert.Empty(t, browser.formatDamaged(snapshot("c")))

	// the version of the selected file in the snapshot is damaged
	browser.currentFileEntry = &data.FileBrowserEntry{RealFile: &data.RealFile{Path: "/data/file"}}
	assert.Equal(t, "⚠ 2", browser.formatDamaged(snapshot("a")))
	assert.Equal(t, "1", browser.formatDamaged(snapshot("b")))

	browser.dataErrors = nil
	assert.Empty(t, browser.formatDamaged(snapshot("a")))
}

func TestSortSnapshotEntries_Damaged(t *testing.T) {
	index := zfs.NewDataErrorIndex([]zfs.DataError{
		{Dataset: "tank", Snapshot: "b"}, {Dataset: "tank", Snapshot: "b"}, {Dataset: "tank", Snapshot: "c"},
	}, nil)
	entries := []*data.SnapshotBrowserEntry{
		{Snapshot: &zfs.Snapshot{Name: "a", FullName: "tank@a"}},
		{Snapshot: &zfs.Snapshot{Name: "c", FullName: "tank@c"}},
		{Snapshot: &zfs.Snapshot{Name: "b", FullName: "tank@b"}},
	}

	sorted := sortSnapshotEntries(entries, columnDamaged, false, sortLookups{dataErrors: index})

	// the most damaged first
	var names []string
	for _, entry := range sorted {
		names = append(names, entry.Snapshot.Name)
	}
	assert.Equal(t, []string{"b", "c", "a"}, names)
}
