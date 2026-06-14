package file_browser

import (
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"zfs-file-history/internal/configuration"
	"zfs-file-history/internal/data"
	"zfs-file-history/internal/data/diff_state"
	"zfs-file-history/internal/ui/table"
	"zfs-file-history/internal/ui/theme"
	uiutil "zfs-file-history/internal/ui/util"
	"zfs-file-history/internal/util"

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
	statusCellText := determineStatusIndicator(entry)
	statusCellColor := determineStatusColor(entry)
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
			stat := entry.GetStat()
			if stat != nil && stat.IsDir() {
				cellText = fmt.Sprintf("/%s", cellText)
			} else if entry.Type == data.Directory {
				cellText = fmt.Sprintf("/%s", cellText)
			}
			cellColor = statusCellColor
		case columnType:
			cellText = typeCellText
			cellColor = typeCellColor
			cellAlignment = tview.AlignCenter
		case columnDiff:
			cellText = statusCellText
			cellColor = statusCellColor
			cellAlignment = tview.AlignCenter
		case columnPermissions:
			cellText = determinePermissionsText(entry)
			cellColor = tcell.ColorGray
		case columnUID:
			cellText = determineUIDText(entry)
			cellColor = tcell.ColorGray
		case columnGID:
			cellText = determineGIDText(entry)
			cellColor = tcell.ColorGray
		case columnDateTime:
			cellText = entry.GetStat().ModTime().Format(theme.Style.Format.DateTime)
			switch entry.DiffState {
			case diff_state.Added, diff_state.Deleted:
				cellColor = statusCellColor
			case diff_state.Modified:
				if entry.RealFile.Stat.ModTime() != entry.SnapshotFiles[0].Stat.ModTime() {
					cellColor = statusCellColor
				} else {
					cellColor = tcell.ColorWhite
				}
			default:
				cellColor = tcell.ColorGray
			}
		case columnSize:
			cellText = uiutil.StableLengthHumanizedBytes(uint64(entry.GetStat().Size()))
			switch entry.DiffState {
			case diff_state.Added, diff_state.Deleted:
				cellColor = statusCellColor
			case diff_state.Modified:
				if entry.RealFile.Stat.Size() != entry.SnapshotFiles[0].Stat.Size() {
					cellColor = statusCellColor
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

		// Keep row statusCellText visible while selected by using statusCellColor as selected background.
		// If status is unknown, use default selected background to avoid 'flash'.
		bg := statusCellColor
		if entry.DiffState == diff_state.Unknown {
			bg = theme.Colors.Layout.Table.SelectedBackground
		}

		cell.SetSelectedStyle(
			tcell.StyleDefault.
				Foreground(theme.Colors.Layout.Table.SelectedForeground).
				Background(bg),
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

func determinePermissionsText(entry *data.FileBrowserEntry) string {
	stat := entry.GetStat()
	if stat == nil {
		return "Loading..."
	}
	permissionsMode := configuration.CurrentConfig.FileBrowser.Permissions
	if permissionsMode == configuration.FileBrowserPermissionsFormatSymbolic {
		return util.UnixPermSymbolic(stat.Mode())
	}

	return fmt.Sprintf("%04o", util.UnixPermissions(stat.Mode()))
}

func determineUIDText(entry *data.FileBrowserEntry) string {
	stat := entry.GetStat()
	if stat == nil {
		return "Loading..."
	}
	uid, _, ok := util.UnixOwnerIDs(stat)
	if !ok {
		return "N/A"
	}

	return formatIdentity(uid, util.LookupUserName)
}

func determineGIDText(entry *data.FileBrowserEntry) string {
	stat := entry.GetStat()
	if stat == nil {
		return "Loading..."
	}
	_, gid, ok := util.UnixOwnerIDs(stat)
	if !ok {
		return "N/A"
	}

	return formatIdentity(gid, util.LookupGroupName)
}

func formatIdentity(id uint32, lookupName func(uint32) (string, error)) string {
	idStr := strconv.FormatUint(uint64(id), 10)
	displayMode := configuration.CurrentConfig.FileBrowser.Owner

	name, err := lookupName(id)
	if err != nil {
		if displayMode == configuration.FileBrowserOwnerFormatName {
			return idStr
		}
		return idStr
	}

	switch displayMode {
	case configuration.FileBrowserOwnerFormatName:
		return name
	case configuration.FileBrowserOwnerFormatBoth:
		return fmt.Sprintf("%s(%s)", idStr, name)
	case configuration.FileBrowserOwnerFormatID:
		fallthrough
	default:
		return idStr
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
			result = compareWithMissingStats(a, b, func(statA, statB os.FileInfo) int {
				return statA.ModTime().Compare(statB.ModTime())
			})
		case columnType:
			result = int(b.Type - a.Type)
		case columnSize:
			result = compareWithMissingStats(a, b, func(statA, statB os.FileInfo) int {
				return int(statA.Size() - statB.Size())
			})
		case columnDiff:
			result = int(b.DiffState - a.DiffState)
		case columnPermissions:
			result = compareWithMissingStats(a, b, func(statA, statB os.FileInfo) int {
				permA := util.UnixPermissions(statA.Mode())
				permB := util.UnixPermissions(statB.Mode())
				switch {
				case permA < permB:
					return -1
				case permA > permB:
					return 1
				default:
					return 0
				}
			})
		case columnUID:
			result = compareWithMissingStats(a, b, func(statA, statB os.FileInfo) int {
				uidA, _, okA := util.UnixOwnerIDs(statA)
				uidB, _, okB := util.UnixOwnerIDs(statB)
				return compareUint32WithMissing(uidA, okA, uidB, okB)
			})
		case columnGID:
			result = compareWithMissingStats(a, b, func(statA, statB os.FileInfo) int {
				_, gidA, okA := util.UnixOwnerIDs(statA)
				_, gidB, okB := util.UnixOwnerIDs(statB)
				return compareUint32WithMissing(gidA, okA, gidB, okB)
			})
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

func compareWithMissingStats(a, b *data.FileBrowserEntry, compareFunc func(statA, statB os.FileInfo) int) int {
	statA := a.GetStat()
	statB := b.GetStat()
	if statA != nil && statB != nil {
		return compareFunc(statA, statB)
	} else if statA == nil && statB != nil {
		return -1
	} else if statA != nil && statB == nil {
		return 1
	}
	return 0
}

func compareUint32WithMissing(a uint32, okA bool, b uint32, okB bool) int {
	if !okA && !okB {
		return 0
	}
	if !okA {
		return -1
	}
	if !okB {
		return 1
	}

	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	default:
		return 0
	}
}
