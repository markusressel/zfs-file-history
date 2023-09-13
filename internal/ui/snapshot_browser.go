package ui

import (
	"github.com/rivo/tview"
	"golang.org/x/exp/slices"
	"strings"
	"zfs-file-history/internal/logging"
	"zfs-file-history/internal/util"
	"zfs-file-history/internal/zfs"
)

type SnapshotBrowser struct {
	application             *tview.Application
	snapshots               []*zfs.Snapshot
	snapshotTable           *tview.Table
	path                    string
	currentSnapshot         *zfs.Snapshot
	currentFileEnty         *FileBrowserEntry
	selectedSnapshotChanged chan *zfs.Snapshot
}

func NewSnapshotBrowser(application *tview.Application) *SnapshotBrowser {
	snapshotsBrowser := &SnapshotBrowser{
		application:             application,
		snapshots:               []*zfs.Snapshot{},
		selectedSnapshotChanged: make(chan *zfs.Snapshot),
	}
	snapshotsBrowser.createLayout()

	return snapshotsBrowser
}

func (snapshotBrowser *SnapshotBrowser) SetPath(path string) {
	if snapshotBrowser.path != path {
		snapshotBrowser.path = path

		hostDataset, err := zfs.FindHostDataset(path)
		if err == nil {
			if hostDataset != nil {
				snapshots, err := hostDataset.GetSnapshots()
				if err == nil {
					snapshotBrowser.setSnapshots(snapshots)
					if snapshotBrowser.currentSnapshot == nil && len(snapshots) > 0 {
						snapshotBrowser.SelectSnapshot(snapshots[0])
					}
				} else {
					logging.Error(err.Error())
				}
			}
		} else {
			logging.Error(err.Error())
		}

		snapshotBrowser.updateZfsInfo()
	}
}

func (snapshotBrowser *SnapshotBrowser) SetFileEntry(fileEntry *FileBrowserEntry) {
	snapshotBrowser.currentFileEnty = fileEntry
	snapshotBrowser.updateUi()
}

func (snapshotBrowser *SnapshotBrowser) Focus() {
	snapshotBrowser.application.SetFocus(snapshotBrowser.snapshotTable)
}

func (snapshotBrowser *SnapshotBrowser) HasFocus() bool {
	return snapshotBrowser.snapshotTable.HasFocus()
}

func (snapshotBrowser *SnapshotBrowser) createLayout() *tview.Table {
	table := tview.NewTable()
	table.SetBorder(true)
	table.SetTitle(" Snapshots ")
	table.SetSelectable(true, false)

	table.SetSelectionChangedFunc(func(row int, column int) {
		selectionIndex := util.Coerce(row, -1, len(snapshotBrowser.snapshots))
		var newSelection *zfs.Snapshot
		if selectionIndex < 0 {
			newSelection = nil
		} else {
			newSelection = snapshotBrowser.snapshots[selectionIndex]
		}
		snapshotBrowser.SelectSnapshot(newSelection)
	})

	snapshotBrowser.snapshotTable = table
	snapshotBrowser.updateUi()
	return table
}

func (snapshotBrowser *SnapshotBrowser) setSnapshots(snapshots []*zfs.Snapshot) {
	snapshotBrowser.snapshots = slices.Clone(snapshots)
	slices.SortFunc(snapshotBrowser.snapshots, func(a, b *zfs.Snapshot) int {
		return strings.Compare(a.Name, b.Name)
	})
	snapshotBrowser.updateUi()
}

func (snapshotBrowser *SnapshotBrowser) updateUi() {
	snapshotBrowser.snapshotTable.Clear()
	for i, snapshot := range snapshotBrowser.snapshots {
		cellText := snapshot.Name
		snapshotBrowser.snapshotTable.SetCell(
			i, 0,
			tview.NewTableCell(cellText),
		)
	}
	snapshotBrowser.snapshotTable.ScrollToBeginning()
}

func (snapshotBrowser *SnapshotBrowser) updateZfsInfo() {
	var dataset *zfs.Dataset
	if snapshotBrowser.currentSnapshot != nil {
		dataset = snapshotBrowser.currentSnapshot.ParentDataset
	}

	if dataset != nil {
		snapshots, err := dataset.GetSnapshots()
		if err != nil {
			logging.Fatal(err.Error())
		}
		snapshotBrowser.snapshots = snapshots
	} else {
		snapshotBrowser.snapshots = []*zfs.Snapshot{}
	}

	if len(snapshotBrowser.snapshots) > 0 {
		snapshotBrowser.SelectSnapshot(snapshotBrowser.snapshots[0])
	} else {
		snapshotBrowser.SelectSnapshot(nil)
	}

	snapshotsContainingSelection := []*zfs.Snapshot{}
	if snapshotBrowser.path != "" && snapshotBrowser.currentFileEnty != nil {
		for _, snapshot := range snapshotBrowser.currentFileEnty.SnapshotFiles {
			snapshotsContainingSelection = append(snapshotsContainingSelection, snapshot.Snapshot)
		}
	}
	snapshotBrowser.setSnapshots(snapshotBrowser.snapshots)
}

func (snapshotBrowser *SnapshotBrowser) SelectSnapshot(snapshot *zfs.Snapshot) {
	if snapshotBrowser.currentSnapshot != snapshot {
		snapshotBrowser.currentSnapshot = snapshot
		go func() {
			snapshotBrowser.selectedSnapshotChanged <- snapshotBrowser.currentSnapshot
		}()
	}
}
