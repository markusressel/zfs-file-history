package file_browser

import (
	"testing"
	"zfs-file-history/internal/data"
	"zfs-file-history/internal/ui/theme"
	"zfs-file-history/internal/zfs"

	"github.com/stretchr/testify/assert"
)

func TestFormatDamage(t *testing.T) {
	index := zfs.NewDataErrorIndex([]zfs.DataError{
		{Entry: "/data/both", LivePath: "/data/both", Path: "/both", Dataset: "tank"},
		{Entry: "tank@a:/both", LivePath: "/data/both", Path: "/both", Dataset: "tank", Snapshot: "a"},
		{Entry: "tank@b:/both", LivePath: "/data/both", Path: "/both", Dataset: "tank", Snapshot: "b"},
		{Entry: "/data/live", LivePath: "/data/live", Path: "/live", Dataset: "tank"},
		{Entry: "tank@a:/dir/old", LivePath: "/data/dir/old", Path: "/dir/old", Dataset: "tank", Snapshot: "a"},
	}, nil)
	file := func(path string) *data.FileBrowserEntry {
		return &data.FileBrowserEntry{RealFile: &data.RealFile{Path: path}, Type: data.File}
	}
	colors := theme.Colors.Alert

	tests := []struct {
		entry   *data.FileBrowserEntry
		note    string
		damaged bool
	}{
		{file("/data/both"), "⚠ damaged, also in 2 snapshots", true},
		{file("/data/live"), "⚠ damaged", true},
		// deleted, only in a snapshot
		{&data.FileBrowserEntry{SnapshotFiles: []*data.SnapshotFile{{OriginalPath: "/data/dir/old"}}, Type: data.File}, "⚠ damaged in 1 snapshot", true},
		{&data.FileBrowserEntry{RealFile: &data.RealFile{Path: "/data/dir"}, Type: data.Directory}, "⚠ 1 damaged inside", true},
		// a file is not a folder
		{file("/data/dir"), "", false},
		{file("/data/ok"), "", false},
	}
	for _, test := range tests {
		t.Run(test.entry.GetRealPath(), func(t *testing.T) {
			note, color, damaged := formatDamage(index, test.entry)
			assert.Equal(t, test.damaged, damaged)
			assert.Equal(t, test.note, note)
			if damaged && test.entry.HasReal() && index.IsDamaged(test.entry.GetRealPath(), "") {
				assert.Equal(t, colors.Error, color)
			} else if damaged {
				assert.Equal(t, colors.Warning, color)
			}
		})
	}

	_, _, damaged := formatDamage(nil, file("/data/live"))
	assert.False(t, damaged, "no damaged files")
}
