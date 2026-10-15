package dialog

import (
	"fmt"
	"zfs-file-history/internal/data"
	"zfs-file-history/internal/data/diff_state"
	"zfs-file-history/internal/ui/localization"
	"zfs-file-history/internal/ui/util"

	"github.com/rivo/tview"
)

const (
	ActionDialog util.Page = "ActionDialog"

	FileDialogRestoreFileActionId DialogActionId = iota
	FileDialogRestoreRecursiveDialogActionId
	FileDialogDeleteDialogActionId
	FileDialogCreateSnapshotDialogActionId
	FileDialogShowHistoryActionId
)

func NewFileActionDialog(
	application *tview.Application,
	file *data.FileBrowserEntry,
	handler func(d *SelectionDialog, action DialogActionId) error,
	onComplete func(d *SelectionDialog, option *DialogOption, err error),
) *SelectionDialog {
	dialogOptions := buildFileDialogOptions(file)

	return NewSelectionDialog(
		application,
		string(ActionDialog),
		localization.LocalizationSelectActionDialogTitle,
		fmt.Sprintf("What do you want to do with '%s'?", file.Name),
		dialogOptions,
		handler,
		onComplete,
	)
}

// buildFileDialogOptions returns the actions for the file: the history first, the reason to open the menu most of the
// time (it also shows the changes, see FileHistoryOverlay).
func buildFileDialogOptions(file *data.FileBrowserEntry) []*DialogOption {
	var dialogOptions []*DialogOption

	canRestore := file.HasSnapshot() || (file.DiffState != diff_state.Equal && file.DiffState != diff_state.Unknown)
	switch {
	case file.Type == data.Directory:
		// also interesting if the directory did not change compared to the snapshot
		dialogOptions = append(dialogOptions, &DialogOption{
			Id:          FileDialogShowHistoryActionId,
			Name:        "📂 Browse folder history",
			Description: "Browse the changes of the folder in the snapshots",
		})
		if canRestore {
			dialogOptions = append(dialogOptions, &DialogOption{
				Id:          FileDialogRestoreRecursiveDialogActionId,
				Name:        "🌳 Restore directory recursively",
				Description: "Restore the folder and all its content from the snapshot",
				Severity:    DialogSeverityDanger,
			}, &DialogOption{
				Id:          FileDialogRestoreFileActionId,
				Name:        "📁 Restore directory only",
				Description: "Restore the folder from the snapshot, without its content",
				Severity:    DialogSeverityWarning,
			})
		}
	case file.Type == data.File && canRestore:
		dialogOptions = append(dialogOptions, &DialogOption{
			Id:          FileDialogShowHistoryActionId,
			Name:        "📜 Browse history / versions",
			Description: "Browse all versions of the file in the snapshots and their changes",
		}, &DialogOption{
			Id:          FileDialogRestoreFileActionId,
			Name:        "♻️ Restore file",
			Description: "Replace the file with its version in the snapshot",
			Severity:    DialogSeverityWarning,
		})
	}

	dialogOptions = append(dialogOptions, &DialogOption{
		Id:          FileDialogCreateSnapshotDialogActionId,
		Name:        "📸 Create Snapshot",
		Description: "Snapshot the dataset of the folder now",
	})

	if file.HasReal() {
		dialogOptions = append(dialogOptions, &DialogOption{
			Id:          FileDialogDeleteDialogActionId,
			Name:        fmt.Sprintf("🗑  Delete '%s'", file.RealFile.Name),
			Description: "Delete the file or folder from the disk",
			Severity:    DialogSeverityDanger,
		})
	}

	return append(dialogOptions, &DialogOption{
		Id:   DialogCloseActionId,
		Name: localization.LocalizationCommonClose,
	})
}
