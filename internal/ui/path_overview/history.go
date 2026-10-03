package path_overview

import (
	"os"
	"slices"
	"strings"
	"zfs-file-history/internal/data"
	"zfs-file-history/internal/zfs"
)

// pathHistory summarizes a file or folder across all snapshots of its dataset.
type pathHistory struct {
	// versions are the path in all snapshots, oldest first
	versions []data.PathVersion
	// changed tells for each version whether it differs from the one in the previous snapshot (it appeared,
	// disappeared or changed)
	changed []bool
	// present is the number of snapshots that contain the path
	present int
	// changes is the number of snapshots in which the path changed compared to the previous one
	changes int
	// distinct is the number of different versions of the path
	distinct int
	// first is the oldest snapshot containing the path, lastChange the newest one in which it changed (nil: none)
	first      *zfs.Snapshot
	lastChange *zfs.Snapshot
}

// summarize compares the consecutive versions of a path. The order of versions does not matter.
func summarize(versions []data.PathVersion) *pathHistory {
	sorted := slices.Clone(versions)
	slices.SortStableFunc(sorted, func(a, b data.PathVersion) int {
		if result := a.Snapshot.Properties.CreationDate.Compare(b.Snapshot.Properties.CreationDate); result != 0 {
			return result
		}
		return strings.Compare(a.Snapshot.Name, b.Snapshot.Name)
	})

	history := &pathHistory{versions: sorted, changed: make([]bool, len(sorted))}
	for i, version := range sorted {
		if version.Info != nil {
			history.present++
			if history.first == nil {
				history.first = version.Snapshot
			}
		}
		if i == 0 {
			if version.Info != nil {
				history.distinct++
			}
			continue
		}
		if !isDifferent(sorted[i-1].Info, version.Info) {
			continue
		}
		history.changed[i] = true
		history.changes++
		history.lastChange = version.Snapshot
		if version.Info != nil {
			history.distinct++
		}
	}
	return history
}

// isDifferent returns whether two versions of a path differ (nil: the path does not exist), judged by their
// metadata like the diff states. For folders, the modification time changes when entries are added or removed.
func isDifferent(a os.FileInfo, b os.FileInfo) bool {
	if a == nil || b == nil {
		return (a == nil) != (b == nil)
	}
	return a.IsDir() != b.IsDir() ||
		a.Mode() != b.Mode() ||
		a.Size() != b.Size() ||
		!a.ModTime().Equal(b.ModTime())
}

// indexOf returns the index of the version in the given snapshot, or -1.
func (history *pathHistory) indexOf(snapshot *zfs.Snapshot) int {
	if snapshot == nil {
		return -1
	}
	return slices.IndexFunc(history.versions, func(version data.PathVersion) bool {
		return version.Snapshot.Name == snapshot.Name
	})
}

// sizes returns the sizes of a file for a sparkline, -1 where it does not exist.
func (history *pathHistory) sizes() []int64 {
	values := make([]int64, len(history.versions))
	for i, version := range history.versions {
		values[i] = -1
		if version.Info != nil {
			values[i] = version.Info.Size()
		}
	}
	return values
}

// activity returns for a sparkline: 1 where the path changed, 0 where it did not and -1 where it does not exist.
func (history *pathHistory) activity() []int64 {
	values := make([]int64, len(history.versions))
	for i, version := range history.versions {
		switch {
		case version.Info == nil:
			values[i] = -1
		case history.changed[i]:
			values[i] = 1
		}
	}
	return values
}
