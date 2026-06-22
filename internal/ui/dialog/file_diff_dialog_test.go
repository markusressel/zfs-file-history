package dialog

import (
	"testing"
	"zfs-file-history/internal/data"
	"zfs-file-history/internal/zfs"

	"github.com/rivo/tview"
	"github.com/stretchr/testify/assert"
)

func TestNewFileDiffDialog(t *testing.T) {
	app := tview.NewApplication()
	file := &data.FileBrowserEntry{
		RealFile: &data.RealFile{Path: "/pool/ds1/file.txt"},
	}
	snapshot := &data.SnapshotBrowserEntry{
		Snapshot: &zfs.Snapshot{
			Name: "snap-1",
			ParentDataset: &zfs.Dataset{
				Path:          "/pool/ds1",
				HiddenZfsPath: "/pool/ds1/.zfs",
			},
		},
	}

	d := NewFileDiffDialog(app, file, snapshot)
	assert.Equal(t, "FileDiffDialog", d.GetName())
	assert.NotNil(t, d.GetLayout())
	assert.NotNil(t, d.GetActionChannel())
}
