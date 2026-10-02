package dialog

import (
	"context"
	"os"
	"slices"
	"strings"
	"sync"
	"time"
	"zfs-file-history/internal/zfs"
)

// folderEntryType is the type of a directory entry, as far as the folder history is concerned.
type folderEntryType int

const (
	folderEntryFile folderEntryType = iota
	folderEntryDirectory
	folderEntryLink
	folderEntryOther
)

// folderEntry is a direct entry of a folder, in a snapshot or in the working copy.
type folderEntry struct {
	Name    string
	Type    folderEntryType
	Size    int64
	Mode    os.FileMode
	ModTime time.Time
}

// differsFrom returns whether the entry changed: its type, mode or modification time, or (for files) its size.
// For directories, the modification time changes when entries are added, removed or renamed in them.
func (e folderEntry) differsFrom(other folderEntry) bool {
	if e.Type != other.Type || e.Mode != other.Mode || !e.ModTime.Equal(other.ModTime) {
		return true
	}
	return e.Type == folderEntryFile && e.Size != other.Size
}

// folderListing is the content of a folder: its direct entries.
type folderListing struct {
	// Exists is false if the folder does not exist (in the snapshot)
	Exists  bool
	Entries map[string]folderEntry
}

// totalSize is the size of the files directly in the folder.
func (l folderListing) totalSize() int64 {
	var size int64
	for _, entry := range l.Entries {
		if entry.Type == folderEntryFile {
			size += entry.Size
		}
	}
	return size
}

// readFolderListing reads the direct entries of the folder at path, without following symlinks.
// Entries that cannot be read are skipped. Accesses the file system, so it must not run on the UI thread.
func readFolderListing(path string) folderListing {
	dirEntries, err := os.ReadDir(path)
	if err != nil {
		return folderListing{Exists: false, Entries: map[string]folderEntry{}}
	}
	listing := folderListing{Exists: true, Entries: make(map[string]folderEntry, len(dirEntries))}
	for _, dirEntry := range dirEntries {
		info, err := dirEntry.Info()
		if err != nil {
			continue
		}
		entryType := folderEntryOther
		switch {
		case info.Mode().IsRegular():
			entryType = folderEntryFile
		case info.IsDir():
			entryType = folderEntryDirectory
		case info.Mode()&os.ModeSymlink != 0:
			entryType = folderEntryLink
		}
		listing.Entries[dirEntry.Name()] = folderEntry{
			Name:    dirEntry.Name(),
			Type:    entryType,
			Size:    info.Size(),
			Mode:    info.Mode(),
			ModTime: info.ModTime(),
		}
	}
	return listing
}

// folderChangeKind is how an entry changed between two listings.
type folderChangeKind int

const (
	folderChangeAdded folderChangeKind = iota
	folderChangeDeleted
	folderChangeModified
)

// folderChange is a changed entry between two listings ("before" and "after").
type folderChange struct {
	Name string
	Kind folderChangeKind
	// Before is the entry in the older listing, nil if it was added
	Before *folderEntry
	// After is the entry in the newer listing, nil if it was deleted
	After *folderEntry
}

func (c folderChange) TableRowId() string {
	return c.Name
}

// compareFolderListings returns the entries that differ between the listings, sorted by name.
func compareFolderListings(before folderListing, after folderListing) []*folderChange {
	var changes []*folderChange
	for name, afterEntry := range after.Entries {
		beforeEntry, existed := before.Entries[name]
		switch {
		case !existed:
			changes = append(changes, &folderChange{Name: name, Kind: folderChangeAdded, After: &afterEntry})
		case afterEntry.differsFrom(beforeEntry):
			changes = append(changes, &folderChange{Name: name, Kind: folderChangeModified, Before: &beforeEntry, After: &afterEntry})
		}
	}
	for name, beforeEntry := range before.Entries {
		if _, exists := after.Entries[name]; !exists {
			changes = append(changes, &folderChange{Name: name, Kind: folderChangeDeleted, Before: &beforeEntry})
		}
	}
	slices.SortFunc(changes, func(a, b *folderChange) int {
		return strings.Compare(a.Name, b.Name)
	})
	return changes
}

