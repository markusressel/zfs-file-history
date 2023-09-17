package snapshot_browser

import (
	"fmt"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"golang.org/x/exp/slices"
	"strings"
	"zfs-file-history/internal/data"
	"zfs-file-history/internal/logging"
	"zfs-file-history/internal/ui/table"
	"zfs-file-history/internal/zfs"
)

type SnapshotBrowserEntry struct {
	Snapshot                 *zfs.Snapshot
	ContainsCurrentFileEntry bool
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
	columnContainsFile = &table.Column{
		Id:        2,
		Title:     "Contains File",
		Alignment: tview.AlignLeft,
	}
	tableColumns = []*table.Column{
		columnName, columnDate, columnContainsFile,
	}
)

func NewSnapshotBrowser(application *tview.Application, path string) *SnapshotBrowserComponent {
	toTableCellsFunction := func(row int, columns []*table.Column, entry *SnapshotBrowserEntry) (cells []*tview.TableCell) {
		result := []*tview.TableCell{}
		for _, column := range columns {
			cellText := "N/A"
			cellColor := tcell.ColorWhite
			if entry.ContainsCurrentFileEntry {
				cellColor = tcell.ColorGreen
			}
			if column == columnDate {
				cellText = entry.Snapshot.Date.Format("2006-01-02 15:04:05")
			} else if column == columnName {
				cellText = entry.Snapshot.Name
			} else if column == columnContainsFile {
				if entry.ContainsCurrentFileEntry {
					cellText = "✓"
				} else {
					cellText = ""
				}
			}
			cell := tview.NewTableCell(cellText).
				SetTextColor(cellColor)
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
			} else if columnToSortBy == columnContainsFile {
				if a.ContainsCurrentFileEntry && !b.ContainsCurrentFileEntry {
					return -1
				} else if !a.ContainsCurrentFileEntry && b.ContainsCurrentFileEntry {
					return 1
				}
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
	if path == "" {
		snapshotBrowser.clear()
		return
	}
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
		containsFile := false
		if snapshotBrowser.currentFileEntry != nil {
			filePath := snapshotBrowser.currentFileEntry.GetRealPath()
			containsFile, err = snapshot.ContainsFile(filePath)
			if err != nil {
				logging.Error(err.Error())
				return result
			}
		}
		result = append(result, &SnapshotBrowserEntry{
			Snapshot:                 snapshot,
			ContainsCurrentFileEntry: containsFile,
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
