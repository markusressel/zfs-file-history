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

// VersionChangeKind is what happened to a path in a snapshot, compared to the previous snapshot.
type VersionChangeKind int

const (
	// VersionUnchanged: the same as in the previous snapshot (or in neither of them)
	VersionUnchanged VersionChangeKind = iota
	// VersionInitial: in the oldest snapshot, there is no previous one to compare with
	VersionInitial
	// VersionCreated: in the snapshot, but not in the previous one
	VersionCreated
	// VersionDeleted: in the previous snapshot, but not in this one
	VersionDeleted
	// VersionModified: in both, but different
	VersionModified
)

// VersionChange is what happened to a path in a snapshot, compared to the previous snapshot.
type VersionChange struct {
	Kind VersionChangeKind
	// SizeDelta is the change of the size (for folders on ZFS: of the number of items, see os.FileInfo.Size)
	SizeDelta int64
}

// IsNewVersion returns whether a new version of the path starts in the snapshot.
func (change VersionChange) IsNewVersion() bool {
	return change.Kind != VersionUnchanged
}

// VersionChanges returns for each snapshot (by name) what happened to the path compared to the previous snapshot.
// versions may be in any order.
func VersionChanges(versions []PathVersion) map[string]VersionChange {
	sorted := slices.Clone(versions)
	slices.SortStableFunc(sorted, func(a, b PathVersion) int {
		if result := a.Snapshot.Properties.CreationDate.Compare(b.Snapshot.Properties.CreationDate); result != 0 {
			return result
		}
		return strings.Compare(a.Snapshot.Name, b.Snapshot.Name)
	})
	result := make(map[string]VersionChange, len(sorted))
	var previous os.FileInfo
	for i, version := range sorted {
		current := version.Info
		var change VersionChange
		switch {
		case !InfoDiffers(previous, current):
			change.Kind = VersionUnchanged
		case i == 0:
			change.Kind = VersionInitial
		case previous == nil:
			change = VersionChange{Kind: VersionCreated, SizeDelta: current.Size()}
		case current == nil:
			change = VersionChange{Kind: VersionDeleted, SizeDelta: -previous.Size()}
		default:
			change = VersionChange{Kind: VersionModified, SizeDelta: current.Size() - previous.Size()}
		}
		result[version.Snapshot.Name] = change
		previous = current
	}
	return result
}
