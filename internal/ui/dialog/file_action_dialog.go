package dialog

import (
	"fmt"
	"os/exec"
	"slices"
	"zfs-file-history/internal/data"
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
			Id:       FileDialogDeleteDialogActionId,
			Name:     fmt.Sprintf("🗑  Delete '%s'", file.RealFile.Name),
			Severity: DialogSeverityDanger,
		})
	}

	if file.HasSnapshot() {
		if file.Type == data.Directory {
			dialogOptions = slices.Insert(dialogOptions, 0, &DialogOption{
				Id:       FileDialogRestoreFileActionId,
				Name:     "📁 Restore directory only",
				Severity: DialogSeverityWarning,
			})
			dialogOptions = slices.Insert(dialogOptions, 0, &DialogOption{
				Id:       FileDialogRestoreRecursiveDialogActionId,
				Name:     "🌳 Restore directory recursively",
				Severity: DialogSeverityDanger,
			})
		}

		if file.Type == data.File {
			if diffBinAvailable && file.HasDiff() {
				dialogOptions = slices.Insert(dialogOptions, 0, &DialogOption{
					Id:   FileDialogShowDiffActionId,
					Name: "🔍 Show diff",
				})
			}
			dialogOptions = slices.Insert(dialogOptions, 0, &DialogOption{
				Id:   FileDialogShowHistoryActionId,
				Name: "📜 Browse history / versions",
			})
			dialogOptions = slices.Insert(dialogOptions, 1, &DialogOption{
				Id:       FileDialogRestoreFileActionId,
				Name:     "♻️ Restore file",
				Severity: DialogSeverityWarning,
			})
		}
	}

	dialogOptions = slices.Insert(dialogOptions, 0, &DialogOption{
		Id:   FileDialogCreateSnapshotDialogActionId,
		Name: "📸 Create Snapshot",
	})

	return ensureDialogCloseIsLast(dialogOptions)
}

func DiffBinExists() bool {
	_, err := exec.LookPath(DiffBinPath)
	if err != nil {
		return false
	}
	return true
}
