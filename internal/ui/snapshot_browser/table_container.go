package snapshot_browser

import (
	"fmt"
	"math/big"
	"sort"
	"strings"
	"zfs-file-history/internal/data"
	"zfs-file-history/internal/data/diff_state"
	"zfs-file-history/internal/ui/table"
	"zfs-file-history/internal/ui/theme"
	uiutil "zfs-file-history/internal/ui/util"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

func (snapshotBrowser *SnapshotBrowserComponent) createSnapshotBrowserTable(application *tview.Application) *table.RowSelectionTable[data.SnapshotBrowserEntry] {
	tableContainer := table.NewTableContainer[data.SnapshotBrowserEntry](
		application,
		snapshotBrowser.createSnapshotBrowserTableCells,
		createSnapshotBrowserTableSortFunction,
	)
	return tableContainer
}

func (snapshotBrowser *SnapshotBrowserComponent) createSnapshotBrowserTableCells(row int, columns []*table.Column, entry *data.SnapshotBrowserEntry) (cells []*tview.TableCell) {
	result := []*tview.TableCell{}
	statusColor := determineStatusColor(entry)
	for _, column := range columns {
		cellText := "N/A"
		cellAlign := tview.AlignLeft
		cellColor := determineBaseTextColor(entry)
		switch column {
		case columnDate:
			cellText = entry.Snapshot.Properties.CreationDate.Format(theme.Style.Format.DateTime)
		case columnName:
			cellText = entry.Snapshot.Name
		case columnDiff:
			cellAlign = tview.AlignCenter
			if entry.IsLoading && snapshotBrowser.diffLoader != nil && snapshotBrowser.diffLoader.ShowLoadingSpinner() {
				cellText = "⟳"
				cellColor = tcell.ColorYellow
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
		case columnUsed:
			cellText = uiutil.StableLengthHumanizedBytes(entry.Snapshot.Properties.Used)
		case columnRefer:
			cellText = uiutil.StableLengthHumanizedBytes(entry.Snapshot.Properties.Referenced)
		case columnRatio:
			ratio := entry.Snapshot.Properties.CompressionRatio
			cellText = fmt.Sprintf("%.2fx", ratio)
		case columnClones:
			cellText = fmt.Sprintf("%d", entry.Snapshot.Properties.Clones)
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

func determineBaseTextColor(entry *data.SnapshotBrowserEntry) tcell.Color {
	switch entry.DiffState {
	case diff_state.Deleted:
		fallthrough
	case diff_state.Added:
		fallthrough
	case diff_state.Modified:
		return tcell.ColorWhite
	default:
		return tcell.ColorGray
	}
}

func createSnapshotBrowserTableSortFunction(entries []*data.SnapshotBrowserEntry, columnToSortBy *table.Column, inverted bool) []*data.SnapshotBrowserEntry {
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
		case columnRatio:
			ratioA := a.Snapshot.Properties.CompressionRatio
			ratioB := b.Snapshot.Properties.CompressionRatio
			result = big.NewFloat(ratioA).Cmp(big.NewFloat(ratioB))
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
