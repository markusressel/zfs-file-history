package snapshot_browser

import (
	"zfs-file-history/internal/data"
	"zfs-file-history/internal/state"
	"zfs-file-history/internal/ui/shortcut_helper"
	"zfs-file-history/internal/ui/theme"
	"zfs-file-history/internal/zfs"

	"github.com/gdamore/tcell/v2"
)

// entryVersionStarts tells for the snapshots (by name) whether a new version of the selected entry starts in them, see
// data.NewVersions.
type entryVersionStarts struct {
	path       string
	newVersion map[string]bool
}

// changedInSnapshot returns whether something changed in the snapshot compared to the previous one, for what is
// looked at: a new version of the selected entry starts in it; without a selected entry, entries of the folder
// changed (if they are compared, see folder_changes.go); otherwise data was written to the dataset. known is false
// while it is determined. Must be called on the UI thread.
func (snapshotBrowser *SnapshotBrowserComponent) changedInSnapshot(snapshot *zfs.Snapshot) (changed bool, known bool) {
	if fileEntry := snapshotBrowser.currentFileEntry; fileEntry != nil {
		versions := snapshotBrowser.entryVersions
		if versions == nil || versions.path != fileEntry.GetRealPath() {
			return false, false
		}
		changed, known = versions.newVersion[snapshot.Name]
		return changed, known
	}
	if changes, ok := snapshotBrowser.changesOf(snapshot); ok {
		return changes.Exists && (changes.Initial || changes.VsPrevious.Total() > 0), true
	}
	return snapshot.Properties.Written > 0, true
}

// rowColor returns the text color of the row of a snapshot: bright if something changed in it (see
// changedInSnapshot), dimmed if it holds the same as the previous one.
func (snapshotBrowser *SnapshotBrowserComponent) rowColor(entry *data.SnapshotBrowserEntry) tcell.Color {
	if changed, known := snapshotBrowser.changedInSnapshot(entry.Snapshot); known && !changed {
		return theme.Colors.SnapshotBrowser.Table.Unchanged
	}
	return theme.Colors.SnapshotBrowser.Table.Changed
}

// setEntryVersions sets where new versions of the selected entry start, and updates the rows. Runs on the UI thread.
func (snapshotBrowser *SnapshotBrowserComponent) setEntryVersions(versions *entryVersionStarts) {
	snapshotBrowser.entryVersions = versions
	snapshotBrowser.changesUpdated()
}

// changesUpdated updates the rows after changedInSnapshot changed. Runs on the UI thread.
func (snapshotBrowser *SnapshotBrowserComponent) changesUpdated() {
	snapshotBrowser.tableContainer.RenderCells()
	if snapshotBrowser.onlyChanges {
		snapshotBrowser.tableContainer.RefreshFilter()
	}
}

// onlyChangesStateKey is the key of the "only changes" setting in state.Current, per page (see UseColumnLayout).
func (snapshotBrowser *SnapshotBrowserComponent) onlyChangesStateKey() string {
	return snapshotBrowser.layoutStateKey + ".onlyChanges"
}

// loadOnlyChanges applies the saved "only changes" setting. Must be called on the UI thread.
func (snapshotBrowser *SnapshotBrowserComponent) loadOnlyChanges() {
	snapshotBrowser.setOnlyChanges(state.Current.Toggle(snapshotBrowser.onlyChangesStateKey(), false))
}

// toggleOnlyChanges shows only the snapshots in which something changed, or all, and remembers the setting.
// Must be called on the UI thread.
func (snapshotBrowser *SnapshotBrowserComponent) toggleOnlyChanges() {
	snapshotBrowser.setOnlyChanges(!snapshotBrowser.onlyChanges)
	state.Current.SetToggle(snapshotBrowser.onlyChangesStateKey(), snapshotBrowser.onlyChanges)
}

func (snapshotBrowser *SnapshotBrowserComponent) setOnlyChanges(onlyChanges bool) {
	snapshotBrowser.onlyChanges = onlyChanges
	if !onlyChanges {
		snapshotBrowser.tableContainer.SetExtraFilter(nil)
	} else {
		snapshotBrowser.tableContainer.SetExtraFilter(func(entry *data.SnapshotBrowserEntry) bool {
			// shown while it is determined, so the list does not start empty
			changed, known := snapshotBrowser.changedInSnapshot(entry.Snapshot)
			return changed || !known
		})
	}
	snapshotBrowser.updateTitleAndFooter()
}

// onlyChangesShortcut is the shortcut that toggles the "only changes" setting.
func (snapshotBrowser *SnapshotBrowserComponent) onlyChangesShortcut() shortcut_helper.ShortcutEntry {
	name := "Only changes"
	if snapshotBrowser.onlyChanges {
		name = "All snapshots"
	}
	return shortcut_helper.ShortcutEntry{KeyCombo: []string{"v"}, Name: name, Group: shortcut_helper.GroupView}
}
