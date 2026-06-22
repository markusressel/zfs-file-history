package dialog

import (
	"fmt"
	"slices"
	"zfs-file-history/internal/data"
	"zfs-file-history/internal/ui/localization"
	"zfs-file-history/internal/ui/util"

	"github.com/rivo/tview"
)

const (
	RestoreFileDialogPage util.Page = "RestoreFileDialog"

	RestoreFileDialogRestoreFileActionId DialogActionId = iota
	RestoreFileDialogRestoreRecursiveActionId
)

func NewRestoreFileDialog(application *tview.Application, file *data.FileBrowserEntry) *SelectionDialog {
	return NewSelectionDialog(
		application,
		string(RestoreFileDialogPage),
		" ♻️ Restore File ",
		fmt.Sprintf("Restore '%s'?", file.Name),
		buildRestoreDialogOptions(file),
		50,
		6,
	)
}

func buildRestoreDialogOptions(file *data.FileBrowserEntry) []*DialogOption {
	dialogOptions := []*DialogOption{
		{
			Id:   DialogCloseActionId,
			Name: localization.LocalizationCommonClose,
		},
	}

	if file.Type == data.Directory {
		dialogOptions = slices.Insert(dialogOptions, 0, &DialogOption{
			Id:       RestoreFileDialogRestoreFileActionId,
			Name:     "📁 Restore directory only",
			Severity: DialogSeverityWarning,
		})
		dialogOptions = slices.Insert(dialogOptions, 0, &DialogOption{
			Id:       RestoreFileDialogRestoreRecursiveActionId,
			Name:     "🌳 Restore directory recursively",
			Severity: DialogSeverityDanger,
		})
	}
	if file.Type == data.File {
		dialogOptions = slices.Insert(dialogOptions, 1, &DialogOption{
			Id:       RestoreFileDialogRestoreFileActionId,
			Name:     "♻️ Restore file",
			Severity: DialogSeverityWarning,
		})
	}

	return ensureDialogCloseIsLast(dialogOptions)
}
