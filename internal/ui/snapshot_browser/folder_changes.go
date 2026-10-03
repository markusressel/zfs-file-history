package snapshot_browser

import (
	"context"
	"slices"
	"time"
	"zfs-file-history/internal/folder_listing"
	"zfs-file-history/internal/logging"
	"zfs-file-history/internal/zfs"
)

// folderChangesDelay is how long the folder has to stay the same before it is compared with its snapshots, so
// passing through folders does not read all their snapshots.
const folderChangesDelay = 250 * time.Millisecond

// compareSnapshots compares the folder in all snapshots with now and the previous snapshot, replaceable in tests.
var compareSnapshots = folder_listing.CompareSnapshots

// readWorkingCopy reads the folder as it is now, if no one provided it (see SetWorkingCopy), replaceable in tests.
var readWorkingCopy = folder_listing.Read

// folderChanges are the folder of the browser in each of its snapshots, compared with now and with the previous
// snapshot, see FolderChangesLoaded.
type folderChanges struct {
	folderPath string
	bySnapshot map[string]folder_listing.SnapshotChanges
}

// folderChangesInput is what the folder changes are computed from, to compute them only when it changed.
type folderChangesInput struct {
	folderPath    string
	snapshotNames []string
	// workingCopy is nil if it is read in the background
	workingCopy *folder_listing.Listing
}

func (input folderChangesInput) equal(other folderChangesInput) bool {
	if input.folderPath != other.folderPath || !slices.Equal(input.snapshotNames, other.snapshotNames) {
		return false
	}
	if input.workingCopy == nil || other.workingCopy == nil {
		return input.workingCopy == other.workingCopy
	}
	return input.workingCopy.Equal(*other.workingCopy)
}

// SetWorkingCopy sets the entries of the folder at path as they are now (e.g. those of the file browser, see
// FileBrowserComponent.WorkingCopyListing), so they need not be read again. The folder changes are computed again
// if they changed. Must be called on the UI thread.
func (snapshotBrowser *SnapshotBrowserComponent) SetWorkingCopy(path string, listing folder_listing.Listing) {
	snapshotBrowser.workingCopyPath = path
	snapshotBrowser.workingCopy = listing
	snapshotBrowser.updateFolderChanges()
}

// RequireFolderChanges computes the folder changes even while no column shows them, for listeners of
// FolderChangesLoaded (e.g. the path overview). Must be called on the UI thread.
func (snapshotBrowser *SnapshotBrowserComponent) RequireFolderChanges() {
	snapshotBrowser.folderChangesRequired = true
	snapshotBrowser.updateFolderChanges()
}

// folderChangesWanted returns whether the folder changes are shown or required.
func (snapshotBrowser *SnapshotBrowserComponent) folderChangesWanted() bool {
	if snapshotBrowser.folderChangesRequired {
		return true
	}
	columns := snapshotBrowser.tableContainer.GetColumnSpec()
	return slices.Contains(columns, columnVsNow) || slices.Contains(columns, columnChanges)
}

// updateFolderChanges compares the folder with its snapshots in the background, once the folder, its snapshots and
// its entries stayed the same for folderChangesDelay, unless nothing changed since the last time. A comparison for
// previous ones is canceled. Must be called on the UI thread.
func (snapshotBrowser *SnapshotBrowserComponent) updateFolderChanges() {
	path := snapshotBrowser.path
	snapshots := snapshotBrowser.currentSnapshots
	if path == "" || len(snapshots) == 0 || !snapshotBrowser.folderChangesWanted() {
		return
	}
	input := folderChangesInput{folderPath: path}
	for _, snapshot := range snapshots {
		input.snapshotNames = append(input.snapshotNames, snapshot.Name)
	}
	if snapshotBrowser.workingCopyPath == path {
		workingCopy := snapshotBrowser.workingCopy
		input.workingCopy = &workingCopy
	}
	if snapshotBrowser.folderChangesInput != nil && input.equal(*snapshotBrowser.folderChangesInput) {
		return
	}
	snapshotBrowser.folderChangesInput = &input
	if snapshotBrowser.cancelFolderChanges != nil {
		snapshotBrowser.cancelFolderChanges()
		snapshotBrowser.cancelFolderChanges = nil
	}
	// captured on the UI thread
	snapshots = slices.Clone(snapshots)

	snapshotBrowser.folderChangesDebouncer.Call(func() {
		ctx, cancel := context.WithCancel(context.Background())
		snapshotBrowser.cancelFolderChanges = cancel
		go func() {
			result, err := computeFolderChanges(ctx, input, snapshots)
			if err != nil {
				if ctx.Err() == nil {
					logging.Error("Could not compare %s with its snapshots: %v", path, err)
				}
				return
			}
			snapshotBrowser.application.QueueUpdateDraw(func() {
				if ctx.Err() != nil {
					return
				}
				snapshotBrowser.folderChanges = result
				snapshotBrowser.changesUpdated()
				snapshotBrowser.emit(FolderChangesLoaded{FolderPath: result.folderPath, BySnapshot: result.bySnapshot})
			})
		}()
	})
}

// computeFolderChanges compares the folder with its snapshots. Accesses the file system, so it must not run on the
// UI thread.
func computeFolderChanges(ctx context.Context, input folderChangesInput, snapshots []*zfs.Snapshot) (*folderChanges, error) {
	var workingCopy folder_listing.Listing
	if input.workingCopy != nil {
		workingCopy = *input.workingCopy
	} else {
		workingCopy = readWorkingCopy(input.folderPath)
	}
	bySnapshot, err := compareSnapshots(ctx, input.folderPath, snapshots, workingCopy)
	if err != nil {
		return nil, err
	}
	return &folderChanges{folderPath: input.folderPath, bySnapshot: bySnapshot}, nil
}

// changesOf returns the changes of the folder in the snapshot, and false while they are computed.
func (snapshotBrowser *SnapshotBrowserComponent) changesOf(snapshot *zfs.Snapshot) (folder_listing.SnapshotChanges, bool) {
	if snapshotBrowser.folderChanges == nil || snapshotBrowser.folderChanges.folderPath != snapshotBrowser.path {
		return folder_listing.SnapshotChanges{}, false
	}
	changes, ok := snapshotBrowser.folderChanges.bySnapshot[snapshot.Name]
	return changes, ok
}
