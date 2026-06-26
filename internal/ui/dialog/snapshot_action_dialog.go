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
)

func NewSnapshotActionDialog(
	application *tview.Application,
	snapshot *data.SnapshotBrowserEntry,
	asyncWork func(d *SelectionDialog, action DialogActionId) error,
	onComplete func(d *SelectionDialog, option *DialogOption, err error),
) *SelectionDialog {
	dialogOptions := []*DialogOption{
		{
			Id:   SnapshotDialogCreateSnapshotActionId,
			Name: "📸 Create Snapshot",
		},
		{
			Id:       SnapshotDialogDestroySnapshotActionId,
			Name:     fmt.Sprintf("💥 Destroy '%s'", snapshot.Snapshot.Name),
			Severity: DialogSeverityDanger,
		},
		{
			Id:       SnapshotDialogDestroySnapshotRecursivelyActionId,
			Name:     fmt.Sprintf("💥 Destroy (recursive) '%s'", snapshot.Snapshot.Name),
			Severity: DialogSeverityDanger,
		},
		{
			Id:   DialogCloseActionId,
			Name: localization.LocalizationCommonClose,
		},
	}

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
