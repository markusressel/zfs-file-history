package ui

import (
	"github.com/rivo/tview"
	"golang.org/x/exp/slices"
	"strings"
	"zfs-file-history/internal/zfs"
)

type SnapshotInfo struct {
	snapshots []*zfs.Snapshot
	layout    *tview.Table
}

func NewSnapshotInfo(snapshots []*zfs.Snapshot) *SnapshotInfo {
	return &SnapshotInfo{
		snapshots: snapshots,
	}
}

func (snapshotInfo *SnapshotInfo) Layout() *tview.Table {
	layout := tview.NewTable()
	layout.SetBorder(true)
	layout.SetTitle(" Snapshots ")
	snapshotInfo.layout = layout
	snapshotInfo.updateUi()
	return layout
}

func (snapshotInfo *SnapshotInfo) SetSnapshots(snapshots []*zfs.Snapshot) {
	snapshotInfo.snapshots = slices.Clone(snapshots)
	slices.SortFunc(snapshotInfo.snapshots, func(a, b *zfs.Snapshot) int {
		return strings.Compare(a.Name, b.Name)
	})
	snapshotInfo.updateUi()
}

func (snapshotInfo *SnapshotInfo) updateUi() {
	snapshotInfo.layout.Clear()
	for i, snapshot := range snapshotInfo.snapshots {
		cellText := snapshot.Name
		snapshotInfo.layout.SetCell(
			i, 0,
			tview.NewTableCell(cellText),
		)
	}
	snapshotInfo.layout.ScrollToBeginning()
}
