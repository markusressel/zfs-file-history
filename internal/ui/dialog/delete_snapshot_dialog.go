package dialog

import (
	"fmt"
	"zfs-file-history/internal/data"
	"zfs-file-history/internal/ui/util"

	"github.com/rivo/tview"
)

const (
	DeleteSnapshotDialogPage util.Page = "DeleteSnapshotDialog"

	DeleteSnapshotDialogDeleteSnapshotActionId DialogActionId = iota
)

type DeleteSnapshotDialog struct {
	application   *tview.Application
	snapshot      *data.SnapshotBrowserEntry
	layout        *tview.Flex
	actionChannel chan DialogActionId
}

func NewDeleteSnapshotDialog(application *tview.Application, snapshot *data.SnapshotBrowserEntry) *DeleteSnapshotDialog {
	dialog := &DeleteSnapshotDialog{
		application:   application,
		snapshot:      snapshot,
		actionChannel: make(chan DialogActionId),
	}

	dialog.createLayout()

	return dialog
}

func (d *DeleteSnapshotDialog) createLayout() {
	dialogTitle := " 💥 Destroy Snapshot "

	textDescription := fmt.Sprintf("Destroy '%s'?", d.snapshot.Snapshot.Name)
	textDescriptionView := tview.NewTextView().SetText(textDescription)

	dialogOptions := buildConfirmDialogOptions(DeleteSnapshotDialogDeleteSnapshotActionId, "Destroy", true, DialogSeverityDanger)

	optionTable := createOptionTable(d.application, dialogOptions, d.selectAction)

	dialogContent := tview.NewFlex().SetDirection(tview.FlexRow)
	dialogContent.AddItem(textDescriptionView, 0, 1, false)
	dialogContent.AddItem(optionTable, 0, 1, true)

	dialog := createModal(dialogTitle, dialogContent, 50, 6)
	dialog.SetInputCapture(createOptionDialogInputCapture(optionTable, dialogOptions, d.selectAction, d.Close))
	d.layout = dialog
}

func (d *DeleteSnapshotDialog) GetName() string {
	return string(DeleteSnapshotDialogPage)
}

func (d *DeleteSnapshotDialog) GetLayout() *tview.Flex {
	return d.layout
}

func (d *DeleteSnapshotDialog) GetActionChannel() <-chan DialogActionId {
	return d.actionChannel
}

func (d *DeleteSnapshotDialog) Close() {
	emitDialogActions(d.actionChannel, DialogCloseActionId)
}

func (d *DeleteSnapshotDialog) selectAction(option *DialogOption) {
	switch option.Id {
	case DeleteSnapshotDialogDeleteSnapshotActionId:
		d.DeleteSnapshot()
	case DialogCloseActionId:
		d.Close()
	default:
		d.Close()
	}
}

func (d *DeleteSnapshotDialog) DeleteSnapshot() {
	emitDialogActions(d.actionChannel, DialogCloseActionId, DeleteSnapshotDialogDeleteSnapshotActionId)
}
