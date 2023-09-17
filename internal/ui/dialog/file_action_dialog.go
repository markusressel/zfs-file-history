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
	ActionDialog util.Page = "ActionDialog"

	// recursively restores all files and folders top to bottom starting with the given entry
	RestoreFileDialogAction DialogAction = iota
	RestoreRecursiveDialogAction
	DeleteDialogAction
	CreateSnapshotDialogAction
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
	RestoreSingleDialogOption DialogOptionId = iota
	RestoreRecursiveDialogOption
	DeleteDialogOption
	CreateSnapshotDialogOption
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
		dialogOptions = slices.Insert(dialogOptions, 0, &DialogOption{
			Id:   DeleteDialogOption,
			Name: fmt.Sprintf("Delete '%s'", d.file.RealFile.Name),
		})
	}

	if d.file.HasSnapshot() {
		if d.file.Type == data.Directory {
			dialogOptions = slices.Insert(dialogOptions, 0, &DialogOption{
				Id:   RestoreRecursiveDialogOption,
				Name: fmt.Sprintf("Restore directory recursively"),
			})
			dialogOptions = slices.Insert(dialogOptions, 0, &DialogOption{
				Id:   RestoreSingleDialogOption,
				Name: fmt.Sprintf("Restore directory only"),
			})
		}

		if d.file.Type == data.File {
			dialogOptions = slices.Insert(dialogOptions, 0, &DialogOption{
				Id:   RestoreSingleDialogOption,
				Name: fmt.Sprintf("Restore file"),
			})
		}
	}

	dialogOptions = slices.Insert(dialogOptions, 0, &DialogOption{
		Id:   CreateSnapshotDialogOption,
		Name: fmt.Sprintf("Create Snapshot"),
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

func (d *FileActionDialog) GetName() string {
	return string(ActionDialog)
}

func (d *FileActionDialog) GetLayout() *tview.Flex {
	return d.layout
}

func (d *FileActionDialog) GetActionChannel() <-chan DialogAction {
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
		d.actionChannel <- RestoreFileDialogAction
	}()
}

func (d *FileActionDialog) selectAction(option *DialogOption) {
	switch option.Id {
	case RestoreSingleDialogOption:
		d.RestoreFile()
	case RestoreRecursiveDialogOption:
		d.RestoreRecursive()
	case DeleteDialogOption:
		d.DeleteFile()
	case CreateSnapshotDialogOption:
		d.CreateSnapshot()
	case CloseDialogOption:
		d.Close()
	}
}

func (d *FileActionDialog) RestoreRecursive() {
	go func() {
		d.actionChannel <- ActionClose
		d.actionChannel <- RestoreRecursiveDialogAction
	}()
}

func (d *FileActionDialog) DeleteFile() {
	go func() {
		d.actionChannel <- ActionClose
		d.actionChannel <- DeleteDialogAction
	}()
}

func (d *FileActionDialog) CreateSnapshot() {
	go func() {
		d.actionChannel <- ActionClose
		d.actionChannel <- CreateSnapshotDialogAction
	}()
}
