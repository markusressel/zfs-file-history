package file_browser

import (
	"context"
	"errors"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
	"zfs-file-history/internal/data"
	"zfs-file-history/internal/data/diff_state"
	"zfs-file-history/internal/folder_listing"
	"zfs-file-history/internal/state"
	"zfs-file-history/internal/zfs"
)

// loadSnapshotsOfPath loads the snapshots of the dataset that contains path, if the snapshot browser does not know
// them yet, none if path is not on a dataset. Calls into ZFS, so it must not run on the UI thread. Replaceable in
// tests.
var loadSnapshotsOfPath = func(path string) ([]*zfs.Snapshot, error) {
	dataset, err := zfs.FindHostDataset(path)
	if errors.Is(err, zfs.ErrNoHostDataset) {
		// e.g. a folder of the system outside of the pools: nothing to search, not an error
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return dataset.GetSnapshots()
}

// allSnapshotsInput is what the entries of all snapshots are found with, captured on the UI thread, see
// computeTableEntries.
type allSnapshotsInput struct {
	// snapshots of the dataset at datasetPath, which contains the folder (unless a child dataset between them does,
	// see isInDataset), if known is true
	snapshots   []*zfs.Snapshot
	datasetPath string
	known       bool
	// load loads the snapshots if they are not known
	load func(path string) ([]*zfs.Snapshot, error)
	// showEntriesAfter is showEntriesWhileSearchingAfter, captured so a load that outlives a test does not read it
	// while the next test replaces it
	showEntriesAfter time.Duration
}

// allSnapshotsInput returns what the entries of all snapshots of the folder at path are found with, nil while they
// are not shown. Must be called on the UI thread.
func (fileBrowser *FileBrowserComponent) allSnapshotsInput(path string) *allSnapshotsInput {
	if !fileBrowser.showAllSnapshots || path == "" {
		return nil
	}
	input := &allSnapshotsInput{load: loadSnapshotsOfPath, showEntriesAfter: showEntriesWhileSearchingAfter}
	if fileBrowser.snapshotsOf != nil {
		input.datasetPath, input.snapshots, input.known = fileBrowser.snapshotsOf(path)
	}
	return input
}

// SetSnapshotsFunc sets the function that returns the snapshots of the dataset that contains a path and the path of
// the dataset if they are loaded already (e.g. by the snapshot browser), so showing the entries of all snapshots need
// not load them again. Must be called on the UI thread.
func (fileBrowser *FileBrowserComponent) SetSnapshotsFunc(snapshotsOf func(path string) (datasetPath string, snapshots []*zfs.Snapshot, ok bool)) {
	fileBrowser.snapshotsOf = snapshotsOf
}

// snapshotListingCache keeps the folder last read in the snapshots. Snapshots never change, so it is read once,
// not again for each snapshot selected. Safe for concurrent use.
type snapshotListingCache struct {
	mutex      sync.Mutex
	folderPath string
	// listings are by snapshotKey
	listings map[string]folder_listing.Listing
}

// snapshotKey identifies a snapshot, also if one of the same name is created again.
func snapshotKey(snapshot *zfs.Snapshot) string {
	return snapshot.Path + "@" + snapshot.Properties.CreationDate.String()
}

// read returns the folder at folderPath in the snapshots, in their order, reading those it does not have yet.
// progress (if not nil) is called with the number of snapshots done, from any goroutine. Accesses the file system,
// so it must not run on the UI thread.
func (cache *snapshotListingCache) read(ctx context.Context, folderPath string, snapshots []*zfs.Snapshot, progress func(done int)) ([]folder_listing.Listing, error) {
	cache.mutex.Lock()
	if cache.listings == nil || cache.folderPath != folderPath {
		cache.folderPath = folderPath
		cache.listings = map[string]folder_listing.Listing{}
	}
	// taken now: once the lock is released, a read of another folder may replace them
	listingsByKey := make(map[string]folder_listing.Listing, len(snapshots))
	var missing []*zfs.Snapshot
	for _, snapshot := range snapshots {
		key := snapshotKey(snapshot)
		if listing, ok := cache.listings[key]; ok {
			listingsByKey[key] = listing
		} else {
			missing = append(missing, snapshot)
		}
	}
	cache.mutex.Unlock()

	// read without holding the lock, so a load for another folder need not wait
	cached := len(snapshots) - len(missing)
	var readProgress func(read int)
	if progress != nil {
		progress(cached)
		readProgress = func(read int) { progress(cached + read) }
	}
	read, err := folder_listing.ReadInSnapshotsWithProgress(ctx, folderPath, missing, readProgress)
	if err != nil {
		return nil, err
	}

	cache.mutex.Lock()
	defer cache.mutex.Unlock()
	for i, snapshot := range missing {
		key := snapshotKey(snapshot)
		listingsByKey[key] = read[i]
		if cache.folderPath == folderPath {
			cache.listings[key] = read[i]
		}
	}
	listings := make([]folder_listing.Listing, len(snapshots))
	for i, snapshot := range snapshots {
		listings[i] = listingsByKey[snapshotKey(snapshot)]
	}
	return listings, nil
}

// findOnlyInSnapshots reads the folder at path in all snapshots of its dataset and returns the entries that are not
// listed in it now. progress is called with the number of snapshots read and of all of them, from any goroutine.
// Accesses the file system (and ZFS if the snapshots are not known), so it must not run on the UI thread.
func (fileBrowser *FileBrowserComponent) findOnlyInSnapshots(
	ctx context.Context,
	path string,
	input allSnapshotsInput,
	listed []*data.FileBrowserEntry,
	progress func(done int, total int),
) (map[string]folder_listing.SnapshotOnlyEntry, error) {
	snapshots := input.snapshots
	// the snapshot browser still shows the parent dataset right after entering a child dataset
	if !input.known || !isInDataset(path, input.datasetPath) {
		var err error
		if snapshots, err = input.load(path); err != nil {
			return nil, err
		}
	}
	sorted := slices.Clone(snapshots)
	slices.SortStableFunc(sorted, func(a, b *zfs.Snapshot) int {
		return a.Properties.CreationDate.Compare(b.Properties.CreationDate)
	})
	listings, err := fileBrowser.snapshotListings.read(ctx, path, sorted, func(done int) { progress(done, len(sorted)) })
	if err != nil {
		return nil, err
	}
	// only the names matter
	workingCopy := folder_listing.Listing{Exists: true, Entries: map[string]folder_listing.Entry{}}
	for _, entry := range listed {
		if entry.HasReal() {
			workingCopy.Entries[entry.Name] = folder_listing.Entry{Name: entry.Name}
		}
	}
	return folder_listing.FindOnlyInSnapshots(sorted, listings, workingCopy), nil
}

// isInDataset returns whether the folder at path is in the dataset mounted at datasetPath, not in a child dataset
// mounted below it (found by its .zfs folder, like zfs.FindHostDataset). Accesses the file system, so it must not run
// on the UI thread.
func isInDataset(path string, datasetPath string) bool {
	if path != datasetPath && !strings.HasPrefix(path, datasetPath+"/") {
		return false
	}
	for folder := filepath.Clean(path); folder != datasetPath && folder != filepath.Dir(folder); folder = filepath.Dir(folder) {
		if stat, err := os.Lstat(filepath.Join(folder, ".zfs")); err == nil && stat.IsDir() {
			return false
		}
	}
	return true
}

// entriesOnlyInOtherSnapshots returns the entries of onlyInSnapshots that are not listed yet (they exist neither now
// nor in the selected snapshot), each with its version in the newest snapshot that contains it. Accesses the file
// system, so it must not run on the UI thread.
func entriesOnlyInOtherSnapshots(
	ctx context.Context,
	path string,
	listed []*data.FileBrowserEntry,
	onlyInSnapshots map[string]folder_listing.SnapshotOnlyEntry,
) []*data.FileBrowserEntry {
	if len(onlyInSnapshots) == 0 {
		return nil
	}
	listedNames := make(map[string]bool, len(listed))
	for _, entry := range listed {
		listedNames[entry.Name] = true
	}
	var entries []*data.FileBrowserEntry
	for name, onlyInSnapshot := range onlyInSnapshots {
		if ctx.Err() != nil {
			return nil
		}
		if listedNames[name] || len(onlyInSnapshot.Snapshots) == 0 {
			continue
		}
		newest := onlyInSnapshot.Snapshots[0]
		realPath := filepath.Join(path, name)
		snapshotPath := newest.GetSnapshotPath(realPath)
		stat, err := os.Lstat(snapshotPath)
		if err != nil {
			// e.g. the snapshot was destroyed meanwhile
			continue
		}
		snapshotFile := &data.SnapshotFile{Path: snapshotPath, OriginalPath: realPath, Stat: stat, Snapshot: newest}
		entry := data.NewFileBrowserEntry(name, nil, []*data.SnapshotFile{snapshotFile}, entryTypeOf(onlyInSnapshot.Entry.Type))
		entry.OnlyInSnapshots = onlyInSnapshot.Snapshots
		// known without comparing, so it is not shown as unknown while added to a list that is shown already
		entry.DiffState = diff_state.Deleted
		entries = append(entries, entry)
	}
	return entries
}

// entryTypeOf returns the type of an entry of the file browser for the type of a folder listing entry.
func entryTypeOf(entryType folder_listing.EntryType) data.FileBrowserEntryType {
	switch entryType {
	case folder_listing.Directory:
		return data.Directory
	case folder_listing.Link:
		return data.Link
	default:
		return data.File
	}
}

// SetOnlyInSnapshots sets the entries of the folder at path that exist in snapshots but not now (see
// snapshot_browser.FolderChangesLoaded), counted in the footer while they are hidden. While they are shown (read by
// the file browser itself, in the same load as the other entries), the folder is only loaded again if they differ
// from the shown ones, e.g. after a snapshot was destroyed. Must be called on the UI thread.
func (fileBrowser *FileBrowserComponent) SetOnlyInSnapshots(path string, onlyInSnapshots map[string]folder_listing.SnapshotOnlyEntry) {
	fileBrowser.onlyInSnapshotsPath = path
	fileBrowser.onlyInSnapshots = onlyInSnapshots
	// while the snapshots are searched, the search adds what it finds
	if fileBrowser.showAllSnapshots && path == fileBrowser.path && fileBrowser.entriesAreCurrent() &&
		fileBrowser.snapshotSearch == nil && !fileBrowser.showsOnlyInSnapshots(onlyInSnapshots) {
		fileBrowser.Refresh(false)
		return
	}
	fileBrowser.updateFooter()
}

// showsOnlyInSnapshots returns whether the shown entries that only exist in other snapshots are those of
// onlyInSnapshots that exist neither now nor in the selected snapshot, in the same snapshots. Runs on the UI thread.
func (fileBrowser *FileBrowserComponent) showsOnlyInSnapshots(onlyInSnapshots map[string]folder_listing.SnapshotOnlyEntry) bool {
	shown := map[string][]string{}
	listed := map[string]bool{}
	for _, entry := range fileBrowser.tableContainer.GetAllEntries() {
		if entry.OnlyInSnapshots != nil {
			shown[entry.Name] = snapshotNames(entry.OnlyInSnapshots)
		} else {
			listed[entry.Name] = true
		}
	}
	expected := map[string][]string{}
	for name, onlyInSnapshot := range onlyInSnapshots {
		if !listed[name] {
			expected[name] = snapshotNames(onlyInSnapshot.Snapshots)
		}
	}
	return maps.EqualFunc(shown, expected, slices.Equal)
}

// snapshotNames returns the names of the snapshots, by which they are compared (the snapshot browser and the file
// browser may have loaded them separately).
func snapshotNames(snapshots []*zfs.Snapshot) []string {
	names := make([]string, len(snapshots))
	for i, snapshot := range snapshots {
		names[i] = snapshot.Name
	}
	return names
}

// ToggleShowAllSnapshots shows or hides the entries that only exist in other snapshots than the selected one, and
// remembers the setting. Must be called on the UI thread.
func (fileBrowser *FileBrowserComponent) ToggleShowAllSnapshots() {
	fileBrowser.showAllSnapshots = !fileBrowser.showAllSnapshots
	state.Current.SetToggle(toggleShowAllSnapshots, fileBrowser.showAllSnapshots)
	fileBrowser.tableContainer.UpdateShownColumns()
	// the shortcut map and the entries are updated once the entries are loaded
	fileBrowser.Refresh(false)
}

// IsShowingAllSnapshots returns whether the entries that only exist in other snapshots than the selected one are
// shown.
func (fileBrowser *FileBrowserComponent) IsShowingAllSnapshots() bool {
	return fileBrowser.showAllSnapshots
}

// countOnlyInOtherSnapshots returns the number of entries of the folder that exist neither now nor in the selected
// snapshot, but in other snapshots: those shown, or those that showAllSnapshots would show. Runs on the UI thread.
func (fileBrowser *FileBrowserComponent) countOnlyInOtherSnapshots() int {
	if fileBrowser.showAllSnapshots {
		count := 0
		for _, entry := range fileBrowser.tableContainer.GetAllEntries() {
			if entry.OnlyInSnapshots != nil {
				count++
			}
		}
		return count
	}
	if fileBrowser.onlyInSnapshotsPath != fileBrowser.entriesPath || !fileBrowser.entriesAreCurrent() {
		return 0
	}
	listed := make(map[string]bool)
	for _, entry := range fileBrowser.tableContainer.GetAllEntries() {
		listed[entry.Name] = true
	}
	count := 0
	for name := range fileBrowser.onlyInSnapshots {
		if !listed[name] {
			count++
		}
	}
	return count
}
