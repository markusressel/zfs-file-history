// Package folder_listing reads the direct entries of a folder, in the working copy or in snapshots, and compares
// them. It is used by the folder history and the path overview.
package folder_listing

import (
	"context"
	"maps"
	"os"
	"slices"
	"strings"
	"sync"
	"time"
	"zfs-file-history/internal/zfs"
)

// EntryType is the type of a directory entry, as far as comparing folders is concerned.
type EntryType int

const (
	File EntryType = iota
	Directory
	Link
	Other
)

// Entry is a direct entry of a folder, in a snapshot or in the working copy.
type Entry struct {
	Name    string
	Type    EntryType
	Size    int64
	Mode    os.FileMode
	ModTime time.Time
}

// NewEntry returns the entry with the given name and file info (as returned by os.Lstat).
func NewEntry(name string, info os.FileInfo) Entry {
	entryType := Other
	switch {
	case info.Mode().IsRegular():
		entryType = File
	case info.IsDir():
		entryType = Directory
	case info.Mode()&os.ModeSymlink != 0:
		entryType = Link
	}
	return Entry{Name: name, Type: entryType, Size: info.Size(), Mode: info.Mode(), ModTime: info.ModTime()}
}

// DiffersFrom returns whether the entry changed: its type, mode or modification time, or (for files) its size.
// For directories, the modification time changes when entries are added, removed or renamed in them.
func (e Entry) DiffersFrom(other Entry) bool {
	if e.Type != other.Type || e.Mode != other.Mode || !e.ModTime.Equal(other.ModTime) {
		return true
	}
	return e.Type == File && e.Size != other.Size
}

// Listing is the content of a folder: its direct entries.
type Listing struct {
	// Exists is false if the folder does not exist (in the snapshot)
	Exists  bool
	Entries map[string]Entry
}

// Missing is the listing of a folder that does not exist.
func Missing() Listing {
	return Listing{Exists: false, Entries: map[string]Entry{}}
}

// Equal returns whether both listings have the same entries, none of which changed.
func (l Listing) Equal(other Listing) bool {
	return l.Exists == other.Exists && maps.EqualFunc(l.Entries, other.Entries, func(a Entry, b Entry) bool {
		return a.Name == b.Name && !a.DiffersFrom(b)
	})
}

// TotalSize is the size of the files directly in the folder.
func (l Listing) TotalSize() int64 {
	var size int64
	for _, entry := range l.Entries {
		if entry.Type == File {
			size += entry.Size
		}
	}
	return size
}

// Read reads the direct entries of the folder at path, without following symlinks.
// Entries that cannot be read are skipped. Accesses the file system, so it must not run on the UI thread.
func Read(path string) Listing {
	dirEntries, err := os.ReadDir(path)
	if err != nil {
		return Missing()
	}
	listing := Listing{Exists: true, Entries: make(map[string]Entry, len(dirEntries))}
	for _, dirEntry := range dirEntries {
		info, err := dirEntry.Info()
		if err != nil {
			continue
		}
		listing.Entries[dirEntry.Name()] = NewEntry(dirEntry.Name(), info)
	}
	return listing
}

// readWorkers is the number of snapshot directories read at the same time.
const readWorkers = 16

// ReadInSnapshots reads the folder at folderPath in all snapshots, in parallel. The listings are in the order of
// snapshots. Accesses the file system, so it must not run on the UI thread.
func ReadInSnapshots(ctx context.Context, folderPath string, snapshots []*zfs.Snapshot) ([]Listing, error) {
	listings := make([]Listing, len(snapshots))
	indices := make(chan int)
	var workers sync.WaitGroup
	for i := 0; i < readWorkers; i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for index := range indices {
				listings[index] = Read(snapshots[index].GetSnapshotPath(folderPath))
			}
		}()
	}
	for index := range snapshots {
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
	return listings, nil
}

// ChangeKind is how an entry changed between two listings.
type ChangeKind int

