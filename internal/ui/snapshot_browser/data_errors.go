package snapshot_browser

import (
	"fmt"
	"zfs-file-history/internal/zfs"
)

// damagedMarker marks the snapshot that holds a damaged version of the selected entry, see formatDamaged.
const damagedMarker = "⚠"

// SetDataErrors shows which files are damaged in which snapshots (nil: none), see zfs.ListDataErrors. Must be called
// on the UI thread.
func (snapshotBrowser *SnapshotBrowserComponent) SetDataErrors(index *zfs.DataErrorIndex) {
	snapshotBrowser.dataErrors = index
	if snapshotBrowser.tableContainer.IsSortedBy(columnDamaged) {
		snapshotBrowser.tableContainer.Resort()
	}
	snapshotBrowser.tableContainer.RenderCells()
}

// formatDamaged returns the text of the damaged column: the number of damaged files and objects in the snapshot,
// with the marker if the version of the selected entry in it is one of them (e.g. "⚠ 3"), empty if none. A snapshot
// holds the damaged blocks of its files until it is destroyed, also after the files were deleted or restored.
func (snapshotBrowser *SnapshotBrowserComponent) formatDamaged(snapshot *zfs.Snapshot) string {
	count := snapshotBrowser.dataErrors.DamagedInSnapshot(snapshot.FullName)
	if count == 0 {
		return ""
	}
	if entry := snapshotBrowser.currentFileEntry; entry != nil && snapshotBrowser.dataErrors.IsDamaged(entry.GetRealPath(), snapshot.FullName) {
		return fmt.Sprintf("%s %d", damagedMarker, count)
	}
	return fmt.Sprintf("%d", count)
}
