package dialog

import (
	"fmt"
	"github.com/gdamore/tcell/v2"
	"github.com/navidys/tvxwidgets"
	"github.com/rivo/tview"
	"zfs-file-history/internal/data"
	"zfs-file-history/internal/ui/page"
)

const (
	RestoreFileProgress page.Page = "RestoreFileProgressDialog"
)

type RestoreFileProgressDialog struct {
	fileSelection *data.FileBrowserEntry
	layout        *tview.Flex
	actionChannel chan DialogAction
}

func NewRestoreFileProgressDialog(fileSelection *data.FileBrowserEntry) *RestoreFileProgressDialog {
	dialog := &RestoreFileProgressDialog{
		fileSelection: fileSelection,
	}

	dialog.createLayout()

	return dialog
}

func (d *RestoreFileProgressDialog) createLayout() {
	dialogTitle := " Restoring... "

	fileToRestore := d.fileSelection.SnapshotFiles[0]

	text := fmt.Sprintf("Restoring '%s' to '%s'", fileToRestore.Path, fileToRestore.OriginalPath)
	textDescription := tview.NewTextView().SetText(text)
	spinner := tvxwidgets.NewSpinner()

	progressLayout := tview.NewFlex().
		AddItem(textDescription, 0, 1, false).
		AddItem(spinner, 0, 1, false)

	dialog := createModal(dialogTitle, progressLayout, 40, 10)
	dialog.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Rune() == 'q' || event.Key() == tcell.KeyEscape {
			d.Close()
			return nil
		}
		return event
	})
	d.layout = dialog
}

func (d *RestoreFileProgressDialog) GetName() string {
	return string(RestoreFileProgress)
}

func (d *RestoreFileProgressDialog) GetLayout() *tview.Flex {
	return d.layout
}

func (d *RestoreFileProgressDialog) GetActionChannel() chan DialogAction {
	return d.actionChannel
}

func (d *RestoreFileProgressDialog) Close() {
	go func() {
		d.actionChannel <- ActionClose
	}()
}
