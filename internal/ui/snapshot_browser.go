package ui

import (
	"github.com/rivo/tview"
	"golang.org/x/exp/slices"
	"strings"
	"zfs-file-history/internal/zfs"
)

type SnapshotBrowser struct {
	application   *tview.Application
	snapshots     []*zfs.Snapshot
	snapshotTable *tview.Table
}

func NewSnapshotBrowser(application *tview.Application, snapshots []*zfs.Snapshot) *SnapshotBrowser {
	snapshotsBrowser := &SnapshotBrowser{
		application: application,
		snapshots:   snapshots,
	}
	snapshotsBrowser.createLayout()

	return snapshotsBrowser
}

func (snapshotBrowser *SnapshotBrowser) createLayout() *tview.Table {
	table := tview.NewTable()
	table.SetBorder(true)
	table.SetTitle(" Snapshots ")
	table.SetSelectable(true, false)
	snapshotBrowser.snapshotTable = table
	snapshotBrowser.updateUi()
	return table
}

func (snapshotBrowser *SnapshotBrowser) SetSnapshots(snapshots []*zfs.Snapshot) {
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

func (snapshotBrowser *SnapshotBrowser) Focus() {
	snapshotBrowser.application.SetFocus(snapshotBrowser.snapshotTable)
}

func (snapshotBrowser *SnapshotBrowser) HasFocus() bool {
	return snapshotBrowser.snapshotTable.HasFocus()
}
