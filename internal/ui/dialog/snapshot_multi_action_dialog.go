package dialog

import (
	"fmt"
	"zfs-file-history/internal/data"
	"zfs-file-history/internal/ui/util"

	"github.com/rivo/tview"
)

const (
	MultiSnapshotActionDialogPage util.Page = "MultiSnapshotActionDialog"

	MultiSnapshotDialogClearSelectionActionId DialogActionId = iota
	MultiSnapshotDialogDestroySnapshotActionId
	MultiSnapshotDialogDestroySnapshotRecursivelyActionId
)

type MultiSnapshotActionDialog struct {
	application   *tview.Application
	snapshots     []*data.SnapshotBrowserEntry
	layout        *tview.Flex
	actionChannel chan DialogActionId
}

func NewMultiSnapshotActionDialog(application *tview.Application, snapshots []*data.SnapshotBrowserEntry) *MultiSnapshotActionDialog {
	dialog := &MultiSnapshotActionDialog{
		application:   application,
		snapshots:     snapshots,
		actionChannel: make(chan DialogActionId),
	}

	dialog.createLayout()

	return dialog
}

func (d *MultiSnapshotActionDialog) createLayout() {
	dialogTitle := " Select Action "

	snapshotNames := make([]string, 0)
	for _, snapshot := range d.snapshots {
		snapshotNames = append(snapshotNames, snapshot.Snapshot.Name)
	}

	textDescription := fmt.Sprintf("What do you want to do with '%v'?", snapshotNames)
	textDescriptionView := tview.NewTextView().SetText(textDescription)

	dialogOptions := []*DialogOption{
		{
			Id:   MultiSnapshotDialogDestroySnapshotActionId,
			Name: "Destroy all",
		},
		{
			Id:   MultiSnapshotDialogDestroySnapshotRecursivelyActionId,
			Name: "Destroy all (recursive)",
		},
		{
			Id:   MultiSnapshotDialogClearSelectionActionId,
			Name: "Clear Selection",
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

func (d *MultiSnapshotActionDialog) GetName() string {
	return string(MultiSnapshotActionDialogPage)
}

func (d *MultiSnapshotActionDialog) GetLayout() *tview.Flex {
	return d.layout
}

func (d *MultiSnapshotActionDialog) GetActionChannel() <-chan DialogActionId {
	return d.actionChannel
}

func (d *MultiSnapshotActionDialog) Close() {
	emitDialogActions(d.actionChannel, DialogCloseActionId)
}

func (d *MultiSnapshotActionDialog) selectAction(option *DialogOption) {
	switch option.Id {
	case MultiSnapshotDialogClearSelectionActionId:
		d.ClearSelection()
	case MultiSnapshotDialogDestroySnapshotActionId:
		d.DestroyAllSnapshots()
	case MultiSnapshotDialogDestroySnapshotRecursivelyActionId:
		d.DestroyAllSnapshotsRecursively()
	case DialogCloseActionId:
		d.Close()
	default:
		d.Close()
	}
}

func (d *MultiSnapshotActionDialog) ClearSelection() {
	emitDialogActions(d.actionChannel, DialogCloseActionId, MultiSnapshotDialogClearSelectionActionId)
}

func (d *MultiSnapshotActionDialog) DestroyAllSnapshots() {
	emitDialogActions(d.actionChannel, DialogCloseActionId, MultiSnapshotDialogDestroySnapshotActionId)
}

func (d *MultiSnapshotActionDialog) DestroyAllSnapshotsRecursively() {
	emitDialogActions(d.actionChannel, DialogCloseActionId, MultiSnapshotDialogDestroySnapshotRecursivelyActionId)
}
