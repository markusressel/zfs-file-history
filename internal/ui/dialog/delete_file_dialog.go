package dialog

import (
	"fmt"
	"slices"
	"zfs-file-history/internal/data"
	"zfs-file-history/internal/ui/util"

	"github.com/gdamore/tcell/v2"
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
	dialogTitle := " Delete File "

	textDescription := fmt.Sprintf("Delete '%s'?", d.file.Name)
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

	if d.file.HasReal() {
		dialogOptions = slices.Insert(dialogOptions, 0, &DialogOption{
			Id:   DeleteFileDialogDeleteFileActionId,
			Name: "Delete",
		})
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
	go func() {
		d.actionChannel <- DialogCloseActionId
	}()
}

func (d *DeleteFileDialog) selectAction(option *DialogOption) {
	switch option.Id {
	case DeleteFileDialogDeleteFileActionId:
		d.DeleteFile()
	case DialogCloseActionId:
		d.Close()
	}
}

func (d *DeleteFileDialog) DeleteFile() {
	go func() {
		d.actionChannel <- DialogCloseActionId
		d.actionChannel <- DeleteFileDialogDeleteFileActionId
	}()
}
