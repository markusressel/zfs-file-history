package dialog

import (
	"fmt"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"golang.org/x/exp/slices"
	"zfs-file-history/internal/data"
	"zfs-file-history/internal/ui/util"
)

const (
	SnapshotActionDialogPage util.Page = "SnapshotActionDialog"

	SnapshotActionDialogCreateSnapshotDialogAction DialogAction = iota
	DestroySnapshotDialogAction
	DestroySnapshotRecursivelyDialogAction
)

type SnapshotActionDialog struct {
	application   *tview.Application
	snapshot      *data.SnapshotBrowserEntry
	layout        *tview.Flex
	actionChannel chan DialogAction
}

func NewSnapshotActionDialog(application *tview.Application, snapshot *data.SnapshotBrowserEntry) *SnapshotActionDialog {
	dialog := &SnapshotActionDialog{
		application:   application,
		snapshot:      snapshot,
		actionChannel: make(chan DialogAction),
	}

	dialog.createLayout()

	return dialog
}

const (
	CreateSnapshotDialogOptionId DialogOptionId = iota
	DestroySnapshotDialogOptionId
	DestroySnapshotRecursivelyDialogOptionId
)

func (d *SnapshotActionDialog) createLayout() {
	dialogTitle := " Select Action "

	textDesctiption := fmt.Sprintf("What do you want to do with '%s'?", d.snapshot.Snapshot.Name)
	textDesctiptionView := tview.NewTextView().SetText(textDesctiption)

	optionTable := tview.NewTable()
	optionTable.SetSelectable(true, false)
	optionTable.Select(0, 0)

	dialogOptions := []*DialogOption{
		{
			Id:   CloseDialogOptionId,
			Name: "Close",
		},
	}

	createSnapshotDialogOption := &DialogOption{
		Id:   CreateSnapshotDialogOptionId,
		Name: fmt.Sprintf("Create Snapshot"),
	}
	dialogOptions = slices.Insert(dialogOptions, 0, createSnapshotDialogOption)

	destroySnapshotDialogOption := &DialogOption{
		Id:   DestroySnapshotDialogOptionId,
		Name: fmt.Sprintf("Destroy '%s'", d.snapshot.Snapshot.Name),
	}
	dialogOptions = slices.Insert(dialogOptions, 0, destroySnapshotDialogOption)

	DestroySnapshotRecursivelyDialogOption := &DialogOption{
		Id:   DestroySnapshotRecursivelyDialogOptionId,
		Name: fmt.Sprintf("Destroy (recursive) '%s'", d.snapshot.Snapshot.Name),
	}
	dialogOptions = slices.Insert(dialogOptions, 0, DestroySnapshotRecursivelyDialogOption)

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
		var cellExpansion = 0

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
	dialogContent.SetBorderPadding(0, 0, 1, 1)
	dialogContent.AddItem(textDesctiptionView, 0, 1, false)
	dialogContent.AddItem(optionTable, 0, 1, true)

	dialog := createModal(dialogTitle, dialogContent, 50, 15)
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

func (d *SnapshotActionDialog) GetActionChannel() <-chan DialogAction {
	return d.actionChannel
}

func (d *SnapshotActionDialog) Close() {
	go func() {
		d.actionChannel <- ActionClose
	}()
}

func (d *SnapshotActionDialog) selectAction(option *DialogOption) {
	switch option.Id {
	case CreateSnapshotDialogOptionId:
		d.CreateSnapshot()
	case DestroySnapshotDialogOptionId:
		d.DestroySnapshot()
	case DestroySnapshotRecursivelyDialogOptionId:
		d.DestroySnapshotRecursively()
	case CloseDialogOptionId:
		d.Close()
	}
}

func (d *SnapshotActionDialog) CreateSnapshot() {
	go func() {
		d.actionChannel <- ActionClose
		d.actionChannel <- CreateSnapshotDialogAction
	}()
}

func (d *SnapshotActionDialog) DestroySnapshot() {
	go func() {
		d.actionChannel <- ActionClose
		d.actionChannel <- DestroySnapshotDialogAction
	}()
}

func (d *SnapshotActionDialog) DestroySnapshotRecursively() {
	go func() {
		d.actionChannel <- ActionClose
		d.actionChannel <- DestroySnapshotRecursivelyDialogAction
	}()
}
