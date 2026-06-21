package data

import (
	"errors"
	"os"
	"zfs-file-history/internal/data/diff_state"
	"zfs-file-history/internal/util"
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
	// Path is the path of the file within the snapshot
	Path string
	// OriginalPath is the path of the file in the real filesystem
	OriginalPath string
	// Stat is the file info of the file within the snapshot
	Stat os.FileInfo
	// Snapshot is the snapshot this file belongs to
	Snapshot *zfs.Snapshot
}

func (file *SnapshotFile) Equal(e SnapshotFile) bool {
	return file.Path == e.Path && file.OriginalPath == e.OriginalPath && file.Snapshot == e.Snapshot
}

func (file *SnapshotFile) HasChanged() bool {
	return file.Snapshot.IsRealFileDifferent(file.Path)
}

func (file *SnapshotFile) Exists() bool {
	return util.FileExists(file.Path)
}

type FileBrowserEntryType int

const (
	Directory FileBrowserEntryType = iota + 1
	Link
	File
)

type FileBrowserEntry struct {
	Name          string
	RealFile      *RealFile
	SnapshotFiles []*SnapshotFile
	Type          FileBrowserEntryType
	DiffState     diff_state.DiffState
	IsLoading     bool
}

func (entry FileBrowserEntry) TableRowId() string {
	return entry.GetRealPath()
}

func (entry *FileBrowserEntry) Equal(e FileBrowserEntry) bool {
	return entry.GetRealPath() == e.GetRealPath()
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
	} else if len(entry.SnapshotFiles) > 0 && entry.SnapshotFiles[0] != nil {
		return entry.SnapshotFiles[0].OriginalPath
	}
	return ""
}

func (entry *FileBrowserEntry) GetStat() os.FileInfo {
	if entry.HasReal() {
		return entry.RealFile.Stat
	}
	if len(entry.SnapshotFiles) > 0 && entry.SnapshotFiles[0] != nil {
		return entry.SnapshotFiles[0].Stat
	}
	return nil
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

func (entry *FileBrowserEntry) HasDiff() bool {
	return entry != nil && entry.DiffState != diff_state.Equal && entry.HasSnapshot()
}

func (entry *FileBrowserEntry) CanEnter() (bool, error) {
	newPath := entry.GetRealPath()
	stat, err := os.Lstat(newPath)
	if err != nil {
		// cannot enter path, ignoring
		return false, err
	}

	// TODO: add check if the file is a link and if it points to a directory, if so we should allow entering it
	// TODO: also add a check if the file maybe exists only in a snapshot, if so we should also allow entering it, but we need to check if the file is a directory in the snapshot then
	if !stat.IsDir() {
		return false, errors.New("file is not a directory")
	}

	_, err = os.ReadDir(newPath)
	if err != nil {
		return false, err
	}

	return true, nil
}
