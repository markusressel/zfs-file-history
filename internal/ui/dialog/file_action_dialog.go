package dialog

import (
	"fmt"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"os/exec"
	"slices"
	"zfs-file-history/internal/data"
	"zfs-file-history/internal/data/diff_state"
	"zfs-file-history/internal/ui/util"
)

const (
	ActionDialog util.Page = "ActionDialog"

	// recursively restores all files and folders top to bottom starting with the given entry
	FileDialogShowDiffActionId DialogActionId = iota
	FileDialogRestoreFileActionId
	FileDialogRestoreRecursiveDialogActionId
	FileDialogDeleteDialogActionId
	FileDialogCreateSnapshotDialogActionId
)

type FileActionDialog struct {
	application   *tview.Application
	file          *data.FileBrowserEntry
	layout        *tview.Flex
	actionChannel chan DialogActionId
}

func NewFileActionDialog(application *tview.Application, file *data.FileBrowserEntry) *FileActionDialog {
	dialog := &FileActionDialog{
		application:   application,
		file:          file,
		actionChannel: make(chan DialogActionId),
	}

	dialog.createLayout()

	return dialog
}

func (d *FileActionDialog) createLayout() {
	dialogTitle := " Select Action "

	textDescription := fmt.Sprintf("What do you want to do with '%s'?", d.file.Name)
	textDescriptionView := tview.NewTextView().SetText(textDescription)

	optionTable := tview.NewTable()
	optionTable.SetSelectable(true, false)
	optionTable.Select(0, 0)

	dialogOptions := []*DialogOption{
		{
			Id:   DialogCloseActionId,
			Name: "Close",
		},
	}

	if d.file.HasReal() {
		dialogOptions = slices.Insert(dialogOptions, 0, &DialogOption{
			Id:   FileDialogDeleteDialogActionId,
			Name: fmt.Sprintf("Delete '%s'", d.file.RealFile.Name),
		})
	}

	if d.file.HasSnapshot() {
		if d.file.Type == data.Directory {
			dialogOptions = slices.Insert(dialogOptions, 0, &DialogOption{
				Id:   FileDialogRestoreFileActionId,
				Name: fmt.Sprintf("Restore directory only"),
			})
			dialogOptions = slices.Insert(dialogOptions, 0, &DialogOption{
				Id:   FileDialogRestoreRecursiveDialogActionId,
				Name: fmt.Sprintf("Restore directory recursively"),
			})
		}

		if d.file.Type == data.File {
			if DiffBinExists() && d.file.DiffState == diff_state.Modified {
				dialogOptions = slices.Insert(dialogOptions, 0, &DialogOption{
					Id:   FileDialogShowDiffActionId,
					Name: fmt.Sprintf("Show diff"),
				})
			}
			dialogOptions = slices.Insert(dialogOptions, 1, &DialogOption{
				Id:   FileDialogRestoreFileActionId,
				Name: fmt.Sprintf("Restore file"),
			})
		}
	}

	dialogOptions = slices.Insert(dialogOptions, 0, &DialogOption{
		Id:   FileDialogCreateSnapshotDialogActionId,
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

func DiffBinExists() bool {
	_, err := exec.LookPath(DiffBinPath)
	if err != nil {
		return false
	}
	return true
}

func (d *FileActionDialog) GetName() string {
	return string(ActionDialog)
}

func (d *FileActionDialog) GetLayout() *tview.Flex {
	return d.layout
}

func (d *FileActionDialog) GetActionChannel() <-chan DialogActionId {
	return d.actionChannel
}

func (d *FileActionDialog) Close() {
	go func() {
		d.actionChannel <- DialogCloseActionId
	}()
}

func (d *FileActionDialog) RestoreFile() {
	go func() {
		d.actionChannel <- DialogCloseActionId
		d.actionChannel <- FileDialogRestoreFileActionId
	}()
}

func (d *FileActionDialog) selectAction(option *DialogOption) {
	switch option.Id {
	case FileDialogShowDiffActionId:
		d.ShowDiff()
	case FileDialogRestoreFileActionId:
		d.RestoreFile()
	case FileDialogRestoreRecursiveDialogActionId:
		d.RestoreRecursive()
	case FileDialogDeleteDialogActionId:
		d.DeleteFile()
	case FileDialogCreateSnapshotDialogActionId:
		d.CreateSnapshot()
	case DialogCloseActionId:
		d.Close()
	}
}

func (d *FileActionDialog) RestoreRecursive() {
	go func() {
		d.actionChannel <- DialogCloseActionId
		d.actionChannel <- FileDialogRestoreRecursiveDialogActionId
	}()
}

func (d *FileActionDialog) DeleteFile() {
	go func() {
		d.actionChannel <- DialogCloseActionId
		d.actionChannel <- FileDialogDeleteDialogActionId
	}()
}

func (d *FileActionDialog) CreateSnapshot() {
	go func() {
		d.actionChannel <- DialogCloseActionId
		d.actionChannel <- FileDialogCreateSnapshotDialogActionId
	}()
}

func (d *FileActionDialog) ShowDiff() {
	go func() {
		d.actionChannel <- DialogCloseActionId
		d.actionChannel <- FileDialogShowDiffActionId
	}()
}
