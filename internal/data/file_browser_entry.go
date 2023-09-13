package data

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

func (file *SnapshotFile) HasChanged() bool {
	return file.Snapshot.CheckIfFileHasChanged(file.Path)
}

type FileBrowserEntryType int

const (
	File FileBrowserEntryType = iota + 1
	Directory
	Link
)

type FileBrowserEntryStatus int

const (
	Equal FileBrowserEntryStatus = iota
	Deleted
	Added
	Modified
	Unknown
)

type FileBrowserEntry struct {
	Name          string
	RealFile      *RealFile
	SnapshotFiles []*SnapshotFile
	Type          FileBrowserEntryType
	Status        FileBrowserEntryStatus
}

func (entry *FileBrowserEntry) Equal(e FileBrowserEntry) bool {
	return entry.Name == e.Name && entry.RealFile == e.RealFile && slices.Equal(entry.SnapshotFiles, e.SnapshotFiles)
}

func NewFileBrowserEntry(name string, latestFile *RealFile, snapshots []*SnapshotFile, entryType FileBrowserEntryType) *FileBrowserEntry {
	return &FileBrowserEntry{
		Name:          name,
		RealFile:      latestFile,
		SnapshotFiles: snapshots,
		Type:          entryType,
	}
}

func (entry *FileBrowserEntry) GetRealPath() string {
	if entry.HasReal() {
		return entry.RealFile.Path
	} else {
		return entry.SnapshotFiles[0].OriginalPath
	}
}

func (entry *FileBrowserEntry) GetStat() os.FileInfo {
	if entry.HasReal() {
		return entry.RealFile.Stat
	} else {
		return entry.SnapshotFiles[0].Stat
	}
}

// HasSnapshot indicated whether a snapshot file exists on the dataset for this entry.
// See HasReal if you are looking for the real file.
func (entry *FileBrowserEntry) HasSnapshot() bool {
	return len(entry.SnapshotFiles) > 0
}

// HasReal indicated whether a real file exists on the dataset for this entry
// See HasSnapshot if you are looking for a snapshot file.
func (entry *FileBrowserEntry) HasReal() bool {
	return entry.RealFile != nil
}
