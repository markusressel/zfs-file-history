package ui

import (
	"os"
	"zfs-file-history/internal/zfs"
)

type RealFile struct {
	Name string
	Path string
	Stat os.FileInfo
}

type SnapshotFile struct {
	Path         string
	OriginalPath string
	Stat         os.FileInfo
	Snapshot     *zfs.Snapshot
}

type FileBrowserEntry struct {
	Name          string
	LatestFile    *RealFile
	SnapshotFiles []*SnapshotFile
}

func NewFileBrowserEntry(name string, latestFile *RealFile, snapshots []*SnapshotFile) *FileBrowserEntry {
	return &FileBrowserEntry{
		Name:          name,
		LatestFile:    latestFile,
		SnapshotFiles: snapshots,
	}
}

func (fileBrowserEntry *FileBrowserEntry) GetRealPath() string {
	if fileBrowserEntry.HasLatest() {
		return fileBrowserEntry.LatestFile.Path
	} else {
		return fileBrowserEntry.SnapshotFiles[0].OriginalPath
	}
}

func (fileBrowserEntry *FileBrowserEntry) GetStat() os.FileInfo {
	if fileBrowserEntry.HasLatest() {
		return fileBrowserEntry.LatestFile.Stat
	} else {
		return fileBrowserEntry.SnapshotFiles[0].Stat
	}
}

func (fileBrowserEntry *FileBrowserEntry) HasSnapshots() bool {
	return len(fileBrowserEntry.SnapshotFiles) > 0
}

func (fileBrowserEntry *FileBrowserEntry) HasLatest() bool {
	return fileBrowserEntry.LatestFile != nil
}
