package file_history

import (
	"testing"
	"zfs-file-history/internal/data"
	"zfs-file-history/internal/testutil"
	"zfs-file-history/internal/ui/table"
	"zfs-file-history/internal/ui/theme"
	"zfs-file-history/internal/zfs"

	"github.com/stretchr/testify/assert"
)

func TestFileHistoryOverlay_MarksDamagedVersions(t *testing.T) {
	app, _ := testutil.NewApp()
	file := &data.FileBrowserEntry{Name: "a", RealFile: &data.RealFile{Name: "a", Path: "/home/a"}, Type: data.File}
	overlay := NewFileHistoryOverlay(app, file, nil)
	daily := &data.SnapshotBrowserEntry{Snapshot: &zfs.Snapshot{Name: "daily", FullName: "rpool/home@daily"}}
	weekly := &data.SnapshotBrowserEntry{Snapshot: &zfs.Snapshot{Name: "weekly", FullName: "rpool/home@weekly"}}

	overlay.SetDataErrors(zfs.NewDataErrorIndex([]zfs.DataError{{Pool: "rpool", Entry: "rpool/home@daily:/a", Dataset: "rpool/home", Snapshot: "daily", Path: "/a", LivePath: "/home/a"}}, nil))

	cells := overlay.createTableCells(0, []*table.Column{historyColumnName}, daily)
	assert.Equal(t, "daily  ⚠ damaged", cells[0].Text)
	foreground, _, _ := cells[0].Style.Decompose()
	assert.Equal(t, theme.Colors.Alert.Error, foreground)
	cells = overlay.createTableCells(0, []*table.Column{historyColumnName}, weekly)
	assert.Equal(t, "weekly", cells[0].Text)
}
