package ui

import (
	"fmt"
	"github.com/rivo/tview"
	"golang.org/x/exp/slices"
	"zfs-file-history/internal/data"
	"zfs-file-history/internal/logging"
	uiutil "zfs-file-history/internal/ui/util"
	"zfs-file-history/internal/util"
	"zfs-file-history/internal/zfs"
)

type SnapshotBrowserComponent struct {
	application   *tview.Application
	snapshotTable *tview.Table

	path            string
	currentFileEnty *data.FileBrowserEntry

	dataset *zfs.Dataset

	snapshots               []*zfs.Snapshot
	currentSnapshot         *zfs.Snapshot
	selectedSnapshotChanged chan *zfs.Snapshot

	selectedSnapshotMap map[string]*zfs.Snapshot
}

func NewSnapshotBrowser(application *tview.Application) *SnapshotBrowserComponent {
	snapshotsBrowser := &SnapshotBrowserComponent{
		application:             application,
		snapshots:               []*zfs.Snapshot{},
		selectedSnapshotChanged: make(chan *zfs.Snapshot),
		selectedSnapshotMap:     map[string]*zfs.Snapshot{},
	}
	snapshotsBrowser.createLayout()

	return snapshotsBrowser
}

func (snapshotBrowser *SnapshotBrowserComponent) SetPath(path string) {
	if path == "" {
		snapshotBrowser.Clear()
		return
	}
	if snapshotBrowser.path == path {
		return
	}
	snapshotBrowser.path = path

	hostDataset, err := zfs.FindHostDataset(path)
	if err != nil {
		logging.Error(err.Error())
		snapshotBrowser.Clear()
		return
	}

	if hostDataset == nil {
		snapshotBrowser.Clear()
		return
	}

	snapshots, err := hostDataset.GetSnapshots()
	if err != nil {
		logging.Error(err.Error())
		snapshotBrowser.Clear()
		return
	}

	snapshotBrowser.setSnapshots(snapshots)

	// TODO: remember snapshot selection on a "per-dataset" basis
	if snapshotBrowser.currentSnapshot == nil && len(snapshots) > 0 {
		snapshotBrowser.selectSnapshot(snapshotBrowser.snapshots[0])
	} else if !slices.ContainsFunc(snapshotBrowser.snapshots, func(snapshot *zfs.Snapshot) bool {
		return snapshotBrowser.currentSnapshot.Path == snapshot.Path
	}) {
		snapshotBrowser.selectSnapshot(nil)
	}

	// TODO: highlight snapshots which contain the given file
	//snapshotsContainingSelection := []*zfs.Snapshot{}
	//if snapshotBrowser.path != "" && snapshotBrowser.currentFileEnty != nil {
	//	for _, snapshot := range snapshotBrowser.currentFileEnty.SnapshotFiles {
	//		snapshotsContainingSelection = append(snapshotsContainingSelection, snapshot.Snapshot)
	//	}
	//}

	snapshotBrowser.updateUi()
}

func (snapshotBrowser *SnapshotBrowserComponent) SetFileEntry(fileEntry *data.FileBrowserEntry) {
	snapshotBrowser.currentFileEnty = fileEntry
	snapshotBrowser.updateUi()
}

func (snapshotBrowser *SnapshotBrowserComponent) Focus() {
	snapshotBrowser.application.SetFocus(snapshotBrowser.snapshotTable)
}

func (snapshotBrowser *SnapshotBrowserComponent) HasFocus() bool {
	return snapshotBrowser.snapshotTable.HasFocus()
}

func (snapshotBrowser *SnapshotBrowserComponent) createLayout() *tview.Table {
	table := tview.NewTable()
	table.SetBorder(true)
	table.SetBorderPadding(0, 0, 1, 1)
	table.SetSelectable(true, false)

	table.SetSelectionChangedFunc(func(row int, column int) {
		selectionIndex := util.Coerce(row, -1, len(snapshotBrowser.snapshots))
		var newSelection *zfs.Snapshot
		if selectionIndex < 0 {
			newSelection = nil
		} else {
			newSelection = snapshotBrowser.snapshots[selectionIndex]
		}
		snapshotBrowser.selectSnapshot(newSelection)
	})

	snapshotBrowser.snapshotTable = table
	snapshotBrowser.updateUi()
	return table
}

func (snapshotBrowser *SnapshotBrowserComponent) setSnapshots(snapshots []*zfs.Snapshot) {
	snapshotsClone := slices.Clone(snapshots)
	slices.SortFunc(snapshotsClone, func(a, b *zfs.Snapshot) int {
		return a.Date.Compare(*b.Date) * -1
	})

	snapshotBrowser.snapshots = snapshotsClone
	if len(snapshotBrowser.snapshots) <= 0 {
		snapshotBrowser.currentSnapshot = nil
	}

	snapshotBrowser.restoreSnapshotSelection()
}

func (snapshotBrowser *SnapshotBrowserComponent) updateUi() {
	table := snapshotBrowser.snapshotTable
	table.Clear()

	title := "Snapshots"
	if snapshotBrowser.currentSnapshot != nil {
		title = fmt.Sprintf("Snapshot: %s", snapshotBrowser.currentSnapshot.Name)
	}

	uiutil.SetupWindow(table, title)

	for i, snapshot := range snapshotBrowser.snapshots {
		cellText := snapshot.Name
		table.SetCell(
			i, 0,
			tview.NewTableCell(cellText),
		)
	}
}

func (snapshotBrowser *SnapshotBrowserComponent) restoreSnapshotSelection() {
	if snapshotBrowser.dataset == nil {
		return
	}
	lastSelectedSnapshot, ok := snapshotBrowser.selectedSnapshotMap[snapshotBrowser.dataset.Path]
	if ok && slices.Contains(snapshotBrowser.snapshots, lastSelectedSnapshot) {
		snapshotBrowser.selectSnapshot(lastSelectedSnapshot)
	}
}

func (snapshotBrowser *SnapshotBrowserComponent) SetDataset(dataset *zfs.Dataset) {
	snapshotBrowser.dataset = dataset
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
	snapshotBrowser.updateUi()
}

func (snapshotBrowser *SnapshotBrowserComponent) Clear() {
	snapshotBrowser.path = ""
	snapshotBrowser.setSnapshots([]*zfs.Snapshot{})
	snapshotBrowser.updateUi()
}

func (snapshotBrowser *SnapshotBrowserComponent) refresh() {
	zfs.RefreshZfsData()
	snapshotBrowser.SetPath(snapshotBrowser.path)
}
