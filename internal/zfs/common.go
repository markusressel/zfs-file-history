package zfs

import (
	"strings"
	"sync"
	"zfs-file-history/internal/util"

	golibzfs "github.com/kraudcloud/go-libzfs"
)

const (
	SnapshotTimeFormat = "2006-01-02-150405"
)

var (
	DatasetsLoaded = util.NewEmitter[struct{}]()
	// PermissionsChanged is emitted once permissions were delegated (and verified) by the application, e.g. in the
	// missing permissions dialog, so the displayed permissions are read again
	PermissionsChanged = util.NewEmitter[struct{}]()
	datasetCache       = make(map[string]*golibzfs.Dataset)
	// datasetByNameCache holds the libzfs handles opened by OpenDatasetByName, keyed by dataset name
	datasetByNameCache = make(map[string]*golibzfs.Dataset)
	cacheMtx           sync.RWMutex
)

func RefreshZfsData() {
	cacheMtx.Lock()
	datasetCache = make(map[string]*golibzfs.Dataset)
	datasetByNameCache = make(map[string]*golibzfs.Dataset)
	cacheMtx.Unlock()

	DatasetsLoaded.Emit(struct{}{})
}

func IsDatasetsLoaded() bool {
	return true
}

// findSnapshotHandles returns the libzfs handles of the snapshots with the given names (if found),
// from the snapshots loaded with the given dataset handle.
//
// libzfs handles are cached and shared between goroutines. golibzfs.Dataset.GetProperty writes the
// Properties map of a handle while holding golibzfs.Global.Mtx, but Snapshots() and findSnapshot read
// these maps without locking. Concurrent map reads and writes crash the Go runtime, so these reads
// must hold golibzfs.Global.Mtx as well. Nothing in here may call a golibzfs function that locks
// golibzfs.Global.Mtx itself (like GetProperty), as that would deadlock.
func findSnapshotHandles(dataset *golibzfs.Dataset, names []string) map[string]*golibzfs.Dataset {
	golibzfs.Global.Mtx.Lock()
	defer golibzfs.Global.Mtx.Unlock()

	result := make(map[string]*golibzfs.Dataset, len(names))
	snapshots, _ := dataset.Snapshots()
	if len(snapshots) == 0 {
		return result
	}
	for _, name := range names {
		if snapshot := findSnapshot(snapshots, name); snapshot != nil {
			result[name] = snapshot
		}
	}
	return result
}

// findSnapshot returns the snapshot with the given name.
// The caller must hold golibzfs.Global.Mtx, see findSnapshotHandles.
func findSnapshot(snapshots []golibzfs.Dataset, name string) *golibzfs.Dataset {
	for i := range snapshots {
		nameProperty := snapshots[i].Properties[golibzfs.DatasetPropName]
		parts := strings.Split(nameProperty.Value, "@")
		if len(parts) < 2 {
			continue
		}
		currentName := parts[1]
		if currentName == name {
			return &snapshots[i]
		}
	}
	return nil
}
