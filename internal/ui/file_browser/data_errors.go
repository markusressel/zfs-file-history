package file_browser

import (
	"fmt"
	"zfs-file-history/internal/data"
	"zfs-file-history/internal/ui/theme"
	uiutil "zfs-file-history/internal/ui/util"
	"zfs-file-history/internal/zfs"

	"github.com/gdamore/tcell/v2"
)

// damagedMarker marks damaged files, and folders that contain some, behind their name, see formatDamage.
const damagedMarker = "⚠"

// SetDataErrors shows which files are damaged (nil: none), see zfs.ListDataErrors. Must be called on the UI thread.
func (fileBrowser *FileBrowserComponent) SetDataErrors(index *zfs.DataErrorIndex) {
	fileBrowser.dataErrors = index
	fileBrowser.tableContainer.RenderCells()
}

// formatDamage returns the note behind the name of an entry that is damaged (in the file system or in snapshots),
// or of a folder that contains damaged files, and its color: the error color if the file in the file system is
// damaged, else the warning color. Returns false if there is nothing to note.
func formatDamage(index *zfs.DataErrorIndex, entry *data.FileBrowserEntry) (string, tcell.Color, bool) {
	path := entry.GetRealPath()
	colors := theme.Colors.Alert
	live, inSnapshots := 0, 0
	for _, dataError := range index.At(path) {
		if dataError.Snapshot == "" {
			live++
		} else {
			inSnapshots++
		}
	}
	switch {
	case live > 0 && inSnapshots > 0:
		return fmt.Sprintf("%s damaged, also in %d %s", damagedMarker, inSnapshots, uiutil.Plural(inSnapshots, "snapshot", "snapshots")), colors.Error, true
	case live > 0:
		return damagedMarker + " damaged", colors.Error, true
	case inSnapshots > 0:
		return fmt.Sprintf("%s damaged in %d %s", damagedMarker, inSnapshots, uiutil.Plural(inSnapshots, "snapshot", "snapshots")), colors.Warning, true
	}
	if isDirectory(entry) {
		if below := index.DamagedBelow(path); below > 0 {
			return fmt.Sprintf("%s %d damaged inside", damagedMarker, below), colors.Warning, true
		}
	}
	return "", tcell.ColorDefault, false
}

// isDirectory returns whether the entry is a folder (in the file system, or in a snapshot if it was deleted).
func isDirectory(entry *data.FileBrowserEntry) bool {
	if stat := entry.GetStat(); stat != nil {
		return stat.IsDir()
	}
	return entry.Type == data.Directory
}
