package file_history

import (
	"fmt"
	"time"
	"zfs-file-history/internal/data"
	"zfs-file-history/internal/state"
	"zfs-file-history/internal/zfs"
)

// snapshotsOfPath returns the snapshots of the dataset that contains path. The snapshots of cachedEntries (e.g.
// the ones the snapshot browser shows) are used if they belong to that dataset, otherwise they are loaded.
// Accesses ZFS, so it must not run on the UI thread.
func snapshotsOfPath(path string, cachedEntries []*data.SnapshotBrowserEntry) ([]*zfs.Snapshot, error) {
	dataset, err := zfs.FindHostDataset(path)
	if err != nil {
		return nil, fmt.Errorf("failed to find host dataset: %w", err)
	}
	if len(cachedEntries) > 0 && cachedEntries[0].Snapshot != nil && cachedEntries[0].Snapshot.ParentDataset != nil &&
		cachedEntries[0].Snapshot.ParentDataset.Path == dataset.Path {
		var snapshots []*zfs.Snapshot
		for _, entry := range cachedEntries {
			if entry != nil && entry.Snapshot != nil {
				snapshots = append(snapshots, entry.Snapshot)
			}
		}
		return snapshots, nil
	}
	snapshots, err := dataset.GetSnapshots()
	if err != nil {
		return nil, fmt.Errorf("failed to get snapshots for dataset %s: %w", dataset.Path, err)
	}
	return snapshots, nil
}

// Keys of the settings the history overlays remember in state.Current: the column layouts of their tables, and their modes.
const (
	stateKeyFileHistoryTable      = "fileHistory"
	stateKeyFolderHistoryTimeline = "folderHistory.timeline"
	stateKeyFolderHistoryChanges  = "folderHistory.changes"

	// toggleFileHistoryComparePrevious: compare versions to the previous version instead of the working copy
	toggleFileHistoryComparePrevious = "fileHistory.comparePrevious"
	// toggleFolderHistoryCompareNow: show the changes since a snapshot instead of the changes in it
	toggleFolderHistoryCompareNow = "folderHistory.compareNow"
)

// loadDiffMode returns the remembered mode: alternative if the toggle with the given key is on, else standard.
func loadDiffMode(key string, standard diffMode, alternative diffMode) diffMode {
	if state.Current.Toggle(key, false) {
		return alternative
	}
	return standard
}

// saveDiffMode remembers whether the alternative mode is used.
func saveDiffMode(key string, mode diffMode, alternative diffMode) {
	state.Current.SetToggle(key, mode == alternative)
}

// versionAt returns the version that was current in the snapshot created at: the newest of versions created at or
// before it, or the oldest version if all are newer. The histories only list the snapshots in which something
// changed, so a snapshot in between shows the version of the last change before it. nil if versions is empty.
func versionAt[T any](versions []*T, created func(*T) time.Time, at time.Time) *T {
	var current, oldest *T
	for _, version := range versions {
		date := created(version)
		if oldest == nil || date.Before(created(oldest)) {
			oldest = version
		}
		if !date.After(at) && (current == nil || date.After(created(current))) {
			current = version
		}
	}
	if current == nil {
		return oldest
	}
	return current
}
