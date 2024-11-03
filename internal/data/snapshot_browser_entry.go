package data

import (
	"zfs-file-history/internal/data/diff_state"
	"zfs-file-history/internal/zfs"
)

type SnapshotBrowserEntry struct {
	Snapshot  *zfs.Snapshot
	DiffState diff_state.DiffState
}

func (s *SnapshotBrowserEntry) TableRowId() string {
	return s.Snapshot.Path
}
