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

type RestoreFileDialog struct {
	application   *tview.Application
	file          *data.FileBrowserEntry
	layout        *tview.Flex
	actionChannel chan DialogActionId
}

func NewRestoreFileDialog(application *tview.Application, file *data.FileBrowserEntry) *RestoreFileDialog {
	dialog := &RestoreFileDialog{
		application:   application,
		file:          file,
		actionChannel: make(chan DialogActionId),
	}

	dialog.createLayout()

	return dialog
}

func (d *RestoreFileDialog) createLayout() {
	dialogTitle := " ♻️ Restore File "

	textDescription := fmt.Sprintf("Restore '%s'?", d.file.Name)
	textDescriptionView := tview.NewTextView().SetText(textDescription)

	dialogOptions := buildRestoreDialogOptions(d.file)

	optionTable := createOptionTable(d.application, dialogOptions, d.selectAction)

	dialogContent := tview.NewFlex().SetDirection(tview.FlexRow)
	dialogContent.AddItem(textDescriptionView, 0, 1, false)
	dialogContent.AddItem(optionTable, 0, 1, true)

	dialog := createModal(dialogTitle, dialogContent, 50, 6)
	dialog.SetInputCapture(createOptionDialogInputCapture(optionTable, dialogOptions, d.selectAction, d.Close))
	d.layout = dialog
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

func (d *RestoreFileDialog) GetName() string {
	return string(RestoreFileDialogPage)
}

func (d *RestoreFileDialog) GetLayout() *tview.Flex {
	return d.layout
}

func (d *RestoreFileDialog) GetActionChannel() <-chan DialogActionId {
	return d.actionChannel
}

func (d *RestoreFileDialog) Close() {
	emitDialogActions(d.actionChannel, DialogCloseActionId)
}

func (d *RestoreFileDialog) selectAction(option *DialogOption) {
	switch option.Id {
	case RestoreFileDialogRestoreFileActionId:
		d.RestoreFile()
	case RestoreFileDialogRestoreRecursiveActionId:
		d.RestoreFileRecursive()
	case DialogCloseActionId:
		d.Close()
	default:
		d.Close()
	}
}

func (d *RestoreFileDialog) RestoreFile() {
	emitDialogActions(d.actionChannel, DialogCloseActionId, RestoreFileDialogRestoreFileActionId)
}

func (d *RestoreFileDialog) RestoreFileRecursive() {
	emitDialogActions(d.actionChannel, DialogCloseActionId, RestoreFileDialogRestoreRecursiveActionId)
}
