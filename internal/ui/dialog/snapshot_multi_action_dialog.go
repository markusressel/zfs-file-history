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
			Id:          MultiSnapshotDialogHoldSnapshotsActionId,
			Name:        "🔒 Hold all (protect from destruction)",
			Description: "Protect the selected snapshots from being destroyed (zfs hold)",
		},
	}
	if anyHeld {
		dialogOptions = append(dialogOptions, &DialogOption{
			Id:          MultiSnapshotDialogReleaseSnapshotsActionId,
			Name:        "🔓 Release holds of all",
			Description: "Allow destroying the selected snapshots again (zfs release)",
		})
	}
	dialogOptions = append(dialogOptions,
		&DialogOption{
			Id:          MultiSnapshotDialogDestroySnapshotActionId,
			Name:        "💥 Destroy all",
			Description: "Destroy the selected snapshots (zfs destroy)",
			Severity:    DialogSeverityDanger,
		},
		&DialogOption{
			Id:          MultiSnapshotDialogDestroySnapshotRecursivelyActionId,
			Name:        "💥 Destroy all (recursive)",
			Description: "Destroy the selected snapshots and the ones of the same names of the child datasets",
			Severity:    DialogSeverityDanger,
		},
		&DialogOption{
			Id:          MultiSnapshotDialogClearSelectionActionId,
			Name:        "Clear Selection",
			Description: "Unselect the selected snapshots",
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