// countChanges returns the number of added, deleted and modified entries.
func countChanges(changes []*folderChange) (added int, deleted int, modified int) {
	for _, change := range changes {
		switch change.Kind {
		case folderChangeAdded:
			added++
		case folderChangeDeleted:
			deleted++
		case folderChangeModified:
			modified++
		}
	}
	return added, deleted, modified
}

// folderVersionState is what happened to the folder itself in a snapshot.
type folderVersionState int

const (
	// folderVersionChanged: the folder exists before and in the snapshot, its entries changed
	folderVersionChanged folderVersionState = iota
	// folderVersionInitial: the folder exists in the oldest snapshot
	folderVersionInitial
	// folderVersionCreated: the folder exists in the snapshot, but not in the previous one
	folderVersionCreated
	// folderVersionDeleted: the folder existed in the previous snapshot, but not in this one
	folderVersionDeleted
	// folderVersionUnchanged: nothing changed compared to the previous snapshot
	folderVersionUnchanged
	// folderVersionMissing: the folder neither exists in the snapshot nor in the previous one
	folderVersionMissing
)

// folderVersion is the folder in a snapshot, compared to the previous snapshot.
type folderVersion struct {
	Snapshot *zfs.Snapshot
	Listing  folderListing
	State    folderVersionState
	// Changes are the changed entries compared to the previous snapshot
	Changes []*folderChange
	// Index is the position of the snapshot among all snapshots, oldest first (for the sparklines)
	Index int
}

func (v folderVersion) TableRowId() string {
	return v.Snapshot.Name
}

// folderHistory is the history of a folder across all snapshots of its dataset.
type folderHistory struct {
	// All are the versions in all snapshots, oldest first
	All []*folderVersion
	// Changed are the versions in which the folder changed, newest first
	Changed     []*folderVersion
	WorkingCopy folderListing
}

// buildFolderHistory compares the listings of consecutive snapshots. snapshots and listings are in the same order,
// oldest first.
func buildFolderHistory(snapshots []*zfs.Snapshot, listings []folderListing, workingCopy folderListing) *folderHistory {
	history := &folderHistory{WorkingCopy: workingCopy}
	previous := folderListing{Exists: false, Entries: map[string]folderEntry{}}
	for index, snapshot := range snapshots {
		listing := listings[index]
		version := &folderVersion{Snapshot: snapshot, Listing: listing, Index: index}
		switch {
		case !listing.Exists && !previous.Exists:
			version.State = folderVersionMissing
		case !listing.Exists:
			version.State = folderVersionDeleted
			version.Changes = compareFolderListings(previous, listing)
		case index == 0:
			version.State = folderVersionInitial
			version.Changes = compareFolderListings(previous, listing)
		case !previous.Exists:
			version.State = folderVersionCreated
			version.Changes = compareFolderListings(previous, listing)
		default:
			version.Changes = compareFolderListings(previous, listing)
			version.State = folderVersionChanged
			if len(version.Changes) == 0 {
				version.State = folderVersionUnchanged
			}
		}
		history.All = append(history.All, version)
		if version.State != folderVersionUnchanged && version.State != folderVersionMissing {
			history.Changed = append(history.Changed, version)
		}
		previous = listing
	}
	slices.Reverse(history.Changed)
	return history
}

// folderListingWorkers is the number of snapshot directories read at the same time.
const folderListingWorkers = 16

// scanFolderHistory reads the folder in all snapshots (in parallel) and in the working copy, and builds its history.
// snapshots may be in any order. Accesses the file system, so it must not run on the UI thread.
func scanFolderHistory(ctx context.Context, folderPath string, snapshots []*zfs.Snapshot) (*folderHistory, error) {
	sorted := slices.Clone(snapshots)
	slices.SortStableFunc(sorted, func(a, b *zfs.Snapshot) int {
		return a.Properties.CreationDate.Compare(b.Properties.CreationDate)
	})

	listings := make([]folderListing, len(sorted))
	indices := make(chan int)
	var workers sync.WaitGroup
	for i := 0; i < folderListingWorkers; i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for index := range indices {
				listings[index] = readFolderListing(sorted[index].GetSnapshotPath(folderPath))
			}
		}()
	}
	for index := range sorted {
		select {
		case indices <- index:
		case <-ctx.Done():
		}
		if ctx.Err() != nil {
			break
		}
	}
	close(indices)
	workers.Wait()
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	return buildFolderHistory(sorted, listings, readFolderListing(folderPath)), nil
}
