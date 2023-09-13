package ui

import (
	"golang.org/x/exp/slices"
	"os"
	"zfs-file-history/internal/zfs"
)

type RealFile struct {
	Name string
	Path string
	Stat os.FileInfo
}

func (v *RealFile) Equal(e RealFile) bool {
	return v.Name == e.Name && v.Path == e.Path
}

type SnapshotFile struct {
	Path         string
	OriginalPath string
	Stat         os.FileInfo
	Snapshot     *zfs.Snapshot
}

func (v *SnapshotFile) Equal(e SnapshotFile) bool {
	return v.Path == e.Path && v.OriginalPath == e.OriginalPath && v.Snapshot == e.Snapshot
}

type FileBrowserEntry struct {
	Name          string
	LatestFile    *RealFile
	SnapshotFiles []*SnapshotFile
}

func (v *FileBrowserEntry) Equal(e FileBrowserEntry) bool {
	return v.Name == e.Name && v.LatestFile == e.LatestFile && slices.Equal(v.SnapshotFiles, e.SnapshotFiles)
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
