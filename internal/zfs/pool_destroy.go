package zfs

import (
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
)

// PoolActionDestroy destroys the pool with all its data ("zpool destroy", never "-f": a dataset in use stops it
// before anything is destroyed). It cannot be undone, so it is only run by DestroyPool, with a confirmed preview.
// It is not part of PoolActions, so the sudoers rule of "setup --pool-actions" never allows it.
var PoolActionDestroy = PoolAction{Name: "destroy the pool", Done: "Destroyed the pool", Args: []string{"destroy"}}

// PoolDestroyPreview is what destroying a pool loses, see PreviewDestroyPool.
type PoolDestroyPreview struct {
	// Pool is the name of the pool.
	Pool string
	// Datasets are the full names of all filesystems and volumes of the pool, sorted, so its root dataset comes
	// first. Their snapshots are destroyed as well.
	Datasets []string
	// Allocated is the space in bytes the pool takes on its devices.
	Allocated uint64
}

// PreviewDestroyPool returns what destroying the pool with the given name would destroy, without changing anything.
// It refuses pools the running system is mounted from (see protectedMountPaths).
// This spawns zfs processes, so it must not be called on the UI thread.
func PreviewDestroyPool(pool string) (*PoolDestroyPreview, error) {
	const action = "destroy the pool"
	if pool == "" || strings.HasPrefix(pool, "-") || strings.ContainsAny(pool, "/@# ") {
		return nil, fmt.Errorf("cannot %s: invalid pool name %q", action, pool)
	}
	pools, err := backend.ListPools()
	if err != nil {
		return nil, err
	}
	index := slices.IndexFunc(pools, func(entry *PoolListEntry) bool { return entry.Name == pool })
	if index < 0 {
		return nil, fmt.Errorf("cannot %s: pool '%s' does not exist", action, pool)
	}
	datasets, err := listAllDatasetsOfPool(pool)
	if err != nil {
		return nil, err
	}
	if err := refuseSystemMounts(datasets, action); err != nil {
		return nil, err
	}
	return &PoolDestroyPreview{Pool: pool, Datasets: datasets, Allocated: pools[index].Allocated}, nil
}

// listAllDatasetsOfPool returns the full names of all filesystems and volumes of the pool, sorted, one level of
// children per zfs process.
func listAllDatasetsOfPool(pool string) ([]string, error) {
	seen := map[string]bool{pool: true}
	parents := []string{pool}
	for len(parents) > 0 {
		entries, err := backend.ListChildDatasets(parents)
		if err != nil {
			return nil, err
		}
		var children []string
		for _, entry := range entries {
			if !seen[entry.Name] && isWithinDataset(entry.Name, pool) {
				seen[entry.Name] = true
				children = append(children, entry.Name)
			}
		}
		parents = children
	}
	return slices.Sorted(maps.Keys(seen)), nil
}

// DestroyPool destroys the pool exactly as confirmed, a preview of PreviewDestroyPool ("zpool destroy"). Returns
// ErrChangedSinceConfirmation if its datasets changed since the preview, e.g. one was created in between. Returns
// a PoolActionNeedsRootError if zpool and sudo denied it (see RunPoolAction).
// This spawns processes, so it must not be called on the UI thread.
func DestroyPool(confirmed *PoolDestroyPreview) error {
	if confirmed == nil {
		return errors.New("cannot destroy the pool: not confirmed")
	}
	preview, err := PreviewDestroyPool(confirmed.Pool)
	if err != nil {
		return err
	}
	if !sameNames(preview.Datasets, confirmed.Datasets) {
		return ErrChangedSinceConfirmation
	}
	return backend.RunPoolAction(PoolActionDestroy, preview.Pool)
}
