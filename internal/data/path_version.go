package data

import (
	"os"
	"zfs-file-history/internal/zfs"
)

// PathVersion is a file or folder as it is in a snapshot.
type PathVersion struct {
	Snapshot *zfs.Snapshot
	// Info is the file info in the snapshot, nil if the snapshot does not contain the path
	Info os.FileInfo
}
