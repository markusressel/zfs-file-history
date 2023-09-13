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

func (file *RealFile) Equal(e RealFile) bool {
	return file.Name == e.Name && file.Path == e.Path
}

type SnapshotFile struct {
	Path         string
	OriginalPath string
	Stat         os.FileInfo
	Snapshot     *zfs.Snapshot
}

func (file *SnapshotFile) Equal(e SnapshotFile) bool {
	return file.Path == e.Path && file.OriginalPath == e.OriginalPath && file.Snapshot == e.Snapshot
}

type FileBrowserEntry struct {
	Name          string
	LatestFile    *RealFile
	SnapshotFiles []*SnapshotFile
}

func (entry *FileBrowserEntry) Equal(e FileBrowserEntry) bool {
	return entry.Name == e.Name && entry.LatestFile == e.LatestFile && slices.Equal(entry.SnapshotFiles, e.SnapshotFiles)
}

func NewFileBrowserEntry(name string, latestFile *RealFile, snapshots []*SnapshotFile) *FileBrowserEntry {
	return &FileBrowserEntry{
		Name:          name,
		LatestFile:    latestFile,
		SnapshotFiles: snapshots,
	}
}

func (entry *FileBrowserEntry) GetRealPath() string {
	if entry.HasLatest() {
		return entry.LatestFile.Path
	} else {
		return entry.SnapshotFiles[0].OriginalPath
	}
}

func (entry *FileBrowserEntry) GetStat() os.FileInfo {
	if entry.HasLatest() {
		return entry.LatestFile.Stat
	} else {
		return entry.SnapshotFiles[0].Stat
	}
}

func (entry *FileBrowserEntry) HasSnapshots() bool {
	return len(entry.SnapshotFiles) > 0
}

func (entry *FileBrowserEntry) HasLatest() bool {
	return entry.LatestFile != nil
}
