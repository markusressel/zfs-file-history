package snapshot_browser

import (
	"zfs-file-history/internal/data"
	"zfs-file-history/internal/state"
	"zfs-file-history/internal/ui/shortcut_helper"
	"zfs-file-history/internal/ui/theme"
	"zfs-file-history/internal/zfs"

	"github.com/gdamore/tcell/v2"
)

// entryVersionStarts tells for the snapshots (by name) what happened to the selected entry in them, compared to the
// previous snapshot (see data.VersionChanges): where new versions start.
type entryVersionStarts struct {
	path    string
	changes map[string]data.VersionChange
}

// changedInSnapshot returns whether something changed in the snapshot compared to the previous one, for what is
// looked at: a new version of the selected entry starts in it; without a selected entry, entries of the folder
// changed (if they are compared, see folder_changes.go); otherwise data was written to the dataset. known is false
// while it is determined. Must be called on the UI thread.
func (snapshotBrowser *SnapshotBrowserComponent) changedInSnapshot(snapshot *zfs.Snapshot) (changed bool, known bool) {
	if snapshotBrowser.currentFileEntry != nil {
		change, known := snapshotBrowser.entryChangeOf(snapshot)
		return change.IsNewVersion(), known
	}
	if changes, ok := snapshotBrowser.changesOf(snapshot); ok {
		return changes.Exists && (changes.Initial || changes.VsPrevious.Total() > 0), true
	}
	return snapshot.Properties.Written > 0, true
}

// rowColor returns the text color of the row of a snapshot: bright if something changed in it (see
// changedInSnapshot), dimmed if it holds the same as the previous one. Dimmed as well while it is determined, so
// moving through the files only brightens the rows that changed, instead of all rows flashing.
func (snapshotBrowser *SnapshotBrowserComponent) rowColor(entry *data.SnapshotBrowserEntry) tcell.Color {
	if changed, _ := snapshotBrowser.changedInSnapshot(entry.Snapshot); changed {
		return theme.Colors.SnapshotBrowser.Table.Changed
	}
	return theme.Colors.SnapshotBrowser.Table.Unchanged
}

// entryChangeOf returns what happened to the selected entry in the snapshot, false while it is determined (or
// without a selected entry). Must be called on the UI thread.
func (snapshotBrowser *SnapshotBrowserComponent) entryChangeOf(snapshot *zfs.Snapshot) (data.VersionChange, bool) {
	fileEntry, versions := snapshotBrowser.currentFileEntry, snapshotBrowser.entryVersions
	if fileEntry == nil || versions == nil || versions.path != fileEntry.GetRealPath() {
		return data.VersionChange{}, false
	}
	change, ok := versions.changes[snapshot.Name]
	return change, ok
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
