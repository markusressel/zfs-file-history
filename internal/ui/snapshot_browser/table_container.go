package snapshot_browser

import (
	"cmp"
	"fmt"
	"math/big"
	"sort"
	"strings"
	"time"
	"zfs-file-history/internal/data"
	"zfs-file-history/internal/data/diff_state"
	"zfs-file-history/internal/folder_listing"
	"zfs-file-history/internal/ui/table"
	"zfs-file-history/internal/ui/theme"
	"zfs-file-history/internal/ui/txwidgets"
	uiutil "zfs-file-history/internal/ui/util"
	"zfs-file-history/internal/zfs"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

func (snapshotBrowser *SnapshotBrowserComponent) createSnapshotBrowserTable(application *tview.Application) *table.RowSelectionTable[data.SnapshotBrowserEntry] {
	tableContainer := table.NewTableContainer[data.SnapshotBrowserEntry](
		application,
		snapshotBrowser.createSnapshotBrowserTableCells,
		snapshotBrowser.sortEntries,
	)
	return tableContainer
}

func (snapshotBrowser *SnapshotBrowserComponent) createSnapshotBrowserTableCells(row int, columns []*table.Column, entry *data.SnapshotBrowserEntry) (cells []*tview.TableCell) {
	result := []*tview.TableCell{}
	statusColor := determineStatusColor(entry)
	for _, column := range columns {
		cellText := "N/A"
		cellAlign := tview.AlignLeft
		cellColor := snapshotBrowser.rowColor(entry)
		switch column {
		case columnDate:
			cellText = uiutil.FormatTime(entry.Snapshot.Properties.CreationDate)
		case columnName:
			cellText = formatName(entry.Snapshot)
		case columnDiff:
			cellAlign = tview.AlignCenter
			if entry.IsLoading && snapshotBrowser.diffLoader != nil && snapshotBrowser.diffLoader.ShowLoadingSpinner() {
				cellText = "⟳"
				cellColor = tcell.ColorYellow
			} else if entry.IsLoading && entry.DiffState == diff_state.Unknown {
				// empty until it is known (or the spinner shows up), so it does not flash "?"
				cellText = ""
			} else {
				switch entry.DiffState {
				case diff_state.Equal:
					cellText = "="
					cellColor = theme.Colors.SnapshotBrowser.Table.State.Equal
				case diff_state.Deleted:
					cellText = "+"
					cellColor = theme.Colors.SnapshotBrowser.Table.State.SnapshotOnly
				case diff_state.Added:
					cellText = "-"
					cellColor = theme.Colors.SnapshotBrowser.Table.State.LocalOnly
				case diff_state.Modified:
					cellText = "≠"
					cellColor = theme.Colors.SnapshotBrowser.Table.State.Modified
				default:
					cellText = "?"
					cellColor = tcell.ColorGray
				}
			}
		case columnChange:
			cellText = snapshotBrowser.formatEntryChange(entry.Snapshot)
		case columnSize:
			cellText, cellColor = formatEntrySize(entry, cellColor)
		case columnModified:
			cellText, cellColor = formatEntryModified(entry, cellColor)
		case columnVsNow, columnChanges:
			cellText = snapshotBrowser.formatFolderChanges(entry.Snapshot, column == columnVsNow)
		case columnUsed:
			cellText = uiutil.StableLengthHumanizedBytes(entry.Snapshot.Properties.Used)
			cellColor = snapshotBrowser.usedScale.SizeColor(entry.Snapshot.Properties.Used, cellColor)
		case columnRefer:
			cellText = uiutil.StableLengthHumanizedBytes(entry.Snapshot.Properties.Referenced)
		case columnWritten:
			cellText = uiutil.StableLengthHumanizedBytes(entry.Snapshot.Properties.Written)
			cellColor = snapshotBrowser.writtenScale.SizeColor(entry.Snapshot.Properties.Written, cellColor)
		case columnRatio:
			ratio := entry.Snapshot.Properties.CompressionRatio
			cellText = fmt.Sprintf("%.2fx", ratio)
		case columnClones:
			cellText = fmt.Sprintf("%d", entry.Snapshot.Properties.Clones)
		case columnHolds:
			cellAlign = tview.AlignCenter
			cellText = formatHolds(entry.Snapshot.Properties.Holds)
		}
		cell := tview.NewTableCell(cellText).
			SetTextColor(cellColor).
			SetAlign(cellAlign)

		cell.SetSelectedStyle(
			tcell.StyleDefault.
				Foreground(theme.Colors.Layout.Table.SelectedForeground).
				Background(statusColor),
		)
		result = append(result, cell)
	}
	return result
}

// formatEntrySize returns the size of the selected file in the snapshot, or the number of items of the selected
// folder: empty without a selected entry, "—" if the snapshot does not contain it.
func formatEntrySize(entry *data.SnapshotBrowserEntry, color tcell.Color) (string, tcell.Color) {
	switch {
	case !entry.HasEntryInfo:
		return "", color
	case entry.EntryInfo == nil:
		return "—", theme.Colors.Layout.Table.ZeroSize
	case entry.EntryInfo.IsDir():
		items := folderItems(entry.EntryInfo.Size())
		return fmt.Sprintf("%d %s", items, uiutil.Plural(items, "item", "items")), color
	}
	return uiutil.StableLengthHumanizedBytes(uint64(max(entry.EntryInfo.Size(), 0))), color
}

// folderItems returns the number of direct entries of a folder from its size: on ZFS, the size of a directory is
// its number of entries, plus two for "." and "..". So it needs no listing of the folder.
func folderItems(size int64) int {
	return int(max(size-2, 0))
}

// formatEntryChange returns what happened to the selected entry in the snapshot, compared to the previous one, in
// the colors of the diff states: "+" created, "−" deleted, "≠ +1.2 KiB" modified (with the change of its size, or of
// the number of items of a folder), "initial" in the oldest snapshot, empty if it is unchanged or not known yet.
func (snapshotBrowser *SnapshotBrowserComponent) formatEntryChange(snapshot *zfs.Snapshot) string {
	change, ok := snapshotBrowser.entryChangeOf(snapshot)
	if !ok {
		return ""
	}
	colors := theme.Colors.FileBrowser.Table.State
	switch change.Kind {
	case data.VersionInitial:
		return txwidgets.Span(theme.Colors.Layout.Table.ZeroSize, "initial")
	case data.VersionCreated:
		return txwidgets.Span(colors.Added, "+")
	case data.VersionDeleted:
		return txwidgets.Span(colors.Deleted, "−")
	case data.VersionModified:
		text := "≠"
		if change.SizeDelta != 0 {
			text += " " + snapshotBrowser.formatSizeDelta(change.SizeDelta)
		}
		return txwidgets.Span(colors.Modified, "%s", text)
	}
	return ""
}

// formatSizeDelta returns a change of the size of the selected entry, e.g. "+1.2 KiB", or "-3 items" for a folder.
func (snapshotBrowser *SnapshotBrowserComponent) formatSizeDelta(delta int64) string {
	sign := "+"
	if delta < 0 {
		sign = "-"
	}
	magnitude := uint64(max(delta, -delta))
	if fileEntry := snapshotBrowser.currentFileEntry; fileEntry != nil && fileEntry.Type == data.Directory {
		return fmt.Sprintf("%s%d %s", sign, magnitude, uiutil.Plural(int(magnitude), "item", "items"))
	}
	return sign + strings.TrimSpace(uiutil.HumanizedBytes(magnitude))
}

// formatEntryModified returns the modification time of the selected file or folder in the snapshot, i.e. which
// version it holds: empty without a selected entry, "—" if the snapshot does not contain it.
func formatEntryModified(entry *data.SnapshotBrowserEntry, color tcell.Color) (string, tcell.Color) {
	switch {
	case !entry.HasEntryInfo:
		return "", color
	case entry.EntryInfo == nil:
		return "—", theme.Colors.Layout.Table.ZeroSize
	}
	return uiutil.FormatTime(entry.EntryInfo.ModTime()), color
}

// formatFolderChanges returns how the folder in the snapshot compares with now (vsNow) or the previous snapshot,
// e.g. "+2 −1 ~4" in the colors of the diff states: "…" while it is computed, "—" if the snapshot does not contain
// the folder, "=" (dimmed) without differences.
func (snapshotBrowser *SnapshotBrowserComponent) formatFolderChanges(snapshot *zfs.Snapshot, vsNow bool) string {
	dim := theme.Colors.Layout.Table.ZeroSize
	changes, ok := snapshotBrowser.changesOf(snapshot)
	switch {
	case !ok:
		return txwidgets.Span(dim, "…")
	case !changes.Exists:
		return txwidgets.Span(dim, "—")
	case !vsNow && changes.Initial:
		return txwidgets.Span(dim, "initial")
	}
	counts := changes.VsPrevious
	if vsNow {
		counts = changes.VsNow
	}
	if counts.Total() == 0 {
		return txwidgets.Span(dim, "=")
	}
	return uiutil.FormatChangeCounts(counts.Added, counts.Deleted, counts.Modified)
}

// heldMarker is shown in front of the names of held snapshots, like in the action dialog.
const heldMarker = "🔒 "

// formatName returns the text of the name column: the name, behind a lock if the snapshot is held, so held
// snapshots stand out without the holds column and independent of the selection.
func formatName(snapshot *zfs.Snapshot) string {
	if snapshot.Properties.Holds > 0 {
		return heldMarker + snapshot.Name
	}
	return snapshot.Name
}

// formatHolds returns the text of the holds column, empty without holds, so held snapshots stand out.
func formatHolds(holds uint64) string {
	if holds == 0 {
		return ""
	}
	return fmt.Sprintf("%d", holds)
}

func determineStatusColor(entry *data.SnapshotBrowserEntry) tcell.Color {
	switch entry.DiffState {
	case diff_state.Equal:
		return theme.Colors.SnapshotBrowser.Table.State.Equal
	case diff_state.Deleted:
		return theme.Colors.SnapshotBrowser.Table.State.SnapshotOnly
	case diff_state.Added:
		return theme.Colors.SnapshotBrowser.Table.State.LocalOnly
	case diff_state.Modified:
		return theme.Colors.SnapshotBrowser.Table.State.Modified
	case diff_state.Unknown:
		fallthrough
	default:
		return theme.Colors.SnapshotBrowser.Table.State.Unknown
	}
}

func createSnapshotBrowserTableSortFunction(entries []*data.SnapshotBrowserEntry, columnToSortBy *table.Column, inverted bool) []*data.SnapshotBrowserEntry {
	return sortSnapshotEntries(entries, columnToSortBy, inverted, sortLookups{})
}

// sortEntries sorts the entries of the table, also by the changes of the folder and the selected entry.
func (snapshotBrowser *SnapshotBrowserComponent) sortEntries(entries []*data.SnapshotBrowserEntry, column *table.Column, inverted bool) []*data.SnapshotBrowserEntry {
	return sortSnapshotEntries(entries, column, inverted, sortLookups{
		folderChanges: snapshotBrowser.changesOf,
		entryChange:   snapshotBrowser.entryChangeOf,
	})
}

// sortLookups return what the snapshots are sorted by that is not part of the entries (nil: unknown).
type sortLookups struct {
	folderChanges func(*zfs.Snapshot) (folder_listing.SnapshotChanges, bool)
	entryChange   func(*zfs.Snapshot) (data.VersionChange, bool)
}

// entryChangeWeight orders the changes of the selected entry for sorting: unknown first, then unchanged, then by
// how much the size changed (created, deleted and modified alike, the oldest snapshot as the smallest change).
func (lookups sortLookups) entryChangeWeight(entry *data.SnapshotBrowserEntry) int64 {
	if lookups.entryChange == nil {
		return -1
	}
	change, ok := lookups.entryChange(entry.Snapshot)
	switch {
	case !ok:
		return -1
	case !change.IsNewVersion():
		return 0
	}
	return 1 + max(change.SizeDelta, -change.SizeDelta)
}

// entrySize is the size of the selected file in the snapshot for sorting (the number of items of a folder, see
// folderItems), -1 if unknown.
func entrySize(entry *data.SnapshotBrowserEntry) int64 {
	if entry.EntryInfo == nil {
		return -1
	}
	return entry.EntryInfo.Size()
}

// entryModified is the modification time of the selected entry in the snapshot for sorting, zero if unknown.
func entryModified(entry *data.SnapshotBrowserEntry) time.Time {
	if entry.EntryInfo == nil {
		return time.Time{}
	}
	return entry.EntryInfo.ModTime()
}

// sortSnapshotEntries sorts the entries by the column. changesOf returns the folder changes of a snapshot (nil: unknown).
func sortSnapshotEntries(entries []*data.SnapshotBrowserEntry, columnToSortBy *table.Column, inverted bool, lookups sortLookups) []*data.SnapshotBrowserEntry {
	// changesTotal is the number of changed entries of the folder for sorting, -1 if unknown
	changesTotal := func(entry *data.SnapshotBrowserEntry, vsNow bool) int {
		if lookups.folderChanges == nil {
			return -1
		}
		changes, ok := lookups.folderChanges(entry.Snapshot)
		if !ok || !changes.Exists {
			return -1
		}
		if vsNow {
			return changes.VsNow.Total()
		}
		return changes.VsPrevious.Total()
	}
	sort.SliceStable(entries, func(i, j int) bool {
		a := entries[i]
		b := entries[j]

		result := 0
		switch columnToSortBy {
		case columnName:
			result = strings.Compare(strings.ToLower(a.Snapshot.Name), strings.ToLower(b.Snapshot.Name))
		case columnDate:
			result = a.Snapshot.Properties.CreationDate.Compare(b.Snapshot.Properties.CreationDate)
		case columnDiff:
			result = int(b.DiffState - a.DiffState)
		case columnUsed:
			result = int(b.Snapshot.Properties.Used - a.Snapshot.Properties.Used)
		case columnRefer:
			result = int(b.Snapshot.Properties.Referenced - a.Snapshot.Properties.Referenced)
		case columnChange:
			// the largest changes first, like the sizes
			result = cmp.Compare(lookups.entryChangeWeight(b), lookups.entryChangeWeight(a))
		case columnSize:
			result = cmp.Compare(entrySize(b), entrySize(a))
		case columnModified:
			result = entryModified(a).Compare(entryModified(b))
		case columnVsNow:
			result = cmp.Compare(changesTotal(a, true), changesTotal(b, true))
		case columnChanges:
			result = cmp.Compare(changesTotal(a, false), changesTotal(b, false))
		case columnWritten:
			// the largest first, like the other sizes
			result = cmp.Compare(b.Snapshot.Properties.Written, a.Snapshot.Properties.Written)
		case columnRatio:
			ratioA := a.Snapshot.Properties.CompressionRatio
			ratioB := b.Snapshot.Properties.CompressionRatio
			result = big.NewFloat(ratioA).Cmp(big.NewFloat(ratioB))
		case columnHolds:
			result = cmp.Compare(a.Snapshot.Properties.Holds, b.Snapshot.Properties.Holds)
		case columnClones:
			clonesA := a.Snapshot.Properties.Clones
			clonesB := b.Snapshot.Properties.Clones
			switch {
			case clonesA < clonesB:
				result = -1
			case clonesA > clonesB:
				result = 1
			default:
				result = 0
			}
		}
		if inverted {
			result *= -1
		}

		if result <= 0 {
			return true
		} else {
			return false
		}
	})
	return entries
}
