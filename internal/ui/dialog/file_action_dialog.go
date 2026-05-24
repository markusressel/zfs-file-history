package dialog

import (
	"fmt"
	"os/exec"
	"slices"
	"zfs-file-history/internal/data"
	"zfs-file-history/internal/data/diff_state"
	"zfs-file-history/internal/ui/util"

	"github.com/rivo/tview"
)

const (
	ActionDialog util.Page = "ActionDialog"

	// recursively restores all files and folders top to bottom starting with the given entry
	FileDialogShowDiffActionId DialogActionId = iota
	FileDialogRestoreFileActionId
	FileDialogRestoreRecursiveDialogActionId
	FileDialogDeleteDialogActionId
	FileDialogCreateSnapshotDialogActionId
)

type FileActionDialog struct {
	application   *tview.Application
	file          *data.FileBrowserEntry
	layout        *tview.Flex
	actionChannel chan DialogActionId
}

func NewFileActionDialog(application *tview.Application, file *data.FileBrowserEntry) *FileActionDialog {
	dialog := &FileActionDialog{
		application:   application,
		file:          file,
		actionChannel: make(chan DialogActionId),
	}

	dialog.createLayout()

	return dialog
}

func (d *FileActionDialog) createLayout() {
	dialogTitle := " Select Action "

	textDescription := fmt.Sprintf("What do you want to do with '%s'?", d.file.Name)
	textDescriptionView := tview.NewTextView().SetText(textDescription)

	dialogOptions := buildFileDialogOptions(d.file, DiffBinExists())
	optionTable := createOptionTable(d.application, dialogOptions, d.selectAction)

	dialogContent := tview.NewFlex().SetDirection(tview.FlexRow)
	dialogContent.AddItem(textDescriptionView, 0, 1, false)
	dialogContent.AddItem(optionTable, 0, 1, true)

	dialog := createModal(dialogTitle, dialogContent, 50, 15)
	dialog.SetInputCapture(createOptionDialogInputCapture(optionTable, dialogOptions, d.selectAction, d.Close))
	d.layout = dialog
}

func buildFileDialogOptions(file *data.FileBrowserEntry, diffBinAvailable bool) []*DialogOption {
	dialogOptions := []*DialogOption{{
		Id:   DialogCloseActionId,
		Name: "Close",
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
			if diffBinAvailable && file.DiffState == diff_state.Modified {
				dialogOptions = slices.Insert(dialogOptions, 0, &DialogOption{
					Id:   FileDialogShowDiffActionId,
					Name: "🔍 Show diff",
				})
			}
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

func ensureDialogCloseIsLast(options []*DialogOption) []*DialogOption {
	closeIndex := slices.IndexFunc(options, func(option *DialogOption) bool {
		return option != nil && option.Id == DialogCloseActionId
	})
	if closeIndex < 0 || closeIndex == len(options)-1 {
		return options
	}

	closeOption := options[closeIndex]
	result := slices.Delete(options, closeIndex, closeIndex+1)
	result = append(result, closeOption)
	return result
}

func DiffBinExists() bool {
	_, err := exec.LookPath(DiffBinPath)
	if err != nil {
		return false
	}
	return true
}

func (d *FileActionDialog) GetName() string {
	return string(ActionDialog)
}

func (d *FileActionDialog) GetLayout() *tview.Flex {
	return d.layout
}

func (d *FileActionDialog) GetActionChannel() <-chan DialogActionId {
	return d.actionChannel
}

func (d *FileActionDialog) Close() {
	emitDialogActions(d.actionChannel, DialogCloseActionId)
}

func (d *FileActionDialog) RestoreFile() {
	emitDialogActions(d.actionChannel, DialogCloseActionId, FileDialogRestoreFileActionId)
}

func (d *FileActionDialog) selectAction(option *DialogOption) {
	switch option.Id {
	case FileDialogShowDiffActionId:
		d.ShowDiff()
	case FileDialogRestoreFileActionId:
		d.RestoreFile()
	case FileDialogRestoreRecursiveDialogActionId:
		d.RestoreRecursive()
	case FileDialogDeleteDialogActionId:
		d.DeleteFile()
	case FileDialogCreateSnapshotDialogActionId:
		d.CreateSnapshot()
	case DialogCloseActionId:
		d.Close()
	default:
		d.Close()
	}
}

func (d *FileActionDialog) RestoreRecursive() {
	emitDialogActions(d.actionChannel, DialogCloseActionId, FileDialogRestoreRecursiveDialogActionId)
}

func (d *FileActionDialog) DeleteFile() {
	emitDialogActions(d.actionChannel, DialogCloseActionId, FileDialogDeleteDialogActionId)
}

func (d *FileActionDialog) CreateSnapshot() {
	emitDialogActions(d.actionChannel, DialogCloseActionId, FileDialogCreateSnapshotDialogActionId)
}

func (d *FileActionDialog) ShowDiff() {
	emitDialogActions(d.actionChannel, DialogCloseActionId, FileDialogShowDiffActionId)
}
