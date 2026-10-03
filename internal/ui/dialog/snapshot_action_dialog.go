package dialog

import (
	"fmt"
	"zfs-file-history/internal/data"
	"zfs-file-history/internal/ui/localization"
	"zfs-file-history/internal/ui/util"

	"github.com/rivo/tview"
)

const (
	SnapshotActionDialogPage util.Page = "SnapshotActionDialog"

	SnapshotDialogCreateSnapshotActionId DialogActionId = iota
	SnapshotDialogDestroySnapshotActionId
	SnapshotDialogDestroySnapshotRecursivelyActionId
	SnapshotDialogCloneSnapshotActionId
	SnapshotDialogHoldSnapshotActionId
	SnapshotDialogReleaseSnapshotActionId
	SnapshotDialogShowHistoryActionId
)

// NewSnapshotActionDialog creates the dialog with the actions for a snapshot. historyTarget is the file or folder
// whose history can be opened at the snapshot ("": no history action).
func NewSnapshotActionDialog(
	application *tview.Application,
	snapshot *data.SnapshotBrowserEntry,
	historyTarget string,
	asyncWork func(d *SelectionDialog, action DialogActionId) error,
	onComplete func(d *SelectionDialog, option *DialogOption, err error),
) *SelectionDialog {
	var dialogOptions []*DialogOption
	if historyTarget != "" {
		dialogOptions = append(dialogOptions, &DialogOption{
			Id:   SnapshotDialogShowHistoryActionId,
			Name: fmt.Sprintf("🕘 History of '%s' at this snapshot", historyTarget),
		})
	}
	dialogOptions = append(dialogOptions, []*DialogOption{
		{
			Id:   SnapshotDialogCreateSnapshotActionId,
			Name: "📸 Create Snapshot",
		},
		{
			Id:   SnapshotDialogCloneSnapshotActionId,
			Name: fmt.Sprintf("🧬 Clone '%s'", snapshot.Snapshot.Name),
		},
		{
			Id:   SnapshotDialogHoldSnapshotActionId,
			Name: fmt.Sprintf("🔒 Hold '%s' (protect from destruction)", snapshot.Snapshot.Name),
		},
	}...)
	if snapshot.Snapshot.Properties.Holds > 0 {
		dialogOptions = append(dialogOptions, &DialogOption{
			Id:   SnapshotDialogReleaseSnapshotActionId,
			Name: fmt.Sprintf("🔓 Release hold of '%s'", snapshot.Snapshot.Name),
		})
	}
	dialogOptions = append(dialogOptions,
		&DialogOption{
			Id:       SnapshotDialogDestroySnapshotActionId,
			Name:     fmt.Sprintf("💥 Destroy '%s'", snapshot.Snapshot.Name),
			Severity: DialogSeverityDanger,
		},
		&DialogOption{
			Id:       SnapshotDialogDestroySnapshotRecursivelyActionId,
			Name:     fmt.Sprintf("💥 Destroy (recursive) '%s'", snapshot.Snapshot.Name),
			Severity: DialogSeverityDanger,
		},
		&DialogOption{
			Id:   DialogCloseActionId,
			Name: localization.LocalizationCommonClose,
		},
	)

	return NewSelectionDialog(
		application,
		string(SnapshotActionDialogPage),
		localization.LocalizationSelectActionDialogTitle,
		fmt.Sprintf("What do you want to do with '%s'?", snapshot.Snapshot.Name),
		dialogOptions,
		asyncWork,
		onComplete,
	)
}
