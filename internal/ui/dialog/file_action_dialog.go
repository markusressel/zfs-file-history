package dialog

import (
	"fmt"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"golang.org/x/exp/slices"
	"os"
	"zfs-file-history/internal/data"
	"zfs-file-history/internal/logging"
	"zfs-file-history/internal/ui/util"
)

const (
	ActionDialog util.Page = "ActionDialog"

	RestoreAction DialogAction = iota
)

type FileActionDialog struct {
	application   *tview.Application
	file          *data.FileBrowserEntry
	layout        *tview.Flex
	actionChannel chan DialogAction
}

func NewFileActionDialog(application *tview.Application, file *data.FileBrowserEntry) *FileActionDialog {
	dialog := &FileActionDialog{
		application:   application,
		file:          file,
		actionChannel: make(chan DialogAction),
	}

	dialog.createLayout()

	return dialog
}

type DialogOptionId int

const (
	RestoreFileDialogOption DialogOptionId = iota
	DeleteFileDialogOption
	CloseDialogOption
)

func (d *FileActionDialog) createLayout() {
	dialogTitle := " Select Action "

	textDesctiption := fmt.Sprintf("What do you want to do with '%s'?", d.file.Name)
	textDesctiptionView := tview.NewTextView().SetText(textDesctiption)

	optionTable := tview.NewTable()
	optionTable.SetSelectable(true, false)
	optionTable.Select(0, 0)

	dialogOptions := []*DialogOption{
		{
			Id:   CloseDialogOption,
			Name: "Close",
		},
	}

	if d.file.HasReal() {
		option := &DialogOption{
			Id:   DeleteFileDialogOption,
			Name: fmt.Sprintf("Delete '%s'", d.file.RealFile.Name),
		}
		dialogOptions = slices.Insert(dialogOptions, 0, option)
	}

	if d.file.HasSnapshot() {
		option := &DialogOption{
			Id:   RestoreFileDialogOption,
			Name: fmt.Sprintf("Restore from '%s'", d.file.SnapshotFiles[0].Snapshot.Name),
		}
		dialogOptions = slices.Insert(dialogOptions, 0, option)
	}

	optionTable.SetMouseCapture(func(action tview.MouseAction, event *tcell.EventMouse) (tview.MouseAction, *tcell.EventMouse) {
		switch action {
		case tview.MouseLeftDoubleClick:
			go func() {
				d.application.QueueUpdateDraw(func() {
					row, _ := optionTable.GetSelection()
					dialogOption := dialogOptions[row]
					d.selectAction(dialogOption)
				})
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

		cellText = fmt.Sprintf("%s", columnTitle.Name)

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
		if event.Rune() == 'q' || event.Key() == tcell.KeyEscape {
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

func (d *FileActionDialog) GetName() string {
	return string(ActionDialog)
}

func (d *FileActionDialog) GetLayout() *tview.Flex {
	return d.layout
}

func (d *FileActionDialog) GetActionChannel() chan DialogAction {
	return d.actionChannel
}

func (d *FileActionDialog) Close() {
	go func() {
		d.actionChannel <- ActionClose
	}()
}

func (d *FileActionDialog) RestoreFile() {
	go func() {
		d.actionChannel <- ActionClose
		d.actionChannel <- RestoreAction
	}()
}

func (d *FileActionDialog) DeleteFile() {
	go func() {
		path := d.file.RealFile.Path
		err := os.RemoveAll(path)
		if err != nil {
			logging.Error(err.Error())
		}

		d.actionChannel <- ActionClose
	}()
}

func (d *FileActionDialog) selectAction(option *DialogOption) {
	switch option.Id {
	case RestoreFileDialogOption:
		d.RestoreFile()
	case DeleteFileDialogOption:
		d.DeleteFile()
	case CloseDialogOption:
		d.Close()
	}
}
