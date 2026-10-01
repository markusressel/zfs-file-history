package dialog

import (
	"fmt"
	"math"
	"os"
	"time"
	"zfs-file-history/internal/data"
	"zfs-file-history/internal/logging"
	"zfs-file-history/internal/ui/theme"
	uiutil "zfs-file-history/internal/ui/util"

	"github.com/gdamore/tcell/v2"
	"github.com/navidys/tvxwidgets"
	"github.com/rivo/tview"
)

const (
	RestoreFileProgress uiutil.Page = "RestoreFileProgressDialog"
)

type RestoreFileProgressDialog struct {
	application   *tview.Application
	fileSelection *data.FileBrowserEntry
	actionChannel chan DialogActionId

	layout              *tview.Flex
	descriptionTextView *tview.TextView
	actionsHelpTextView *tview.TextView
	actionPages         *tview.Pages
	closeTable          *tview.Table

	progress      *tvxwidgets.PercentageModeGauge
	progressValue int

	// isRunning is true while the restore is running. Only accessed on the UI thread.
	isRunning bool
	// done is closed when the background restore has finished (successfully or not),
	// to stop the background tickers without sharing isRunning with them.
	done chan struct{}
}

// NewRestoreFileProgressDialog creates the dialog and immediately starts the restore in the background.
// Must be called on the UI thread.
func NewRestoreFileProgressDialog(application *tview.Application, fileSelection *data.FileBrowserEntry, recursive bool) *RestoreFileProgressDialog {
	dialog := &RestoreFileProgressDialog{
		application:   application,
		fileSelection: fileSelection,
		actionChannel: make(chan DialogActionId),
		isRunning:     true,
		done:          make(chan struct{}),
	}

	dialog.createLayout()
	dialog.runAction(recursive)

	return dialog
}

func (d *RestoreFileProgressDialog) createLayout() {
	dialogTitle := " ♻️ Restore "

	fileToRestore := d.fileSelection.SnapshotFiles[0]

	text := fmt.Sprintf("Restoring '%s' from snapshot '%s'", d.fileSelection.Name, fileToRestore.Snapshot.Name)
	descriptionTextView := tview.NewTextView().SetText(text)
	d.descriptionTextView = descriptionTextView

	spinner := tvxwidgets.NewSpinner().SetStyle(tvxwidgets.SpinnerCircleQuarters)
	updateSpinner := func() {
		tick := time.NewTicker(100 * time.Millisecond)
		defer tick.Stop()
		for {
			select {
			case <-d.done:
				return
			case <-tick.C:
			}
			d.application.QueueUpdateDraw(func() {
				if !d.isRunning {
					return
				}
				spinner.Pulse()
			})
		}
	}
	go updateSpinner()

	descriptionLayout := tview.NewFlex().SetDirection(tview.FlexColumn).
		AddItem(spinner, 2, 0, false).
		AddItem(descriptionTextView, 0, 1, false)

	abortTextView := uiutil.CreateAttentionTextView("Press 'q' to abort")
	d.actionsHelpTextView = abortTextView

	dialogOptions := []*DialogOption{
		{
			Id:   DialogCloseActionId,
			Name: "Close",
		},
	}
	closeTable := createOptionTable(d.application, dialogOptions, func(option *DialogOption) {
		d.Close()
	})
	d.closeTable = closeTable

	actionPages := tview.NewPages().
		AddPage("running", abortTextView, true, true).
		AddPage("finished", closeTable, true, false)
	d.actionPages = actionPages

	progress := tvxwidgets.NewPercentageModeGauge()
	progressTitle := theme.CreateTitleText("Progress")
	progress.SetTitle(progressTitle)
	progress.SetBorder(true)
	d.progress = progress

	progressLayout := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(descriptionLayout, 0, 1, false).
		AddItem(progress, 3, 0, false).
		AddItem(actionPages, 1, 0, false)
	progressLayout.SetBorderPadding(0, 0, 1, 1)

	dialog := createModal(dialogTitle, progressLayout, DialogSizeConstraints{
		Title:        dialogTitle,
		Description:  text,
		StaticHeight: 4, // 3 for progress bar, 1 for actionPages
	})
	dialog.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyEscape {
			d.Close()
			return nil
		}
		if !d.isRunning && event.Key() == tcell.KeyEnter {
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

func (d *RestoreFileProgressDialog) GetActionChannel() <-chan DialogActionId {
	return d.actionChannel
}

func (d *RestoreFileProgressDialog) Close() {
	go func() {
		d.actionChannel <- DialogCloseActionId
	}()
}

func (d *RestoreFileProgressDialog) runAction(recursive bool) {
	go func() {
		defer close(d.done)
		snapshot := d.fileSelection.SnapshotFiles[0].Snapshot

		snapshotFile := d.fileSelection.SnapshotFiles[0]
		srcFilePath := snapshotFile.Path
		dstFilePath := snapshotFile.OriginalPath

		if srcFilePath == "" {
			// The file is absent in the snapshot.
			// Restoring it means deleting the working copy!
			err := os.RemoveAll(dstFilePath)
			d.handleError(err)
			if err != nil {
				return
			}
		} else if recursive {
			// TODO: this loops two times currently to ensure folder modtime properties are correct.
			//  See implementation for what we need to do to fix this
			for i := 0; i < 2; i++ {
				err := snapshot.RestoreRecursive(srcFilePath)
				d.handleError(err)
				if err != nil {
					return
				}
			}
		} else {
			err := snapshot.Restore(srcFilePath)
			d.handleError(err)
			if err != nil {
				return
			}
		}

		d.handleDone()
	}()

	d.progressValue = 0
	d.progress.SetMaxValue(100)

	progressUpdate := func() {
		tick := time.NewTicker(100 * time.Millisecond)
		defer tick.Stop()
		for {
			select {
			case <-d.done:
				return
			case <-tick.C:
			}
			d.application.QueueUpdateDraw(func() {
				if !d.isRunning {
					return
				}
				d.progressValue = int(math.Min(float64(d.progress.GetMaxValue()), float64(d.progressValue)))
				d.progress.SetValue(d.progressValue)
			})
		}
	}
	go progressUpdate()
}

// handleError shows the error, if any. Called from the background restore.
func (d *RestoreFileProgressDialog) handleError(err error) {
	if err != nil {
		logging.Error("Error during restore: %s", err.Error())
		d.application.QueueUpdateDraw(func() {
			d.isRunning = false
			d.descriptionTextView.SetText(err.Error()).SetTextColor(tcell.ColorRed)
			d.progress.SetTitle(theme.CreateTitleText("Failed!"))
			d.progress.SetTitleColor(tcell.ColorRed)
			d.actionPages.ShowPage("finished")
			d.application.SetFocus(d.closeTable)
		})
	}
}

// handleDone shows the successful completion. Called from the background restore.
func (d *RestoreFileProgressDialog) handleDone() {
	d.application.QueueUpdateDraw(func() {
		d.isRunning = false
		finishedValue := d.progress.GetMaxValue()
		d.progress.SetValue(finishedValue)
		d.progress.SetTitle(theme.CreateTitleText("Done!"))
		d.progress.SetTitleColor(tcell.ColorGreen)
		d.actionPages.ShowPage("finished")
		d.application.SetFocus(d.closeTable)
	})
}
