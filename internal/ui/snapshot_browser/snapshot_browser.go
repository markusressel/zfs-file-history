package snapshot_browser

import (
	"fmt"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"golang.org/x/exp/slices"
	"sort"
	"strings"
	"zfs-file-history/internal/data"
	"zfs-file-history/internal/data/diff_state"
	"zfs-file-history/internal/logging"
	"zfs-file-history/internal/ui/table"
	"zfs-file-history/internal/util"
	"zfs-file-history/internal/zfs"
)

type SelectionInfo[T any] struct {
	Index int
	Entry *T
}

type SnapshotBrowserEntry struct {
	Snapshot  *zfs.Snapshot
	DiffState diff_state.DiffState
}

type SnapshotBrowserComponent struct {
	application *tview.Application

	tableContainer *table.RowSelectionTable[SnapshotBrowserEntry]

	path             string
	hostDataset      *zfs.Dataset
	currentFileEntry *data.FileBrowserEntry

	selectedSnapshotMap map[string]*SelectionInfo[SnapshotBrowserEntry]

	selectedSnapshotChangedCallback func(snapshot *SnapshotBrowserEntry)
}

var (
	columnName = &table.Column{
		Id:        0,
		Title:     "Name",
		Alignment: tview.AlignLeft,
	}
	columnDate = &table.Column{
		Id:        1,
		Title:     "Date",
		Alignment: tview.AlignLeft,
	}
	columnDiff = &table.Column{
		Id:        2,
		Title:     "Diff",
		Alignment: tview.AlignCenter,
	}
	tableColumns = []*table.Column{
		columnName, columnDate, columnDiff,
	}
)

