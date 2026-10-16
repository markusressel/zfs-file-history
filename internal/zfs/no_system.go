package zfs

import (
	"context"
	"errors"
	"testing"
)

// ErrNoSystem is returned by NoSystem: tests do not use the ZFS of the system.
var ErrNoSystem = errors.New("tests do not use the ZFS of the system, see zfs.SetBackend")

// NoSystem is the Backend of test binaries unless a test sets another (see SetBackend): a system without ZFS, where
// everything fails with ErrNoSystem. With the System, the same test would see the pools of each system it runs on
// (none in CI), which made tests pass on one and fail on another.
type NoSystem struct{}

var _ Backend = NoSystem{}

// defaultBackend returns the Backend used until SetBackend: the System, NoSystem in tests.
func defaultBackend() Backend {
	if testing.Testing() {
		return NoSystem{}
	}
	return System{}
}

func (NoSystem) Mounts() ([]Mount, error)                   { return nil, ErrNoSystem }
func (NoSystem) Reload()                                    {}
func (NoSystem) ListDatasets() ([]*DatasetListEntry, error) { return nil, ErrNoSystem }
func (NoSystem) ListChildDatasets([]string) ([]*DatasetListEntry, error) {
	return nil, ErrNoSystem
}
func (NoSystem) DatasetProperties(string) (map[string]string, error) { return nil, ErrNoSystem }
func (NoSystem) ListSnapshotNames(string) ([]string, error)          { return nil, ErrNoSystem }
func (NoSystem) SnapshotProperties(string, []string) map[string]SnapshotProperties {
	return map[string]SnapshotProperties{}
}
func (NoSystem) CreateSnapshot(string, string, bool) error        { return ErrNoSystem }
func (NoSystem) DestroySnapshot(string, string, bool, bool) error { return ErrNoSystem }
func (NoSystem) CloneSnapshot(string, string) error               { return ErrNoSystem }
func (NoSystem) RollbackSnapshot(string, string, bool) error      { return ErrNoSystem }
func (NoSystem) PreviewDestroyDataset(string, bool) (*DestroyPreview, error) {
	return nil, ErrNoSystem
}
func (NoSystem) DestroyDataset(string, bool) error            { return ErrNoSystem }
func (NoSystem) PromoteDataset(string) error                  { return ErrNoSystem }
func (NoSystem) ListProperties(string) ([]*Property, error)   { return nil, ErrNoSystem }
func (NoSystem) SetProperty(string, string, string) error     { return ErrNoSystem }
func (NoSystem) InheritProperty(string, string) error         { return ErrNoSystem }
func (NoSystem) Delegations(string) (*Delegations, error)     { return nil, ErrNoSystem }
func (NoSystem) ApplyDelegationChange(DelegationChange) error { return ErrNoSystem }
func (NoSystem) CurrentIdentity() (Identity, error)           { return Identity{}, ErrNoSystem }
func (NoSystem) AllowedSudoersRules() map[string]bool         { return map[string]bool{} }
func (NoSystem) ListHolds([]string, bool) ([]Hold, error)     { return nil, ErrNoSystem }
func (NoSystem) Hold(string, []string) error                  { return ErrNoSystem }
func (NoSystem) Release(string, []string) error               { return ErrNoSystem }
func (NoSystem) ListPools() ([]*PoolListEntry, error)         { return nil, ErrNoSystem }
func (NoSystem) PoolStatus(string) (*PoolStatus, error)       { return nil, ErrNoSystem }
func (NoSystem) PoolStatuses() ([]*PoolStatus, error)         { return nil, ErrNoSystem }
func (NoSystem) PoolsWithoutHeadErrlog([]string) []string     { return nil }
func (NoSystem) RunPoolAction(PoolAction, string) error       { return ErrNoSystem }
func (NoSystem) RunDatasetCommand([]string) error             { return ErrNoSystem }
func (NoSystem) PoolFeatures(string) (*PoolFeatures, error)   { return nil, ErrNoSystem }
func (NoSystem) PreviewDestroySnapshots(string, []string, bool, bool) (*DestroyPreview, error) {
	return nil, ErrNoSystem
}
func (NoSystem) DataErrorStatuses() ([]*PoolStatus, []string, error) { return nil, nil, ErrNoSystem }
func (NoSystem) PoolHistory(context.Context, string, func(PoolHistoryProgress)) ([]PoolHistoryEntry, error) {
	return nil, ErrNoSystem
}
func (NoSystem) WatchEvents(context.Context, func(), func(), func(PoolHistoryEntry)) error {
	return ErrNoSystem
}
