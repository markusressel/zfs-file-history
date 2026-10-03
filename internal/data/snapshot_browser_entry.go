package data

import (
	"os"
	"zfs-file-history/internal/data/diff_state"
	"zfs-file-history/internal/zfs"
)

type SnapshotBrowserEntry struct {
	Snapshot             *zfs.Snapshot
	DiffState            diff_state.DiffState
	WorkingCopyDiffState diff_state.DiffState
	IsLoading            bool
	// HasEntryInfo is true once the selected file or folder was looked up in the snapshot (false without one)
	HasEntryInfo bool
	// EntryInfo is the selected file or folder in the snapshot, nil if the snapshot does not contain it
	EntryInfo os.FileInfo
}

func (s SnapshotBrowserEntry) TableRowId() string {
	return s.Snapshot.Path
}
