package dialog

import (
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"os/exec"
	"strings"
	"zfs-file-history/internal/data"
	"zfs-file-history/internal/ui/util"
)

const (
	DiffBinPath = "/usr/bin/diff"

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
	dialogTitle := " File Diff "

	realFilePath := d.file.RealFile.Path
	snapshotFilePath := d.snapshot.Snapshot.GetSnapshotPath(d.file.RealFile.Path)

	output, err := exec.Command(
		DiffBinPath,
		"-U", "3",
		snapshotFilePath,
		realFilePath,
	).Output()
	diffText := string(output)
	if err != nil && err.Error() != "exit status 1" {
		diffText = "error calculating diff: " + err.Error()
	}

	diffTextLines := strings.Split(diffText, "\n")
	for i := 0; i < len(diffTextLines); i++ {
		line := diffTextLines[i]
		if strings.HasPrefix(line, "+") {
			diffTextLines[i] = `[green]` + line + `[white]`
		}
		if strings.HasPrefix(line, "-") {
			diffTextLines[i] = `[red]` + line + `[white]`
		}
	}
	diffText = strings.Join(diffTextLines, "\n")

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

	width := 80
	height := 20
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
