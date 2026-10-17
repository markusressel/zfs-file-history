package file_browser

import (
	"os"
	"testing"
	"zfs-file-history/internal/zfs"
)

// TestMain gives the folders of the tests no snapshots: all snapshots are shown by default, and the snapshots of the
// dataset are looked up by the .zfs folder of the file system, which a temporary directory may be below on a system
// with ZFS. Tests that need snapshots replace it (see newAllSnapshotsTest).
func TestMain(m *testing.M) {
	loadSnapshotsOfPath = func(string) ([]*zfs.Snapshot, error) { return nil, nil }
	os.Exit(m.Run())
}
