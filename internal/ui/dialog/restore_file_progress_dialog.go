package dialog

import (
	"fmt"
	"github.com/gdamore/tcell/v2"
	"github.com/navidys/tvxwidgets"
	"github.com/rivo/tview"
	"math"
	"time"
	"zfs-file-history/internal/data"
	"zfs-file-history/internal/logging"
	"zfs-file-history/internal/ui/theme"
	uiutil "zfs-file-history/internal/ui/util"
)

const (
	RestoreFileProgress uiutil.Page = "RestoreFileProgressDialog"
)

type RestoreFileProgressDialog struct {
	application   *tview.Application
	fileSelection *data.FileBrowserEntry
	actionChannel chan DialogAction

	layout              *tview.Flex
	descriptionTextView *tview.TextView
	abortTextView       *tview.TextView

	progress      *tvxwidgets.PercentageModeGauge
	progressValue int

	isRunning bool
}

func NewRestoreFileProgressDialog(application *tview.Application, fileSelection *data.FileBrowserEntry, recursive bool) *RestoreFileProgressDialog {
	dialog := &RestoreFileProgressDialog{
		application:   application,
		fileSelection: fileSelection,
		actionChannel: make(chan DialogAction, 10),
	}

	dialog.createLayout()
	dialog.runAction(recursive)

	return dialog
}

func (d *RestoreFileProgressDialog) createLayout() {
	dialogTitle := "Restore"

	fileToRestore := d.fileSelection.SnapshotFiles[0]

	text := fmt.Sprintf("Restoring '%s' from snapshot '%s'", d.fileSelection.Name, fileToRestore.Snapshot.Name)
	descriptionTextView := tview.NewTextView().SetText(text)
	d.descriptionTextView = descriptionTextView

	spinner := tvxwidgets.NewSpinner().SetStyle(tvxwidgets.SpinnerCircleQuarters)
	updateSpinner := func() {
		tick := time.NewTicker(100 * time.Millisecond)
		for {
			<-tick.C
			if !d.isRunning {
				break
			}
			spinner.Pulse()
			d.application.Draw()
		}
	}
	go updateSpinner()

	descriptionLayout := tview.NewFlex().SetDirection(tview.FlexColumn).
		AddItem(spinner, 2, 0, false).
		AddItem(descriptionTextView, 0, 1, false)

	abortTextView := uiutil.CreateAttentionTextView("Press 'q' to abort")
	d.abortTextView = abortTextView

	progress := tvxwidgets.NewPercentageModeGauge()
	progressTitle := theme.CreateTitleText("Progress")
	progress.SetTitle(progressTitle)
	progress.SetBorder(true)
	d.progress = progress

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

func (d *RestoreFileProgressDialog) GetActionChannel() <-chan DialogAction {
	return d.actionChannel
}

func (d *RestoreFileProgressDialog) Close() {
	go func() {
		d.actionChannel <- ActionClose
	}()
}

func (d *RestoreFileProgressDialog) runAction(recursive bool) {
	go func() {
		d.isRunning = true
		snapshot := d.fileSelection.SnapshotFiles[0].Snapshot

		snapshotFile := d.fileSelection.SnapshotFiles[0]
		srcFilePath := snapshotFile.Path

		if recursive {
			// TODO: this loops two times currently to ensure folder modtime properties are correct.
			//  See implementation for what we need to do to fix this
			for i := 0; i < 2; i++ {
				err := snapshot.RestoreRecursive(srcFilePath)
				d.handleError(err)
				d.application.Draw()
				if err != nil {
					logging.Error(err.Error())
					return
				}
			}
		} else {
			err := snapshot.Restore(srcFilePath)
			d.handleError(err)
			d.application.Draw()
			if err != nil {
				logging.Error(err.Error())
				return
			}
		}

		d.handleDone()
		d.application.Draw()
	}()

	d.progressValue = 0
	d.progress.SetMaxValue(100)

	progressUpdate := func() {
		tick := time.NewTicker(100 * time.Millisecond)
		for {
			<-tick.C
			if !d.isRunning {
				break
			}
			d.progressValue = int(math.Min(float64(d.progress.GetMaxValue()), float64(d.progressValue)))
			d.progress.SetValue(d.progressValue)
			d.application.Draw()
		}
	}
	go progressUpdate()
}

func (d *RestoreFileProgressDialog) handleError(err error) {
	if err != nil {
		d.isRunning = false
		d.descriptionTextView.SetText(err.Error()).SetTextColor(tcell.ColorRed)
		d.progress.SetTitle(theme.CreateTitleText("Failed!"))
		d.progress.SetTitleColor(tcell.ColorRed)
	}
}

func (d *RestoreFileProgressDialog) handleDone() {
	d.isRunning = false
	finishedValue := d.progress.GetMaxValue()
	d.progress.SetValue(finishedValue)
	d.progress.SetTitle(theme.CreateTitleText("Done!"))
	d.progress.SetTitleColor(tcell.ColorGreen)
	d.abortTextView.SetText(uiutil.CreateAttentionText("Press 'q' to close"))
}
