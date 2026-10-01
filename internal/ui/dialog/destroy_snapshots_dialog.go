package dialog

import (
	"zfs-file-history/internal/ui/util"

	"github.com/rivo/tview"
)

const (
	DestroySnapshotsDialogPage util.Page = "DestroySnapshotsDialog"

	DestroySnapshotsDialogDestroyActionId DialogActionId = iota
)

// NewDestroySnapshotsDialog asks for confirmation before destroying snapshots. The description should explain
// what exactly will be destroyed (e.g. the result of a dry run). asyncWork receives
// DestroySnapshotsDialogDestroyActionId on confirmation.
func NewDestroySnapshotsDialog(
	application *tview.Application,
	description string,
	asyncWork func(d *SelectionDialog, action DialogActionId) error,
	onComplete func(d *SelectionDialog, option *DialogOption, err error),
) *SelectionDialog {
	return NewSelectionDialog(
		application,
		string(DestroySnapshotsDialogPage),
		" 💥 Destroy Snapshots ",
		description,
		buildConfirmDialogOptions(DestroySnapshotsDialogDestroyActionId, "Destroy", true, DialogSeverityDanger),
		asyncWork,
		onComplete,
	)
}
