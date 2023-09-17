package snapshot_browser

import (
	"fmt"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"golang.org/x/exp/slices"
	"strings"
	"zfs-file-history/internal/data"
	"zfs-file-history/internal/data/diff_state"
	"zfs-file-history/internal/logging"
	"zfs-file-history/internal/ui/table"
	"zfs-file-history/internal/zfs"
)

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

	selectedSnapshotMap map[string]*SnapshotBrowserEntry

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

func NewSnapshotBrowser(application *tview.Application, path string) *SnapshotBrowserComponent {
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
		result := slices.Clone(entries)
		slices.SortFunc(result, func(a, b *SnapshotBrowserEntry) int {
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
			return result
		})
		return result
	}

	tableContainer := table.NewTableContainer[SnapshotBrowserEntry](
		application,
		toTableCellsFunction,
		tableEntrySortFunction,
	)

	snapshotsBrowser := &SnapshotBrowserComponent{
		application:                     application,
		selectedSnapshotMap:             map[string]*SnapshotBrowserEntry{},
		tableContainer:                  tableContainer,
		selectedSnapshotChangedCallback: func(snapshot *SnapshotBrowserEntry) {},
	}

	tableContainer.SetColumnSpec(tableColumns, columnDate, true)
	tableContainer.SetSelectionChangedCallback(func(entry *SnapshotBrowserEntry) {
		snapshotsBrowser.rememberSelectionForDataset(entry)
		snapshotsBrowser.selectedSnapshotChangedCallback(entry)
	})
	snapshotsBrowser.SetPath(path)

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
	title := "Snapshots"
	if snapshotBrowser.GetSelection() != nil {
		title = fmt.Sprintf("Snapshot: %s", snapshotBrowser.GetSelection().Snapshot.Name)
	}
	snapshotBrowser.tableContainer.SetTitle(title)

	newEntries := snapshotBrowser.computeTableEntries()
	snapshotBrowser.tableContainer.SetData(newEntries)
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
	snapshotBrowser.selectedSnapshotMap[snapshotBrowser.hostDataset.Path] = selection
}

func (snapshotBrowser *SnapshotBrowserComponent) restoreSelectionForDataset() {
	if snapshotBrowser.hostDataset == nil {
		snapshotBrowser.selectSnapshot(nil)
		return
	}
	lastSelectedSnapshot, ok := snapshotBrowser.selectedSnapshotMap[snapshotBrowser.hostDataset.Path]
	if ok && lastSelectedSnapshot != nil && slices.ContainsFunc(snapshotBrowser.currentEntries(), func(s *SnapshotBrowserEntry) bool {
		return s.Snapshot.Name == lastSelectedSnapshot.Snapshot.Name
	}) {
		snapshotBrowser.selectSnapshot(lastSelectedSnapshot)
	} else {
		if len(snapshotBrowser.currentEntries()) > 0 {
			entry := snapshotBrowser.currentEntries()[0]
			snapshotBrowser.selectSnapshot(entry)
		} else {
			snapshotBrowser.selectSnapshot(nil)
		}
	}
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

func (snapshotBrowser *SnapshotBrowserComponent) currentEntries() []*SnapshotBrowserEntry {
	return snapshotBrowser.tableContainer.GetEntries()
}

func (snapshotBrowser *SnapshotBrowserComponent) SetSelectedSnapshotChangedCallback(f func(snapshot *SnapshotBrowserEntry)) {
	snapshotBrowser.selectedSnapshotChangedCallback = f
}
