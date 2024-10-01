package dialog

import (
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"github.com/sergi/go-diff/diffmatchpatch"
	"os"
	"os/exec"
	"zfs-file-history/internal/data"
	"zfs-file-history/internal/ui/util"
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
	dialogTitle := " File Diff "

	realFilePath := d.file.RealFile.Path
	snapshotFilePath := d.snapshot.Snapshot.GetSnapshotPath(d.file.RealFile.Path)
	diffText := computeDiffText(realFilePath, snapshotFilePath)

	output, err := exec.Command(
		"/usr/bin/diff",
		"-U", "4611686018427387903",
		snapshotFilePath,
		realFilePath,
	).Output()
	if err != nil {
		diffText = "error calculating diff: " + err.Error()
	}

	diffText = string(output)
	textDescriptionView := tview.NewTextView().SetText(diffText)

	dialogContent := tview.NewFlex().SetDirection(tview.FlexRow)
	dialogContent.AddItem(textDescriptionView, 0, 1, false)

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

func computeDiffText(path string, path2 string) string {
	text1, err := os.ReadFile(path)
	if err != nil {
		return "error calculating diff"
	}
	text2, err := os.ReadFile(path2)
	if err != nil {
		return "error calculating diff"
	}

	dmp := diffmatchpatch.New()
	diffs := dmp.DiffMain(string(text1), string(text2), false)
	return dmp.DiffPrettyText(diffs)
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
