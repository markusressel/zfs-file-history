package dialog

import (
	"fmt"
	"zfs-file-history/internal/data"
	"zfs-file-history/internal/ui/localization"
	"zfs-file-history/internal/ui/util"

	"github.com/rivo/tview"
)

const (
	MultiSnapshotActionDialogPage util.Page = "MultiSnapshotActionDialog"

	MultiSnapshotDialogClearSelectionActionId DialogActionId = iota
	MultiSnapshotDialogDestroySnapshotActionId
	MultiSnapshotDialogDestroySnapshotRecursivelyActionId
)

func NewMultiSnapshotActionDialog(application *tview.Application, snapshots []*data.SnapshotBrowserEntry) *SelectionDialog {
	snapshotNames := make([]string, 0)
	for _, snapshot := range snapshots {
		snapshotNames = append(snapshotNames, snapshot.Snapshot.Name)
	}

	dialogOptions := []*DialogOption{
		{
			Id:       MultiSnapshotDialogDestroySnapshotActionId,
			Name:     "💥 Destroy all",
			Severity: DialogSeverityDanger,
		},
		{
			Id:       MultiSnapshotDialogDestroySnapshotRecursivelyActionId,
			Name:     "💥 Destroy all (recursive)",
			Severity: DialogSeverityDanger,
		},
		{
			Id:   MultiSnapshotDialogClearSelectionActionId,
			Name: "Clear Selection",
		},
		{
			Id:   DialogCloseActionId,
			Name: localization.LocalizationCommonClose,
		},
	}

	return NewSelectionDialog(
		application,
		string(MultiSnapshotActionDialogPage),
		localization.LocalizationSelectActionDialogTitle,
		fmt.Sprintf("What do you want to do with '%v'?", snapshotNames),
		dialogOptions,
	)
}
