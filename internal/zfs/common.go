package zfs

import (
	"zfs-file-history/internal/util"
)

const (
	SnapshotTimeFormat = "2006-01-02-150405"
)

var (
	DatasetsLoaded = util.NewEmitter[struct{}]()
	// PermissionsChanged is emitted once permissions were delegated (and verified) by the application, e.g. in the
	// missing permissions dialog, so the displayed permissions are read again
	PermissionsChanged = util.NewEmitter[struct{}]()
	// PoolsChanged is emitted when the state of a pool changed (e.g. a device failed, or a scrub finished), see
	// WatchZpoolEvents. The datasets did not change, unlike with DatasetsLoaded.
	PoolsChanged = util.NewEmitter[struct{}]()
	// PoolHistoryRecorded is emitted with each event ZFS logged in the history of a pool while the application runs
	// (see WatchZpoolEvents), from the goroutine of the watcher. Commands are not among them: ZFS reports only its
	// own events.
	PoolHistoryRecorded = util.NewEmitter[PoolHistoryEntry]()
)

// RefreshZfsData drops what the backend cached and emits DatasetsLoaded, so everything is loaded again.
func RefreshZfsData() {
	backend.Reload()
	DatasetsLoaded.Emit(struct{}{})
}

func IsDatasetsLoaded() bool {
	return true
}
