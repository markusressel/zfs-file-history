package ui

import (
	"github.com/rivo/tview"
	"golang.org/x/exp/slices"
	"strings"
	"zfs-file-history/internal/zfs"
)

type SnapshotInfo struct {
	application   *tview.Application
	snapshots     []*zfs.Snapshot
	snapshotTable *tview.Table
}

func NewSnapshotInfo(application *tview.Application, snapshots []*zfs.Snapshot) *SnapshotInfo {
	return &SnapshotInfo{
		application: application,
		snapshots:   snapshots,
	}
}

func (snapshotInfo *SnapshotInfo) Layout() *tview.Table {
	table := tview.NewTable()
	table.SetBorder(true)
	table.SetTitle(" Snapshots ")
	table.SetSelectable(true, false)
	snapshotInfo.snapshotTable = table
	snapshotInfo.updateUi()
	return table
}

func (snapshotInfo *SnapshotInfo) SetSnapshots(snapshots []*zfs.Snapshot) {
	snapshotInfo.snapshots = slices.Clone(snapshots)
	slices.SortFunc(snapshotInfo.snapshots, func(a, b *zfs.Snapshot) int {
		return strings.Compare(a.Name, b.Name)
	})
	snapshotInfo.updateUi()
}

func (snapshotInfo *SnapshotInfo) updateUi() {
	snapshotInfo.snapshotTable.Clear()
	for i, snapshot := range snapshotInfo.snapshots {
		cellText := snapshot.Name
		snapshotInfo.snapshotTable.SetCell(
			i, 0,
			tview.NewTableCell(cellText),
		)
	}
	snapshotInfo.snapshotTable.ScrollToBeginning()
}

func (snapshotInfo *SnapshotInfo) Focus() {
	snapshotInfo.application.SetFocus(snapshotInfo.snapshotTable)
}
