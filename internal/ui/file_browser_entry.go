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

func NewFileBrowserEntry(name string, path string, stat os.FileInfo, SnapshotOnly bool, snapshots []*zfs.SnapshotFile) *FileBrowserEntry {
	return &FileBrowserEntry{
		Name:         name,
		Path:         path,
		Stat:         stat,
		SnapshotOnly: SnapshotOnly,
		Snapshots:    snapshots,
	}
}

func (fileBrowserEntry *FileBrowserEntry) HasSnapshots() bool {
	return len(fileBrowserEntry.Snapshots) > 0
}
