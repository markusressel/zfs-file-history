package dialog

import (
	"fmt"
	"zfs-file-history/internal/data"
	"zfs-file-history/internal/ui/util"

	"github.com/rivo/tview"
)

const (
	DeleteSnapshotDialogPage util.Page = "DeleteSnapshotDialog"

	DeleteSnapshotDialogDeleteSnapshotActionId DialogActionId = iota
)

func NewDeleteSnapshotDialog(application *tview.Application, snapshot *data.SnapshotBrowserEntry) *SelectionDialog {
	return NewSelectionDialog(
		application,
		string(DeleteSnapshotDialogPage),
		" 💥 Destroy Snapshot ",
		fmt.Sprintf("Destroy '%s'?", snapshot.Snapshot.Name),
		buildConfirmDialogOptions(DeleteSnapshotDialogDeleteSnapshotActionId, "Destroy", true, DialogSeverityDanger),
	)
}
