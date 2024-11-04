package dialog

import (
	"fmt"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"slices"
	"zfs-file-history/internal/data"
	"zfs-file-history/internal/ui/util"
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
	dialogTitle := " Destroy Snapshot "

	textDescription := fmt.Sprintf("Destroy '%s'?", d.snapshot.Snapshot.Name)
	textDescriptionView := tview.NewTextView().SetText(textDescription)

	optionTable := tview.NewTable()
	optionTable.SetSelectable(true, false)
	optionTable.Select(0, 0)

	dialogOptions := []*DialogOption{
		{
			Id:   DialogCloseActionId,
			Name: "Cancel",
		},
	}

	dialogOptions = slices.Insert(dialogOptions, 0, &DialogOption{
		Id:   DeleteSnapshotDialogDeleteSnapshotActionId,
		Name: "Destroy",
	})

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

	dialog := createModal(dialogTitle, dialogContent, 50, 6)
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
	go func() {
		d.actionChannel <- DialogCloseActionId
	}()
}

func (d *DeleteSnapshotDialog) selectAction(option *DialogOption) {
	switch option.Id {
	case DeleteSnapshotDialogDeleteSnapshotActionId:
		d.DeleteSnapshot()
	case DialogCloseActionId:
		d.Close()
	}
}

func (d *DeleteSnapshotDialog) DeleteSnapshot() {
	go func() {
		d.actionChannel <- DialogCloseActionId
		d.actionChannel <- DeleteSnapshotDialogDeleteSnapshotActionId
	}()
}
