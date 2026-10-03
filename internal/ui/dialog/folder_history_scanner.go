package dialog

import (
	"context"
	"slices"
	"zfs-file-history/internal/folder_listing"
	"zfs-file-history/internal/zfs"
)

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
	Listing  folder_listing.Listing
	State    folderVersionState
	// Changes are the changed entries compared to the previous snapshot
	Changes []*folder_listing.Change
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
	WorkingCopy folder_listing.Listing
}

// buildFolderHistory compares the listings of consecutive snapshots. snapshots and listings are in the same order,
// oldest first.
func buildFolderHistory(snapshots []*zfs.Snapshot, listings []folder_listing.Listing, workingCopy folder_listing.Listing) *folderHistory {
	history := &folderHistory{WorkingCopy: workingCopy}
	previous := folder_listing.Missing()
	for index, snapshot := range snapshots {
		listing := listings[index]
		version := &folderVersion{Snapshot: snapshot, Listing: listing, Index: index}
		switch {
		case !listing.Exists && !previous.Exists:
			version.State = folderVersionMissing
		case !listing.Exists:
			version.State = folderVersionDeleted
			version.Changes = folder_listing.Compare(previous, listing)
		case index == 0:
			version.State = folderVersionInitial
			version.Changes = folder_listing.Compare(previous, listing)
		case !previous.Exists:
			version.State = folderVersionCreated
			version.Changes = folder_listing.Compare(previous, listing)
		default:
			version.Changes = folder_listing.Compare(previous, listing)
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

func scanFolderHistory(ctx context.Context, folderPath string, snapshots []*zfs.Snapshot) (*folderHistory, error) {
	sorted := slices.Clone(snapshots)
	slices.SortStableFunc(sorted, func(a, b *zfs.Snapshot) int {
		return a.Properties.CreationDate.Compare(b.Properties.CreationDate)
	})

	listings, err := folder_listing.ReadInSnapshots(ctx, folderPath, sorted)
	if err != nil {
		return nil, err
	}

	return buildFolderHistory(sorted, listings, folder_listing.Read(folderPath)), nil
}
