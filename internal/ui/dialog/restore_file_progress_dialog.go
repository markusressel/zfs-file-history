package dialog

import (
	"fmt"
	"github.com/gdamore/tcell/v2"
	"github.com/navidys/tvxwidgets"
	"github.com/rivo/tview"
	"time"
	"zfs-file-history/internal/data"
	"zfs-file-history/internal/ui/page"
	uiutil "zfs-file-history/internal/ui/util"
)

const (
	RestoreFileProgress page.Page = "RestoreFileProgressDialog"
)

type RestoreFileProgressDialog struct {
	application   *tview.Application
	fileSelection *data.FileBrowserEntry
	layout        *tview.Flex
	actionChannel chan DialogAction
}

func NewRestoreFileProgressDialog(application *tview.Application, fileSelection *data.FileBrowserEntry) *RestoreFileProgressDialog {
	dialog := &RestoreFileProgressDialog{
		application:   application,
		fileSelection: fileSelection,
		actionChannel: make(chan DialogAction),
	}

	dialog.createLayout()

	return dialog
}

func (d *RestoreFileProgressDialog) createLayout() {
	dialogTitle := " Restoring... "

	fileToRestore := d.fileSelection.SnapshotFiles[0]

	text := fmt.Sprintf("Restoring '%s' from snapshot '%s'", d.fileSelection.Name, fileToRestore.Snapshot.Name)
	descriptionTextView := tview.NewTextView().SetText(text)

	spinner := tvxwidgets.NewSpinner().SetStyle(tvxwidgets.SpinnerCircleQuarters)
	updateSpinner := func() {
		tick := time.NewTicker(100 * time.Millisecond)
		for {
			select {
			case <-tick.C:
				spinner.Pulse()
				d.application.Draw()
			}
		}
	}
	go updateSpinner()

	descriptionLayout := tview.NewFlex().SetDirection(tview.FlexColumn).
		AddItem(spinner, 2, 0, false).
		AddItem(descriptionTextView, 0, 1, false)

	abortTextView := uiutil.CreateAttentionText("Press 'q' to abort")

	progress := tvxwidgets.NewPercentageModeGauge()
	progressTitle := fmt.Sprintf(" %s ... ", d.fileSelection.Name)
	progress.SetTitle(progressTitle)
	progress.SetBorder(true)

	value := 0
	progress.SetMaxValue(100)

	progressUpdate := func() {
		tick := time.NewTicker(100 * time.Millisecond)
		for {
			select {
			case <-tick.C:
				if value > progress.GetMaxValue() {
					value = 0
				} else {
					value = value + 1
				}
				progress.SetValue(value)
				d.application.Draw()
			}
		}
	}
	go progressUpdate()

	progressLayout := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(descriptionLayout, 0, 1, false).
		AddItem(progress, 3, 0, false).
		AddItem(abortTextView, 1, 0, false)
	progressLayout.SetBorderPadding(0, 0, 1, 1)

	dialog := createModal(dialogTitle, progressLayout, 60, 10)
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