const (
	Added ChangeKind = iota
	Deleted
	Modified
)

// Change is a changed entry between two listings ("before" and "after").
type Change struct {
	Name string
	Kind ChangeKind
	// Before is the entry in the older listing, nil if it was added
	Before *Entry
	// After is the entry in the newer listing, nil if it was deleted
	After *Entry
}

func (c Change) TableRowId() string {
	return c.Name
}

// Compare returns the entries that differ between the listings, sorted by name.
func Compare(before Listing, after Listing) []*Change {
	var changes []*Change
	for name, afterEntry := range after.Entries {
		beforeEntry, existed := before.Entries[name]
		switch {
		case !existed:
			changes = append(changes, &Change{Name: name, Kind: Added, After: &afterEntry})
		case afterEntry.DiffersFrom(beforeEntry):
			changes = append(changes, &Change{Name: name, Kind: Modified, Before: &beforeEntry, After: &afterEntry})
		}
	}
	for name, beforeEntry := range before.Entries {
		if _, exists := after.Entries[name]; !exists {
			changes = append(changes, &Change{Name: name, Kind: Deleted, Before: &beforeEntry})
		}
	}
	slices.SortFunc(changes, func(a, b *Change) int {
		return strings.Compare(a.Name, b.Name)
	})
	return changes
}

// CountChanges returns the number of added, deleted and modified entries.
func CountChanges(changes []*Change) (added int, deleted int, modified int) {
	for _, change := range changes {
		switch change.Kind {
		case Added:
			added++
		case Deleted:
			deleted++
		case Modified:
			modified++
		}
	}
	return added, deleted, modified
}

// Counts is the number of added, deleted and modified entries between two listings.
type Counts struct {
	Added    int
	Deleted  int
	Modified int
}

// CountsOf counts the changes by their kind.
func CountsOf(changes []*Change) Counts {
	var counts Counts
	counts.Added, counts.Deleted, counts.Modified = CountChanges(changes)
	return counts
}

// Total is the number of changed entries.
func (c Counts) Total() int {
	return c.Added + c.Deleted + c.Modified
}

// SnapshotChanges is a folder in a snapshot, compared with now and with the previous snapshot.
type SnapshotChanges struct {
	// Exists is false if the snapshot does not contain the folder; the counts are zero then
	Exists bool
	// VsNow are the entries that differ between the snapshot and now
	VsNow Counts
	// Initial is true for the oldest snapshot: there is no previous one to compare with
	Initial bool
	// VsPrevious are the entries that changed since the previous snapshot (the folder did not exist in it: all
	// entries are added)
	VsPrevious Counts
}

// CompareSnapshots reads the folder at folderPath in all snapshots (in any order) and compares it with workingCopy
// (the folder as it is now) and with the previous snapshot. Only the counts are kept, not the listings, by snapshot
// name. Accesses the file system, so it must not run on the UI thread.
func CompareSnapshots(ctx context.Context, folderPath string, snapshots []*zfs.Snapshot, workingCopy Listing) (map[string]SnapshotChanges, error) {
	sorted := slices.Clone(snapshots)
	slices.SortStableFunc(sorted, func(a, b *zfs.Snapshot) int {
		return a.Properties.CreationDate.Compare(b.Properties.CreationDate)
	})
	listings, err := ReadInSnapshots(ctx, folderPath, sorted)
	if err != nil {
		return nil, err
	}
	result := make(map[string]SnapshotChanges, len(sorted))
	previous := Missing()
	for i, listing := range listings {
		changes := SnapshotChanges{Exists: listing.Exists, Initial: i == 0}
		if listing.Exists {
			changes.VsNow = CountsOf(Compare(listing, workingCopy))
			if !changes.Initial {
				changes.VsPrevious = CountsOf(Compare(previous, listing))
			}
		}
		result[sorted[i].Name] = changes
		previous = listing
	}
	return result, nil
}
