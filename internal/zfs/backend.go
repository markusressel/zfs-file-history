package zfs

import (
	"context"
)

// Backend is where the datasets, snapshots and pools come from, and what changes them. System is the one of the
// system (libzfs, the zfs and zpool commands); others (e.g. in tests) can replace it, see SetBackend.
//
// The functions of this package check their arguments and explain errors (e.g. missing permissions), the backend
// only does what is asked. It speaks Go types, not the text output of the commands. All methods may block (they
// run processes), so they must not be called on the UI thread, and they must be safe for concurrent use.
type Backend interface {
	// Mounts returns the mounted ZFS datasets and snapshots.
	Mounts() ([]Mount, error)
	// Reload drops what the backend cached, so the next calls return the current state.
	Reload()
	// ListDatasets returns all filesystem datasets, with the paths they are mounted at.
	ListDatasets() ([]*DatasetListEntry, error)
	// ListChildDatasets returns the datasets (filesystems and volumes) and their direct children, without the paths
	// they are mounted at.
	ListChildDatasets(parents []string) ([]*DatasetListEntry, error)
	// DatasetProperties returns the datasetProperties of the dataset, with exact values ("zfs get -p"). Unknown
	// ones are left out.
	DatasetProperties(name string) (map[string]string, error)
	// ListSnapshotNames returns the names (behind the "@") of the snapshots of the dataset, the newest first.
	ListSnapshotNames(dataset string) ([]string, error)
	// SnapshotProperties returns the properties of the snapshots (names behind the "@") of the dataset. Snapshots
	// that cannot be read are left out.
	SnapshotProperties(dataset string, names []string) map[string]SnapshotProperties
	// CreateSnapshot creates the snapshot (name behind the "@") of the dataset, as the current user. With recursive
	// also the ones with the same name of the descendants, all at once.
	CreateSnapshot(dataset string, name string, recursive bool) error
	// DestroySnapshot destroys the snapshot (name behind the "@") of the dataset, as the current user. With
	// recursive also the ones with the same name of the descendants, with dependantClones also their clones.
	DestroySnapshot(dataset string, name string, recursive bool, dependantClones bool) error
	// CloneSnapshot creates the dataset target from the snapshot with the full name, as the current user.
	CloneSnapshot(fullName string, target string) error
	// RollbackSnapshot rolls the dataset back to its snapshot (name behind the "@"), as the current user. With
	// destroyNewer the newer snapshots (and bookmarks) are destroyed, without it the rollback fails if there are any.
	RollbackSnapshot(dataset string, name string, destroyNewer bool) error
	// PreviewDestroyDataset returns what destroying the dataset would destroy and free, without destroying anything.
	// With recursive also its snapshots and descendants, never their dependent clones.
	PreviewDestroyDataset(name string, recursive bool) (*DestroyPreview, error)
	// DestroyDataset destroys the dataset, as the current user. With recursive also its snapshots and descendants,
	// never their dependent clones (it fails if there are any).
	DestroyDataset(name string, recursive bool) error
	// PromoteDataset promotes the clone, so it no longer depends on its origin, as the current user.
	PromoteDataset(name string) error
	// ListProperties returns all properties of the dataset, with human-readable values.
	ListProperties(dataset string) ([]*Property, error)
	// SetProperty sets the property of the dataset as the current user.
	SetProperty(dataset string, name string, value string) error
	// InheritProperty removes the local value of the property of the dataset as the current user.
	InheritProperty(dataset string, name string) error
	// Delegations returns the delegated permissions of the dataset and its ancestors ("zfs allow").
	Delegations(dataset string) (*Delegations, error)
	// ApplyDelegationChange delegates and revokes the permissions of the change, as the current user.
	ApplyDelegationChange(change DelegationChange) error
	// CurrentIdentity returns the user whose permissions apply.
	CurrentIdentity() (Identity, error)
	// AllowedSudoersRules returns the ids of the SudoersRules the user may use.
	AllowedSudoersRules() map[string]bool

	// ListHolds returns the holds of the snapshots with the given full names, with recursive also the ones of the
	// snapshots with the same name of the descendant datasets.
	ListHolds(snapshotNames []string, recursive bool) ([]Hold, error)
	// Hold adds a hold with the tag to the snapshots with the given full names.
	Hold(tag string, snapshotNames []string) error
	// Release removes the hold with the tag from the snapshots with the given full names.
	Release(tag string, snapshotNames []string) error
	// PreviewDestroySnapshots returns what destroying the snapshots (names behind the "@") of the dataset would
	// destroy and free, without destroying anything.
	PreviewDestroySnapshots(dataset string, snapshots []string, recursive bool, dependantClones bool) (*DestroyPreview, error)

	// ListPools returns the listing properties of all pools.
	ListPools() ([]*PoolListEntry, error)
	// PoolStatus returns the status of the pool with the given name.
	PoolStatus(name string) (*PoolStatus, error)
	// PoolStatuses returns the status of all pools.
	PoolStatuses() ([]*PoolStatus, error)
	// DataErrorStatuses returns the status of all pools with their damaged files (PoolStatus.ErrorEntries), and the
	// pools whose damaged files cannot be listed (e.g. without root).
	DataErrorStatuses() (statuses []*PoolStatus, unavailable []string, err error)
	// PoolHistory returns the history of the pool, oldest first, and reports how far it got to progress (if not nil).
	PoolHistory(ctx context.Context, pool string, progress func(PoolHistoryProgress)) ([]PoolHistoryEntry, error)
	// PoolsWithoutHeadErrlog returns the pools of the given ones whose feature head_errlog is disabled.
	PoolsWithoutHeadErrlog(pools []string) []string
	// PoolFeatures returns the features the installed ZFS supports with their states on the pool, its property
	// compatibility and the features its files allow (Unavailable is not set, see ListPoolFeatures).
	PoolFeatures(pool string) (*PoolFeatures, error)
	// RunPoolAction runs the action on the pool. Returns a PoolActionNeedsRootError if the user may not.
	RunPoolAction(action PoolAction, pool string) error
	// WatchEvents calls onDatasetsChanged when datasets or snapshots were created, destroyed, or pools imported or
	// exported, onPoolsChanged when the state of a pool changed, and onHistory with each event ZFS logged in the
	// history of a pool (PoolHistoryEntry.Internal), from another goroutine, until ctx is done. Returns an error if
	// the events cannot be watched (e.g. no permission).
	WatchEvents(ctx context.Context, onDatasetsChanged func(), onPoolsChanged func(), onHistory func(PoolHistoryEntry)) error
}

// Mount is a mounted ZFS dataset or snapshot.
type Mount struct {
	// Name is the name of the dataset, or the full name of the snapshot (snapshots are mounted below .zfs/snapshot
	// when they are accessed)
	Name       string
	Mountpoint string
}

// backend is the Backend used by the functions of this package, see SetBackend.
var backend = defaultBackend()

// SetBackend replaces the Backend (System by default, NoSystem in tests). Must be called before anything uses this
// package, it is not safe for concurrent use.
func SetBackend(newBackend Backend) {
	backend = newBackend
}
