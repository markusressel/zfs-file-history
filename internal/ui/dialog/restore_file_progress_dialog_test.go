package dialog

import (
	"testing"
	"time"
	"zfs-file-history/internal/data"
	"zfs-file-history/internal/zfs"

	"github.com/rivo/tview"
	"github.com/stretchr/testify/assert"
)

func TestRestoreFileProgressDialog(t *testing.T) {
	app := tview.NewApplication()
	file := &data.FileBrowserEntry{
		Name: "test.txt",
		SnapshotFiles: []*data.SnapshotFile{
			{
				Path: "/pool/ds1/.zfs/snapshot/snap1/test.txt",
				Snapshot: &zfs.Snapshot{
					Name: "snap1",
					ParentDataset: &zfs.Dataset{
						Path:          "/pool/ds1",
						HiddenZfsPath: "/pool/ds1/.zfs",
					},
				},
			},
		},
	}

	d := NewRestoreFileProgressDialog(app, file, false)

	assert.Equal(t, string(RestoreFileProgress), d.GetName())
	assert.NotNil(t, d.GetLayout())
	assert.NotNil(t, d.GetActionChannel())

	// Wait briefly to allow background goroutine and handleError to execute
	time.Sleep(100 * time.Millisecond)

	d.Close()
}
