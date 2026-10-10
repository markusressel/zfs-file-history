package dialog

import (
	"fmt"
	"slices"
	"zfs-file-history/internal/data"
	"zfs-file-history/internal/data/diff_state"
	"zfs-file-history/internal/ui/localization"
	"zfs-file-history/internal/ui/util"

	"github.com/rivo/tview"
)

const (
	ActionDialog util.Page = "ActionDialog"

	FileDialogShowDiffActionId DialogActionId = iota
	FileDialogRestoreFileActionId
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
	dialogOptions := buildFileDialogOptions(file, DiffBinExists())

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

func buildFileDialogOptions(file *data.FileBrowserEntry, diffBinAvailable bool) []*DialogOption {
	dialogOptions := []*DialogOption{{
		Id:   DialogCloseActionId,
		Name: localization.LocalizationCommonClose,
	}}

	if file.HasReal() {
		dialogOptions = slices.Insert(dialogOptions, 0, &DialogOption{
			Id:          FileDialogDeleteDialogActionId,
			Name:        fmt.Sprintf("🗑  Delete '%s'", file.RealFile.Name),
			Description: "Delete the file or folder from the disk",
			Severity:    DialogSeverityDanger,
		})
	}

	canRestore := file.HasSnapshot() || (file.DiffState != diff_state.Equal && file.DiffState != diff_state.Unknown)
	if canRestore {
		if file.Type == data.Directory {
			dialogOptions = slices.Insert(dialogOptions, 0, &DialogOption{
				Id:          FileDialogRestoreFileActionId,
				Name:        "📁 Restore directory only",
				Description: "Restore the folder from the snapshot, without its content",
				Severity:    DialogSeverityWarning,
			})
			dialogOptions = slices.Insert(dialogOptions, 0, &DialogOption{
				Id:          FileDialogRestoreRecursiveDialogActionId,
				Name:        "🌳 Restore directory recursively",
				Description: "Restore the folder and all its content from the snapshot",
				Severity:    DialogSeverityDanger,
			})
		}

		if file.Type == data.File {
			if diffBinAvailable && file.HasDiff() {
				dialogOptions = slices.Insert(dialogOptions, 0, &DialogOption{
					Id:          FileDialogShowDiffActionId,
					Name:        "🔍 Show diff",
					Description: "Compare the file with its version in the snapshot",
				})
			}
			dialogOptions = slices.Insert(dialogOptions, 0, &DialogOption{
				Id:          FileDialogShowHistoryActionId,
				Name:        "📜 Browse history / versions",
				Description: "Browse all versions of the file in the snapshots",
			})
			dialogOptions = slices.Insert(dialogOptions, 1, &DialogOption{
				Id:          FileDialogRestoreFileActionId,
				Name:        "♻️ Restore file",
				Description: "Replace the file with its version in the snapshot",
				Severity:    DialogSeverityWarning,
			})
		}
	}

	if file.Type == data.Directory {
		// also interesting if the directory did not change compared to the snapshot
		dialogOptions = slices.Insert(dialogOptions, 0, &DialogOption{
			Id:          FileDialogShowHistoryActionId,
			Name:        "📂 Browse folder history",
			Description: "Browse the changes of the folder in the snapshots",
		})
	}

	dialogOptions = slices.Insert(dialogOptions, 0, &DialogOption{
		Id:          FileDialogCreateSnapshotDialogActionId,
		Name:        "📸 Create Snapshot",
		Description: "Snapshot the dataset of the folder now",
	})

	return ensureDialogCloseIsLast(dialogOptions)
}
