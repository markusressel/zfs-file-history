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
	MultiSnapshotDialogHoldSnapshotsActionId
	MultiSnapshotDialogReleaseSnapshotsActionId
)

func NewMultiSnapshotActionDialog(
	application *tview.Application,
	snapshots []*data.SnapshotBrowserEntry,
	asyncWork func(d *SelectionDialog, action DialogActionId) error,
	onComplete func(d *SelectionDialog, option *DialogOption, err error),
) *SelectionDialog {
	snapshotNames := make([]string, 0)
	anyHeld := false
	for _, snapshot := range snapshots {
		snapshotNames = append(snapshotNames, snapshot.Snapshot.Name)
		anyHeld = anyHeld || snapshot.Snapshot.Properties.Holds > 0
	}

	dialogOptions := []*DialogOption{
		{
			Id:   MultiSnapshotDialogHoldSnapshotsActionId,
			Name: "🔒 Hold all (protect from destruction)",
		},
	}
	if anyHeld {
		dialogOptions = append(dialogOptions, &DialogOption{
			Id:   MultiSnapshotDialogReleaseSnapshotsActionId,
			Name: "🔓 Release holds of all",
		})
	}
	dialogOptions = append(dialogOptions,
		&DialogOption{
			Id:       MultiSnapshotDialogDestroySnapshotActionId,
			Name:     "💥 Destroy all",
			Severity: DialogSeverityDanger,
		},
		&DialogOption{
			Id:       MultiSnapshotDialogDestroySnapshotRecursivelyActionId,
			Name:     "💥 Destroy all (recursive)",
			Severity: DialogSeverityDanger,
		},
		&DialogOption{
			Id:   MultiSnapshotDialogClearSelectionActionId,
			Name: "Clear Selection",
		},
		&DialogOption{
			Id:   DialogCloseActionId,
			Name: localization.LocalizationCommonClose,
		},
	)

	return NewSelectionDialog(
		application,
		string(MultiSnapshotActionDialogPage),
		localization.LocalizationSelectActionDialogTitle,
		fmt.Sprintf("What do you want to do with '%v'?", snapshotNames),
		dialogOptions,
		asyncWork,
		onComplete,
	)
}
