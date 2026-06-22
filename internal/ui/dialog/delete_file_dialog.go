package dialog

import (
	"fmt"
	"zfs-file-history/internal/data"
	"zfs-file-history/internal/ui/localization"
	"zfs-file-history/internal/ui/util"

	"github.com/rivo/tview"
)

const (
	DeleteFileDialogPage util.Page = "DeleteFileDialog"

	DeleteFileDialogDeleteFileActionId DialogActionId = iota
)

func NewDeleteFileDialog(application *tview.Application, file *data.FileBrowserEntry) *SelectionDialog {
	return NewSelectionDialog(
		application,
		string(DeleteFileDialogPage),
		" 🗑️ Delete File ",
		fmt.Sprintf("Delete '%s'?", file.Name),
		buildConfirmDialogOptions(DeleteFileDialogDeleteFileActionId, localization.LocalizationCommonDelete, file.HasReal(), DialogSeverityDanger),
		50,
		6,
	)
}
