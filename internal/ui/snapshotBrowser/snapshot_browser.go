package snapshotBrowser

import (
	"fmt"
	"github.com/rivo/tview"
	"golang.org/x/exp/slices"
	"zfs-file-history/internal/data"
	"zfs-file-history/internal/logging"
	"zfs-file-history/internal/ui/table"
	"zfs-file-history/internal/zfs"
)

type SnapshotBrowserComponent struct {
	application *tview.Application

	tableContainer *table.RowSelectionTable[zfs.Snapshot]

	path            string
	currentFileEnty *data.FileBrowserEntry

	dataset *zfs.Dataset

	snapshots               []*zfs.Snapshot
	currentSnapshot         *zfs.Snapshot
	selectedSnapshotChanged chan *zfs.Snapshot

	selectedSnapshotMap map[string]*zfs.Snapshot
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
	tableColumns = []*table.Column{
		columnName, columnDate,
	}
)

func NewSnapshotBrowser(application *tview.Application, path string) *SnapshotBrowserComponent {
	toTableCellsFunction := func(row int, columns []*table.Column, entry *zfs.Snapshot) (cells []*tview.TableCell) {
		result := []*tview.TableCell{}
		for _, column := range columns {
			cellText := "N/A"
			if column == columnDate {
				cellText = entry.Date.Format("2006-01-02 15:04:05")
			} else if column == columnName {
				cellText = entry.Name
			}
			result = append(result, tview.NewTableCell(cellText))
		}
		return result
	}

	tableEntrySortFunction := func(entries []*zfs.Snapshot, column *table.Column, inverted bool) []*zfs.Snapshot {
		result := slices.Clone(entries)
		slices.SortFunc(result, func(a, b *zfs.Snapshot) int {
			return a.Date.Compare(*b.Date) * -1
		})
		return result
	}

	tableContainer := table.NewTableContainer[zfs.Snapshot](
		application,
		toTableCellsFunction,
		tableEntrySortFunction,
	)

	snapshotsBrowser := &SnapshotBrowserComponent{
		application:             application,
		snapshots:               []*zfs.Snapshot{},
		selectedSnapshotChanged: make(chan *zfs.Snapshot),
		selectedSnapshotMap:     map[string]*zfs.Snapshot{},
		tableContainer:          tableContainer,
	}

	tableContainer.SetColumnSpec(tableColumns, columnDate, false)
	snapshotsBrowser.SetPath(path)

	return snapshotsBrowser
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

func (snapshotBrowser *SnapshotBrowserComponent) SetFileEntry(fileEntry *data.FileBrowserEntry) {
	snapshotBrowser.currentFileEnty = fileEntry
	snapshotBrowser.updateTableContents()
}

func (snapshotBrowser *SnapshotBrowserComponent) Focus() {
	snapshotBrowser.application.SetFocus(snapshotBrowser.tableContainer.GetLayout())
}

func (snapshotBrowser *SnapshotBrowserComponent) HasFocus() bool {
	return snapshotBrowser.tableContainer.HasFocus()
}

func (snapshotBrowser *SnapshotBrowserComponent) SetDataset(dataset *zfs.Dataset) {
	snapshotBrowser.dataset = dataset
}

func (snapshotBrowser *SnapshotBrowserComponent) updateTableContents() {
	title := "Snapshots"
	if snapshotBrowser.currentSnapshot != nil {
		title = fmt.Sprintf("Snapshot: %s", snapshotBrowser.currentSnapshot.Name)
	}
	snapshotBrowser.tableContainer.SetTitle(title)

	newEntries := snapshotBrowser.computeTableEntries()
	snapshotBrowser.tableContainer.SetData(newEntries)
	snapshotBrowser.restoreSelectionForDataset()
}

func (snapshotBrowser *SnapshotBrowserComponent) computeTableEntries() []*zfs.Snapshot {
	result := []*zfs.Snapshot{}
	hostDataset, err := zfs.FindHostDataset(snapshotBrowser.path)
	if err != nil {
		logging.Error(err.Error())
		snapshotBrowser.clear()
		return result
	}

	if hostDataset == nil {
		snapshotBrowser.clear()
		return result
	}

	result, err = hostDataset.GetSnapshots()
	if err != nil {
		logging.Error(err.Error())
		snapshotBrowser.clear()
		return result
	}

	return result
}

func (snapshotBrowser *SnapshotBrowserComponent) clear() {
	snapshotBrowser.path = ""
	snapshotBrowser.updateTableContents()
}

func (snapshotBrowser *SnapshotBrowserComponent) Refresh() {
	zfs.RefreshZfsData()
	snapshotBrowser.SetPath(snapshotBrowser.path)
}

func (snapshotBrowser *SnapshotBrowserComponent) restoreSelectionForDataset() {
	if snapshotBrowser.dataset == nil {
		return
	}
	lastSelectedSnapshot, ok := snapshotBrowser.selectedSnapshotMap[snapshotBrowser.dataset.Path]
	if ok && slices.Contains(snapshotBrowser.snapshots, lastSelectedSnapshot) {
		snapshotBrowser.selectSnapshot(lastSelectedSnapshot)
	}
}

func (snapshotBrowser *SnapshotBrowserComponent) selectSnapshot(snapshot *zfs.Snapshot) {
	if snapshotBrowser.currentSnapshot == snapshot {
		return
	}
	snapshotBrowser.currentSnapshot = snapshot
	if snapshot != nil {
		snapshotBrowser.selectedSnapshotMap[snapshot.ParentDataset.Path] = snapshot
	}
	go func() {
		snapshotBrowser.selectedSnapshotChanged <- snapshotBrowser.currentSnapshot
	}()
	snapshotBrowser.updateTableContents()
}

func (snapshotBrowser *SnapshotBrowserComponent) OnSelectedSnapshotChanged() <-chan *zfs.Snapshot {
	return snapshotBrowser.selectedSnapshotChanged
}

func (snapshotBrowser *SnapshotBrowserComponent) GetLayout() tview.Primitive {
	return snapshotBrowser.tableContainer.GetLayout()
}
