package dialog

import (
	"fmt"
	"zfs-file-history/internal/data"
	"zfs-file-history/internal/ui/util"

	"github.com/gdamore/tcell/v2"
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

	optionTable := tview.NewTable()
	optionTable.SetSelectable(true, false)
	optionTable.Select(0, 0)

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
	go func() {
		d.actionChannel <- DialogCloseActionId
	}()
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
		d.Close()
	}
}

func (d *SnapshotActionDialog) CreateSnapshot() {
	go func() {
		d.actionChannel <- DialogCloseActionId
		d.actionChannel <- SnapshotDialogCreateSnapshotActionId
	}()
}

func (d *SnapshotActionDialog) DestroySnapshot() {
	go func() {
		d.actionChannel <- DialogCloseActionId
		d.actionChannel <- SnapshotDialogDestroySnapshotActionId
	}()
}

func (d *SnapshotActionDialog) DestroySnapshotRecursively() {
	go func() {
		d.actionChannel <- DialogCloseActionId
		d.actionChannel <- SnapshotDialogDestroySnapshotRecursivelyActionId
	}()
}
