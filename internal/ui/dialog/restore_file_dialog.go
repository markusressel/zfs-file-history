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

func NewRestoreFileDialog(
	application *tview.Application,
	file *data.FileBrowserEntry,
	asyncWork func(d *SelectionDialog, action DialogActionId) error,
	onComplete func(d *SelectionDialog, option *DialogOption, err error),
) *SelectionDialog {
	return NewSelectionDialog(
		application,
		string(RestoreFileDialogPage),
		" ♻️ Restore File ",
		restoreQuestion(file),
		buildRestoreDialogOptions(file),
		asyncWork,
		onComplete,
	)
}

// restoreQuestion asks whether to restore the file, from which snapshot if it is known (e.g. the newest one that
// contains a file that only exists in other snapshots than the selected one).
func restoreQuestion(file *data.FileBrowserEntry) string {
	if len(file.SnapshotFiles) > 0 && file.SnapshotFiles[0] != nil && file.SnapshotFiles[0].Snapshot != nil {
		return fmt.Sprintf("Restore '%s' from snapshot '%s'?", file.Name, file.SnapshotFiles[0].Snapshot.Name)
	}
	return fmt.Sprintf("Restore '%s'?", file.Name)
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
