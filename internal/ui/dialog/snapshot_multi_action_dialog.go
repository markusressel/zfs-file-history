package dialog

import (
	"fmt"
	"zfs-file-history/internal/data"
	"zfs-file-history/internal/ui/util"

	"github.com/gdamore/tcell/v2"
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

	optionTable := tview.NewTable()
	optionTable.SetSelectable(true, false)
	optionTable.Select(0, 0)

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

	optionTable.SetMouseCapture(func(action tview.MouseAction, event *tcell.EventMouse) (tview.MouseAction, *tcell.EventMouse) {
		switch action {
		case tview.MouseLeftDoubleClick:
			go func() {
				row, _ := optionTable.GetSelection()
				dialogOption := dialogOptions[row]
				d.selectAction(dialogOption)
				d.application.Draw()
			}()
			return action, nil
		}
		return action, event
	})

	_, rows := 1, len(dialogOptions)
	fileIndex := 0
	for row := 0; row < rows; row++ {
		columnTitle := dialogOptions[row]

		var cellColor = tcell.ColorWhite
		var cellText string
		var cellAlignment = tview.AlignLeft
		var cellExpansion = 1

		cellText = columnTitle.Name

		optionTable.SetCell(row, 0,
			tview.NewTableCell(cellText).
				SetTextColor(cellColor).
				SetAlign(cellAlignment).
				SetExpansion(cellExpansion),
		)
		fileIndex = (fileIndex + 1) % rows
	}

	dialogContent := tview.NewFlex().SetDirection(tview.FlexRow)
	dialogContent.AddItem(textDescriptionView, 0, 1, false)
	dialogContent.AddItem(optionTable, 0, 1, true)

	dialog := createModal(dialogTitle, dialogContent, 50, 10)
	dialog.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyEscape {
			d.Close()
			return nil
		} else if event.Key() == tcell.KeyEnter {
			row, _ := optionTable.GetSelection()
			dialogOption := dialogOptions[row]
			d.selectAction(dialogOption)
			return nil
		}
		return event
	})
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
	go func() {
		d.actionChannel <- DialogCloseActionId
	}()
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
	}
}

func (d MultiSnapshotActionDialog) ClearSelection() {
	go func() {
		d.actionChannel <- DialogCloseActionId
		d.actionChannel <- MultiSnapshotDialogClearSelectionActionId
	}()
}

func (d *MultiSnapshotActionDialog) DestroyAllSnapshots() {
	go func() {
		d.actionChannel <- DialogCloseActionId
		d.actionChannel <- MultiSnapshotDialogDestroySnapshotActionId
	}()
}

func (d *MultiSnapshotActionDialog) DestroyAllSnapshotsRecursively() {
	go func() {
		d.actionChannel <- DialogCloseActionId
		d.actionChannel <- MultiSnapshotDialogDestroySnapshotRecursivelyActionId
	}()
}
