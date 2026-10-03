package path_overview

import (
	"context"
	"slices"
	"time"
	"zfs-file-history/internal/folder_listing"
	"zfs-file-history/internal/logging"
	"zfs-file-history/internal/zfs"
)

// distanceDelay is how long the folder has to stay the same before its distances are computed, so passing through
// folders does not read all their snapshots.
const distanceDelay = 250 * time.Millisecond

// readListings reads the folder in all snapshots, replaceable in tests.
var readListings = folder_listing.ReadInSnapshots

// folderDistances are the number of entries that differ between the folder in each snapshot and now, by snapshot
// name. A snapshot that does not contain the folder has no distance.
type folderDistances struct {
	folderPath string
	byName     map[string]int
}

// SetWorkingCopy sets the entries of the folder at path as they are now (see FileBrowserComponent.WorkingCopyListing).
// The distances are computed again if they changed.
func (overview *PathOverviewComponent) SetWorkingCopy(path string, listing folder_listing.Listing) {
	if path == overview.workingCopyPath && listing.Equal(overview.workingCopy) {
		return
	}
	overview.workingCopyPath = path
	overview.workingCopy = listing
	overview.scheduleDistances()
}

// setSnapshots sets the snapshots of the folder at path, and computes the distances again if they changed.
func (overview *PathOverviewComponent) setSnapshots(path string, snapshots []*zfs.Snapshot) {
	sameNames := slices.EqualFunc(snapshots, overview.snapshots, func(a *zfs.Snapshot, b *zfs.Snapshot) bool {
		return a.Name == b.Name
	})
	if path == overview.snapshotsPath && sameNames {
		return
	}
	overview.snapshotsPath = path
	overview.snapshots = snapshots
	overview.scheduleDistances()
}

// scheduleDistances computes the distances in the background once the folder, its snapshots and its entries are
// known and stayed the same for distanceDelay. A computation for previous ones is canceled.
func (overview *PathOverviewComponent) scheduleDistances() {
	if overview.cancelDistances != nil {
		overview.cancelDistances()
		overview.cancelDistances = nil
	}
	path := overview.folderPath
	if path == "" || overview.snapshotsPath != path || overview.workingCopyPath != path {
		overview.distanceDebouncer.Cancel()
		return
	}
	// captured on the UI thread
	snapshots := overview.snapshots
	workingCopy := overview.workingCopy

	overview.distanceDebouncer.Call(func() {
		ctx, cancel := context.WithCancel(context.Background())
		overview.cancelDistances = cancel
		go func() {
			distances, err := computeDistances(ctx, path, snapshots, workingCopy)
			if err != nil {
				if ctx.Err() == nil {
					logging.Error("Could not compare %s with its snapshots: %v", path, err)
				}
				return
			}
			overview.application.QueueUpdateDraw(func() {
				if ctx.Err() == nil {
					overview.distances = distances
				}
			})
		}()
	})
}

// computeDistances reads the folder in all snapshots and compares it with workingCopy. Accesses the file system, so
// it must not run on the UI thread.
func computeDistances(ctx context.Context, folderPath string, snapshots []*zfs.Snapshot, workingCopy folder_listing.Listing) (*folderDistances, error) {
	listings, err := readListings(ctx, folderPath, snapshots)
	if err != nil {
		return nil, err
	}
	distances := &folderDistances{folderPath: folderPath, byName: map[string]int{}}
	for i, listing := range listings {
		if listing.Exists {
			distances.byName[snapshots[i].Name] = len(folder_listing.Compare(listing, workingCopy))
		}
	}
	return distances, nil
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
