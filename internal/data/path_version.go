package data

import (
	"os"
	"slices"
	"strings"
	"zfs-file-history/internal/zfs"
)

// PathVersion is a file or folder as it is in a snapshot.
type PathVersion struct {
	Snapshot *zfs.Snapshot
	// Info is the file info in the snapshot, nil if the snapshot does not contain the path
	Info os.FileInfo
}

// InfoDiffers returns whether two versions of a path differ (nil: the path does not exist), judged by their
// metadata like the diff states. For folders, the modification time changes when entries are added or removed.
func InfoDiffers(a os.FileInfo, b os.FileInfo) bool {
	if a == nil || b == nil {
		return (a == nil) != (b == nil)
	}
	return a.IsDir() != b.IsDir() ||
		a.Mode() != b.Mode() ||
		a.Size() != b.Size() ||
		!a.ModTime().Equal(b.ModTime())
}

// NewVersions returns for each snapshot (by name) whether a new version of the path starts in it: it was created,
// changed or deleted compared to the previous snapshot, or it is in the oldest snapshot. versions may be in any order.
func NewVersions(versions []PathVersion) map[string]bool {
	sorted := slices.Clone(versions)
	slices.SortStableFunc(sorted, func(a, b PathVersion) int {
		if result := a.Snapshot.Properties.CreationDate.Compare(b.Snapshot.Properties.CreationDate); result != 0 {
			return result
		}
		return strings.Compare(a.Snapshot.Name, b.Snapshot.Name)
	})
	result := make(map[string]bool, len(sorted))
	var previous os.FileInfo
	for _, version := range sorted {
		result[version.Snapshot.Name] = InfoDiffers(previous, version.Info)
		previous = version.Info
	}
	return result
}
