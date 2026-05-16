package dialog

import (
	"fmt"
	"zfs-file-history/internal/data"
	"zfs-file-history/internal/ui/util"

	"github.com/rivo/tview"
)

const (
	SnapshotActionDialogPage util.Page = "SnapshotActionDialog"

	SnapshotDialogCreateSnapshotActionId DialogActionId = iota
	SnapshotDialogDestroySnapshotActionId
	SnapshotDialogDestroySnapshotRecursivelyActionId
)

type SnapshotActionDialog struct {
	application   *tview.Application
	snapshot      *data.SnapshotBrowserEntry
	layout        *tview.Flex
	actionChannel chan DialogActionId
}

func NewSnapshotActionDialog(application *tview.Application, snapshot *data.SnapshotBrowserEntry) *SnapshotActionDialog {
	dialog := &SnapshotActionDialog{
		application:   application,
		snapshot:      snapshot,
		actionChannel: make(chan DialogActionId),
	}

	dialog.createLayout()

	return dialog
}

func (d *SnapshotActionDialog) createLayout() {
	dialogTitle := " Select Action "

	textDescription := fmt.Sprintf("What do you want to do with '%s'?", d.snapshot.Snapshot.Name)
	textDescriptionView := tview.NewTextView().SetText(textDescription)

	dialogOptions := []*DialogOption{
		{
			Id:   SnapshotDialogCreateSnapshotActionId,
			Name: "Create Snapshot",
		},
		{
			Id:   SnapshotDialogDestroySnapshotActionId,
			Name: fmt.Sprintf("Destroy '%s'", d.snapshot.Snapshot.Name),
		},
		{
			Id:   SnapshotDialogDestroySnapshotRecursivelyActionId,
			Name: fmt.Sprintf("Destroy (recursive) '%s'", d.snapshot.Snapshot.Name),
		},
		{
			Id:   DialogCloseActionId,
			Name: "Close",
		},
	}

	optionTable := createOptionTable(d.application, dialogOptions, d.selectAction)

	dialogContent := tview.NewFlex().SetDirection(tview.FlexRow)
	dialogContent.AddItem(textDescriptionView, 0, 1, false)
	dialogContent.AddItem(optionTable, 0, 1, true)

	dialog := createModal(dialogTitle, dialogContent, 50, 10)
	dialog.SetInputCapture(createOptionDialogInputCapture(optionTable, dialogOptions, d.selectAction, d.Close))
	d.layout = dialog
}

func (d *SnapshotActionDialog) GetName() string {
	return string(SnapshotActionDialogPage)
}

func (d *SnapshotActionDialog) GetLayout() *tview.Flex {
	return d.layout
}

func (d *SnapshotActionDialog) GetActionChannel() <-chan DialogActionId {
	return d.actionChannel
}

func (d *SnapshotActionDialog) Close() {
	emitDialogActions(d.actionChannel, DialogCloseActionId)
}

func (d *SnapshotActionDialog) selectAction(option *DialogOption) {
	switch option.Id {
	case SnapshotDialogCreateSnapshotActionId:
		d.CreateSnapshot()
	case SnapshotDialogDestroySnapshotActionId:
		d.DestroySnapshot()
	case SnapshotDialogDestroySnapshotRecursivelyActionId:
		d.DestroySnapshotRecursively()
	case DialogCloseActionId:
	default:
		d.Close()
	}
}

func (d *SnapshotActionDialog) CreateSnapshot() {
	emitDialogActions(d.actionChannel, DialogCloseActionId, SnapshotDialogCreateSnapshotActionId)
}

func (d *SnapshotActionDialog) DestroySnapshot() {
	emitDialogActions(d.actionChannel, DialogCloseActionId, SnapshotDialogDestroySnapshotActionId)
}

func (d *SnapshotActionDialog) DestroySnapshotRecursively() {
	emitDialogActions(d.actionChannel, DialogCloseActionId, SnapshotDialogDestroySnapshotRecursivelyActionId)
}
