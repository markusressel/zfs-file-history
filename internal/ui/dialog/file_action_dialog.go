package dialog

import (
	"fmt"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"zfs-file-history/internal/data"
	"zfs-file-history/internal/ui/page"
)

const (
	ActionDialog page.Page = "ActionDialog"

	RestoreAction DialogAction = iota
)

type FileActionDialog struct {
	file          *data.FileBrowserEntry
	layout        *tview.Flex
	actionChannel chan DialogAction
}

func NewFileActionDialog(file *data.FileBrowserEntry) *FileActionDialog {
	dialog := &FileActionDialog{
		file:          file,
		actionChannel: make(chan DialogAction),
	}

	dialog.createLayout()

	return dialog
}

type DialogOptionId int

const (
	RestoreFileDialogOption DialogOptionId = iota
)

func (d *FileActionDialog) createLayout() {
	dialogTitle := " Select Action "

	optionTable := tview.NewTable()
	optionTable.SetBorderPadding(0, 0, 1, 1)
	optionTable.SetSelectable(true, false)
	optionTable.Select(0, 0)
	optionTable.SetSelectedFunc(func(row, column int) {

	})

	dialogOptions := []*DialogOption{}

	if len(d.file.SnapshotFiles) > 0 {
		restoreOption := &DialogOption{
			Id:   RestoreFileDialogOption,
			Name: fmt.Sprintf("Restore from '%s'", d.file.SnapshotFiles[0].Snapshot.Name),
		}
		dialogOptions = append(dialogOptions, restoreOption)
	}

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

	dialog := createModal(dialogTitle, optionTable, 40, 10)
	dialog.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Rune() == 'q' || event.Key() == tcell.KeyEscape {
			d.Close()
			return nil
		} else if event.Key() == tcell.KeyEnter {
			row, _ := optionTable.GetSelection()
			dialogOption := dialogOptions[row]
			if dialogOption.Name == "Restore" {
				d.RestoreFile()
			}
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
