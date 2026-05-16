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

	"github.com/dustin/go-humanize"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

func createSnapshotBrowserTable(application *tview.Application) *table.RowSelectionTable[data.SnapshotBrowserEntry] {
	tableContainer := table.NewTableContainer[data.SnapshotBrowserEntry](
		application,
		createSnapshotBrowserTableCells,
		createSnapshotBrowserTableSortFunction,
	)
	return tableContainer
}

func createSnapshotBrowserTableCells(row int, columns []*table.Column, entry *data.SnapshotBrowserEntry) (cells []*tview.TableCell) {
	result := []*tview.TableCell{}
	for _, column := range columns {
		cellText := "N/A"
		cellAlign := tview.AlignLeft
		cellColor := tcell.ColorWhite
		switch column {
		case columnDate:
			cellText = entry.Snapshot.Properties.CreationDate.Format(theme.Style.Format.DateTime)
		case columnName:
			cellText = entry.Snapshot.Name
		case columnDiff:
			cellAlign = tview.AlignCenter
			switch entry.DiffState {
			case diff_state.Equal:
				cellText = "="
				cellColor = theme.Colors.SnapshotBrowser.Table.State.Equal
			case diff_state.Deleted:
				cellText = "+"
				cellColor = theme.Colors.SnapshotBrowser.Table.State.Deleted
			case diff_state.Added:
				cellText = "-"
				cellColor = theme.Colors.SnapshotBrowser.Table.State.Added
			case diff_state.Modified:
				cellText = "≠"
				cellColor = theme.Colors.SnapshotBrowser.Table.State.Modified
			case diff_state.Unknown:
				cellText = "N/A"
				cellColor = theme.Colors.SnapshotBrowser.Table.State.Unknown
			}
		case columnUsed:
			cellText = humanize.IBytes(entry.Snapshot.Properties.Used)
		case columnRefer:
			cellText = humanize.IBytes(entry.Snapshot.Properties.Referenced)
		case columnRatio:
			ratio := entry.Snapshot.Properties.CompressionRatio
			cellText = fmt.Sprintf("%.2fx", ratio)
		}
		cell := tview.NewTableCell(cellText).
			SetTextColor(cellColor).SetAlign(cellAlign)
		result = append(result, cell)
	}
	return result
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
			result = a.Snapshot.Properties.CreationDate.Compare(*b.Snapshot.Properties.CreationDate)
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