func NewSnapshotBrowser(application *tview.Application) *SnapshotBrowserComponent {
	toTableCellsFunction := func(row int, columns []*table.Column, entry *SnapshotBrowserEntry) (cells []*tview.TableCell) {
		result := []*tview.TableCell{}
		for _, column := range columns {
			cellText := "N/A"
			cellAlign := tview.AlignLeft
			cellColor := tcell.ColorWhite
			if column == columnDate {
				cellText = entry.Snapshot.Date.Format("2006-01-02 15:04:05")
			} else if column == columnName {
				cellText = entry.Snapshot.Name
			} else if column == columnDiff {
				cellAlign = tview.AlignCenter
				switch entry.DiffState {
				case diff_state.Equal:
					cellText = "="
					cellColor = tcell.ColorGray
				case diff_state.Deleted:
					cellText = "+"
					cellColor = tcell.ColorGreen
				case diff_state.Added:
					cellText = "-"
					cellColor = tcell.ColorRed
				case diff_state.Modified:
					cellText = "≠"
					cellColor = tcell.ColorYellow
				case diff_state.Unknown:
					cellText = "N/A"
					cellColor = tcell.ColorGray
				}
			}
			cell := tview.NewTableCell(cellText).
				SetTextColor(cellColor).SetAlign(cellAlign)
			result = append(result, cell)
		}
		return result
	}

	tableEntrySortFunction := func(entries []*SnapshotBrowserEntry, columnToSortBy *table.Column, inverted bool) []*SnapshotBrowserEntry {
		sort.SliceStable(entries, func(i, j int) bool {
			a := entries[i]
			b := entries[j]

			result := 0
			if columnToSortBy == columnName {
				result = strings.Compare(strings.ToLower(a.Snapshot.Name), strings.ToLower(b.Snapshot.Name))
			} else if columnToSortBy == columnDate {
				result = a.Snapshot.Date.Compare(*b.Snapshot.Date)
			} else if columnToSortBy == columnDiff {
				result = int(b.DiffState - a.DiffState)
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

	tableContainer := table.NewTableContainer[SnapshotBrowserEntry](
		application,
		toTableCellsFunction,
		tableEntrySortFunction,
	)

	snapshotsBrowser := &SnapshotBrowserComponent{
		application:                     application,
		selectedSnapshotMap:             map[string]*SelectionInfo[SnapshotBrowserEntry]{},
		tableContainer:                  tableContainer,
		selectedSnapshotChangedCallback: func(snapshot *SnapshotBrowserEntry) {},
	}

	tableContainer.SetColumnSpec(tableColumns, columnDate, true)
	tableContainer.SetSelectionChangedCallback(func(entry *SnapshotBrowserEntry) {
		snapshotsBrowser.rememberSelectionForDataset(entry)
		snapshotsBrowser.selectedSnapshotChangedCallback(entry)
		snapshotsBrowser.updateTableContents()
	})

	return snapshotsBrowser
}

func (snapshotBrowser *SnapshotBrowserComponent) GetLayout() tview.Primitive {
	return snapshotBrowser.tableContainer.GetLayout()
}

func (snapshotBrowser *SnapshotBrowserComponent) SetPath(path string) {
	if snapshotBrowser.path == path {
		return
	}
	snapshotBrowser.path = path

	snapshotBrowser.updateTableContents()
}

func (snapshotBrowser *SnapshotBrowserComponent) Refresh() {
	zfs.RefreshZfsData()
	snapshotBrowser.SetPath(snapshotBrowser.path)
}

func (snapshotBrowser *SnapshotBrowserComponent) SetFileEntry(fileEntry *data.FileBrowserEntry) {
	if fileEntry != nil &&
		snapshotBrowser.currentFileEntry != nil &&
		snapshotBrowser.currentFileEntry.Name == fileEntry.Name {
		return
	}
	snapshotBrowser.currentFileEntry = fileEntry
	snapshotBrowser.updateTableContents()
}

func (snapshotBrowser *SnapshotBrowserComponent) Focus() {
	snapshotBrowser.application.SetFocus(snapshotBrowser.tableContainer.GetLayout())
}

func (snapshotBrowser *SnapshotBrowserComponent) HasFocus() bool {
	return snapshotBrowser.tableContainer.HasFocus()
}

func (snapshotBrowser *SnapshotBrowserComponent) updateTableContents() {
	newEntries := snapshotBrowser.computeTableEntries()
	snapshotBrowser.tableContainer.SetData(newEntries)

	title := "Snapshots"
	if snapshotBrowser.GetSelection() != nil {
		currentSelectionIndex := slices.Index(snapshotBrowser.GetEntries(), snapshotBrowser.GetSelection()) + 1
		totalEntriesCount := len(snapshotBrowser.GetEntries())
		title = fmt.Sprintf("Snapshot: %s (%d/%d)", snapshotBrowser.GetSelection().Snapshot.Name, currentSelectionIndex, totalEntriesCount)
	}
	snapshotBrowser.tableContainer.SetTitle(title)
	snapshotBrowser.restoreSelectionForDataset()
}

func (snapshotBrowser *SnapshotBrowserComponent) computeTableEntries() []*SnapshotBrowserEntry {
	result := []*SnapshotBrowserEntry{}
	ds, err := zfs.FindHostDataset(snapshotBrowser.path)
	if err != nil {
		logging.Error(err.Error())
		return result
	}
	snapshotBrowser.hostDataset = ds

	if snapshotBrowser.hostDataset == nil {
		return result
	}

	snapshots, err := snapshotBrowser.hostDataset.GetSnapshots()
	if err != nil {
		logging.Error(err.Error())
		return result
	}

	for _, snapshot := range snapshots {
		diffState := diff_state.Unknown
		if snapshotBrowser.currentFileEntry != nil {
			filePath := snapshotBrowser.currentFileEntry.GetRealPath()
			diffState = snapshot.DetermineDiffState(filePath)
		}
		result = append(result, &SnapshotBrowserEntry{
			Snapshot:  snapshot,
			DiffState: diffState,
		})
	}

	return result
}

func (snapshotBrowser *SnapshotBrowserComponent) clear() {
	snapshotBrowser.path = ""
	snapshotBrowser.updateTableContents()
}

func (snapshotBrowser *SnapshotBrowserComponent) rememberSelectionForDataset(selection *SnapshotBrowserEntry) {
	if snapshotBrowser.hostDataset == nil {
		return
	}
	snapshotBrowser.selectedSnapshotMap[snapshotBrowser.hostDataset.Path] = &SelectionInfo[SnapshotBrowserEntry]{
		Index: slices.Index(snapshotBrowser.GetEntries(), selection),
		Entry: selection,
	}
}

func (snapshotBrowser *SnapshotBrowserComponent) getRememberedSelectionInfo(path string) *SelectionInfo[SnapshotBrowserEntry] {
	selectionInfo, ok := snapshotBrowser.selectedSnapshotMap[path]
	if !ok {
		return nil
	} else {
		return selectionInfo
	}
}

func (snapshotBrowser *SnapshotBrowserComponent) restoreSelectionForDataset() {
	var entryToSelect *SnapshotBrowserEntry
	if snapshotBrowser.hostDataset == nil {
		snapshotBrowser.selectSnapshot(entryToSelect)
		return
	}

	entries := snapshotBrowser.GetEntries()
	rememberedSelectionInfo := snapshotBrowser.getRememberedSelectionInfo(snapshotBrowser.hostDataset.Path)
	if rememberedSelectionInfo == nil {
		entryToSelect = entries[0]
	} else {
		var index int
		if rememberedSelectionInfo.Entry == nil {
			snapshotBrowser.selectHeader()
			return
		} else {
			index = slices.IndexFunc(entries, func(entry *SnapshotBrowserEntry) bool {
				return entry.Snapshot.Name == rememberedSelectionInfo.Entry.Snapshot.Name
			})
		}
		if index < 0 {
			closestIndex := util.Coerce(rememberedSelectionInfo.Index, 0, len(entries)-1)
			entryToSelect = entries[closestIndex]
		} else {
			entryToSelect = entries[index]
		}
	}
	snapshotBrowser.selectSnapshot(entryToSelect)
}

func (snapshotBrowser *SnapshotBrowserComponent) selectSnapshot(snapshot *SnapshotBrowserEntry) {
	snapshotBrowser.selectedSnapshotChangedCallback(snapshot)
	if snapshotBrowser.GetSelection() == snapshot {
		return
	}
	snapshotBrowser.tableContainer.Select(snapshot)
}

func (snapshotBrowser *SnapshotBrowserComponent) GetSelection() *SnapshotBrowserEntry {
	return snapshotBrowser.tableContainer.GetSelectedEntry()
}

func (snapshotBrowser *SnapshotBrowserComponent) SetSelectedSnapshotChangedCallback(f func(snapshot *SnapshotBrowserEntry)) {
	snapshotBrowser.selectedSnapshotChangedCallback = f
}

func (snapshotBrowser *SnapshotBrowserComponent) GetEntries() []*SnapshotBrowserEntry {
	return snapshotBrowser.tableContainer.GetEntries()
}

func (snapshotBrowser *SnapshotBrowserComponent) selectHeader() {
	snapshotBrowser.tableContainer.SelectHeader()
}

func (snapshotBrowser *SnapshotBrowserComponent) selectFirstIfExists() {
	snapshotBrowser.tableContainer.SelectFirstIfExists()
}
