package ui

import (
	"os"
	"zfs-file-history/internal/zfs"
)

type FileBrowserEntry struct {
	Name         string
	Path         string
	Stat         os.FileInfo
	SnapshotOnly bool
	Snapshots    []*zfs.SnapshotFile
}

func (fileBrowserEntry *FileBrowserEntry) HasSnapshots() bool {
	return len(fileBrowserEntry.Snapshots) > 0
}
