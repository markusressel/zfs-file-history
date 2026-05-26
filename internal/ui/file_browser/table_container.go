package file_browser

import (
	"fmt"
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

func createFileBrowserTable(application *tview.Application) *table.RowSelectionTable[data.FileBrowserEntry] {
	tableContainer := table.NewTableContainer[data.FileBrowserEntry](
		application,
		fileBrowserEntryTableCellsFunction,
		fileBrowserEntrySortFunction,
	)
	return tableContainer
}

func fileBrowserEntryTableCellsFunction(row int, columns []*table.Column, entry *data.FileBrowserEntry) (cells []*tview.TableCell) {
	status := determineStatusIndicator(entry)
	statusColor := determineStatusColor(entry)
	typeCellText := determineTypeCellText(entry)
	typeCellColor := determineTypeCellColor(entry)

	for _, column := range columns {
		var cellColor = tcell.ColorWhite
		var cellText string
		var cellAlignment = tview.AlignLeft
		var cellExpansion = 0

		switch column {
		case columnName:
			cellText = entry.Name
			if entry.GetStat().IsDir() {
				cellText = fmt.Sprintf("/%s", cellText)
			}
			cellColor = statusColor
		case columnType:
			cellText = typeCellText
			cellColor = typeCellColor
			cellAlignment = tview.AlignCenter
		case columnDiff:
			cellText = status
			cellColor = statusColor
			cellAlignment = tview.AlignCenter
		case columnDateTime:
			cellText = entry.GetStat().ModTime().Format(theme.Style.Format.DateTime)
			switch entry.DiffState {
			case diff_state.Added, diff_state.Deleted:
				cellColor = statusColor
			case diff_state.Modified:
				if entry.RealFile.Stat.ModTime() != entry.SnapshotFiles[0].Stat.ModTime() {
					cellColor = statusColor
				} else {
					cellColor = tcell.ColorWhite
				}
			default:
				cellColor = tcell.ColorGray
			}
		case columnSize:
			cellText = humanize.IBytes(uint64(entry.GetStat().Size()))
			if strings.HasSuffix(cellText, " B") {
				withoutSuffix := strings.TrimSuffix(cellText, " B")
				cellText = fmt.Sprintf("%s   B", withoutSuffix)
			}
			if len(cellText) < 10 {
				cellText = fmt.Sprintf("%s%s", strings.Repeat(" ", 10-len(cellText)), cellText)
			}
			switch entry.DiffState {
			case diff_state.Added, diff_state.Deleted:
				cellColor = statusColor
			case diff_state.Modified:
				if entry.RealFile.Stat.Size() != entry.SnapshotFiles[0].Stat.Size() {
					cellColor = statusColor
				} else {
					cellColor = tcell.ColorWhite
				}
			default:
				cellColor = tcell.ColorGray
			}
			cellAlignment = tview.AlignRight
		default:
			panic("Unknown column")
		}

		cell := tview.NewTableCell(cellText).
			SetTextColor(cellColor).
			SetAlign(cellAlignment).
			SetExpansion(cellExpansion)

		// Keep row status visible while selected by using statusColor as selected background.
		cell.SetSelectedStyle(
			tcell.StyleDefault.
				Foreground(theme.Colors.Layout.Table.SelectedForeground).
				Background(statusColor),
		)
		cells = append(cells, cell)
	}

	return cells
}

func determineTypeCellColor(entry *data.FileBrowserEntry) tcell.Color {
	switch entry.Type {
	case data.Directory:
		return theme.Colors.Layout.Table.Header
	case data.Link:
		return tcell.ColorYellow
	case data.File:
		fallthrough
	default:
		return tcell.ColorGray
	}
}

func determineTypeCellText(entry *data.FileBrowserEntry) string {
	switch entry.Type {
	case data.File:
		return "F"
	case data.Directory:
		return "D"
	case data.Link:
		return "L"
	default:
		return "?"
	}
}

func determineStatusIndicator(entry *data.FileBrowserEntry) string {
	switch entry.DiffState {
	case diff_state.Equal:
		return "="
	case diff_state.Deleted:
		return "-"
	case diff_state.Added:
		return "+"
	case diff_state.Modified:
		return "≠"
	case diff_state.Unknown:
		fallthrough
	default:
		return "N/A"
	}
}

func determineStatusColor(entry *data.FileBrowserEntry) tcell.Color {
	switch entry.DiffState {
	case diff_state.Equal:
		return theme.Colors.FileBrowser.Table.State.Equal
	case diff_state.Deleted:
		return theme.Colors.FileBrowser.Table.State.Deleted
	case diff_state.Added:
		return theme.Colors.FileBrowser.Table.State.Added
	case diff_state.Modified:
		return theme.Colors.FileBrowser.Table.State.Modified
	case diff_state.Unknown:
		fallthrough
	default:
		return theme.Colors.FileBrowser.Table.State.Unknown
	}
}

func fileBrowserEntrySortFunction(entries []*data.FileBrowserEntry, columnToSortBy *table.Column, inverted bool) []*data.FileBrowserEntry {
	sort.SliceStable(entries, func(i, j int) bool {
		a := entries[i]
		b := entries[j]

		result := 0
		switch columnToSortBy {
		case columnName:
			result = strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name))
		case columnDateTime:
			result = a.GetStat().ModTime().Compare(b.GetStat().ModTime())
		case columnType:
			result = int(b.Type - a.Type)
		case columnSize:
			result = int(a.GetStat().Size() - b.GetStat().Size())
		case columnDiff:
			result = int(b.DiffState - a.DiffState)
		}

		if inverted {
			result *= -1
		}

		if result != 0 {
			if result <= 0 {
				return true
			} else {
				return false
			}
		}

		result = int(b.Type - a.Type)
		if result != 0 {
			if result <= 0 {
				return true
			} else {
				return false
			}
		}

		result = strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name))
		if result != 0 {
			if result <= 0 {
				return true
			} else {
				return false
			}
		}

		if result <= 0 {
			return true
		} else {
			return false
		}
	})
	return entries
}
