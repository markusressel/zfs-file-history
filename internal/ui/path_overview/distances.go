package path_overview

import (
	"zfs-file-history/internal/folder_listing"
	"zfs-file-history/internal/zfs"
)

// folderDistances are the number of entries that differ between the folder in each snapshot and now, by snapshot
// name. A snapshot that does not contain the folder has no distance.
type folderDistances struct {
	folderPath string
	byName     map[string]int
}

// SetFolderChanges sets how the folder at path compares with now in each snapshot (see
// snapshot_browser.FolderChangesLoaded). Must be called on the UI thread.
func (overview *PathOverviewComponent) SetFolderChanges(path string, bySnapshot map[string]folder_listing.SnapshotChanges) {
	distances := &folderDistances{folderPath: path, byName: map[string]int{}}
	for name, changes := range bySnapshot {
		if changes.Exists {
			distances.byName[name] = changes.VsNow.Total()
		}
	}
	overview.distances = distances
}

// currentDistances returns the distances of the folder that is shown, nil while they are computed.
func (overview *PathOverviewComponent) currentDistances() *folderDistances {
	if overview.distances == nil || overview.distances.folderPath != overview.folderPath {
		return nil
	}
	return overview.distances
}

// distanceValues returns the distances in the order of the versions of history (oldest first), -1 for snapshots
// that do not contain the folder.
func distanceValues(history *pathHistory, distances *folderDistances) []int64 {
	values := make([]int64, len(history.versions))
	for i, version := range history.versions {
		values[i] = -1
		if distance, ok := distances.byName[version.Snapshot.Name]; ok {
			values[i] = int64(distance)
		}
	}
	return values
}

// sameAsNowSince returns the oldest snapshot from which on the folder is the same as now in all newer snapshots
// (nil: it differs from now in the newest snapshot), and whether it is the same as now in all snapshots.
func sameAsNowSince(history *pathHistory, values []int64) (since *zfs.Snapshot, always bool) {
	for i := len(values) - 1; i >= 0 && values[i] == 0; i-- {
		since = history.versions[i].Snapshot
		always = i == 0
	}
	return since, always
}
