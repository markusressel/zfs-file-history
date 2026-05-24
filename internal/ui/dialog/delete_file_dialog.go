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

type DeleteFileDialog struct {
	application   *tview.Application
	file          *data.FileBrowserEntry
	layout        *tview.Flex
	actionChannel chan DialogActionId
}

func NewDeleteFileDialog(application *tview.Application, file *data.FileBrowserEntry) *DeleteFileDialog {
	dialog := &DeleteFileDialog{
		application:   application,
		file:          file,
		actionChannel: make(chan DialogActionId),
	}

	dialog.createLayout()

	return dialog
}

func (d *DeleteFileDialog) createLayout() {
	dialogTitle := " 🗑️ Delete File "

	textDescription := fmt.Sprintf("Delete '%s'?", d.file.Name)
	textDescriptionView := tview.NewTextView().SetText(textDescription)

	dialogOptions := buildConfirmDialogOptions(DeleteFileDialogDeleteFileActionId, localization.LocalizationCommonDelete, d.file.HasReal(), DialogSeverityDanger)

	optionTable := createOptionTable(d.application, dialogOptions, d.selectAction)

	dialogContent := tview.NewFlex().SetDirection(tview.FlexRow)
	dialogContent.AddItem(textDescriptionView, 0, 1, false)
	dialogContent.AddItem(optionTable, 0, 1, true)

	dialog := createModal(dialogTitle, dialogContent, 50, 6)
	dialog.SetInputCapture(createOptionDialogInputCapture(optionTable, dialogOptions, d.selectAction, d.Close))
	d.layout = dialog
}

func (d *DeleteFileDialog) GetName() string {
	return string(DeleteFileDialogPage)
}

func (d *DeleteFileDialog) GetLayout() *tview.Flex {
	return d.layout
}

func (d *DeleteFileDialog) GetActionChannel() <-chan DialogActionId {
	return d.actionChannel
}

func (d *DeleteFileDialog) Close() {
	emitDialogActions(d.actionChannel, DialogCloseActionId)
}

func (d *DeleteFileDialog) selectAction(option *DialogOption) {
	switch option.Id {
	case DeleteFileDialogDeleteFileActionId:
		d.DeleteFile()
	case DialogCloseActionId:
		d.Close()
	default:
		d.Close()
	}
}

func (d *DeleteFileDialog) DeleteFile() {
	emitDialogActions(d.actionChannel, DialogCloseActionId, DeleteFileDialogDeleteFileActionId)
}
