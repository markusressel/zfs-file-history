package dialog

import (
	"zfs-file-history/internal/data"
	"zfs-file-history/internal/ui/util"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

const (
	FileDiffDialogPage util.Page = "FileDiffDialog"
)

type FileDiffDialog struct {
	application   *tview.Application
	file          *data.FileBrowserEntry
	snapshot      *data.SnapshotBrowserEntry
	layout        *tview.Flex
	actionChannel chan DialogActionId
}

func NewFileDiffDialog(application *tview.Application, file *data.FileBrowserEntry, snapshot *data.SnapshotBrowserEntry) *FileDiffDialog {
	dialog := &FileDiffDialog{
		application:   application,
		file:          file,
		snapshot:      snapshot,
		actionChannel: make(chan DialogActionId),
	}

	dialog.createLayout()

	return dialog
}

func (d *FileDiffDialog) createLayout() {
	dialogTitle := " 🔍 File Diff "

	realFilePath := d.file.RealFile.Path
	snapshotFilePath := d.snapshot.Snapshot.GetSnapshotPath(d.file.RealFile.Path)

	var diffText string
	isBinary := IsBinaryFile(snapshotFilePath) || IsBinaryFile(realFilePath)
	if isBinary {
		diffText = "Binary files differ, content preview not available."
	} else {
		var err error
		diffText, err = RunDiff(snapshotFilePath, realFilePath)
		if err != nil {
			diffText = "error calculating diff: " + err.Error()
		} else {
			diffText = FormatDiffText(diffText, false)
		}
	}

	textDescriptionView := tview.NewTextView().
		SetDynamicColors(true).
		SetRegions(true).
		SetChangedFunc(func() {
			d.application.Draw()
		})

	textDescriptionView.SetText(diffText)

	closeTextView := util.CreateAttentionTextView("Press 'esc' to close")

	dialogContent := tview.NewFlex().SetDirection(tview.FlexRow)
	dialogContent.AddItem(textDescriptionView, 0, 1, true)
	dialogContent.AddItem(closeTextView, 1, 0, false)
	dialogContent.SetBorderPadding(0, 0, 1, 1)

	width, height := CalculateDialogSize(DialogSizeConstraints{
		Title:             dialogTitle,
		ExtraContentWidth: 74,
		StaticHeight:      18, // Sane content height for scrollable diff text
	})

	dialog := createModal(dialogTitle, dialogContent, width, height)
	dialog.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyEscape {
			d.Close()
			return nil
		}
		return event
	})
	d.layout = dialog
}

func (d *FileDiffDialog) GetName() string {
	return string(FileDiffDialogPage)
}

func (d *FileDiffDialog) GetLayout() *tview.Flex {
	return d.layout
}

func (d *FileDiffDialog) GetActionChannel() <-chan DialogActionId {
	return d.actionChannel
}

func (d *FileDiffDialog) Close() {
	go func() {
		d.actionChannel <- DialogCloseActionId
	}()
}
