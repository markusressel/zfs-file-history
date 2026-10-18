package dialog

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync/atomic"
	"time"
	"zfs-file-history/internal/data"
	"zfs-file-history/internal/logging"
	"zfs-file-history/internal/ui/theme"
	uiutil "zfs-file-history/internal/ui/util"
	"zfs-file-history/internal/zfs"

	"github.com/gdamore/tcell/v2"
	"github.com/navidys/tvxwidgets"
	"github.com/rivo/tview"
)

const (
	RestoreFileProgress uiutil.Page = "RestoreFileProgressDialog"
)

// countRestoreEntries counts the entries of a recursive restore, replaceable in tests.
var countRestoreEntries = zfs.CountRestoreEntries

type RestoreFileProgressDialog struct {
	application   *tview.Application
	fileSelection *data.FileBrowserEntry
	actionChannel chan DialogActionId

	layout              *tview.Flex
	descriptionTextView *tview.TextView
	// sizeConstraints decide the size of the dialog; the description is replaced by an error, see handleError
	sizeConstraints     DialogSizeConstraints
	actionsHelpTextView *tview.TextView
	actionPages         *tview.Pages
	closeTable          *tview.Table

	progress *tvxwidgets.PercentageModeGauge

	// isRunning is true while the restore is running. Only accessed on the UI thread.
	isRunning bool
	// cancel stops the restore between entries (and while copying a file), see cancelRestore
	cancel context.CancelFunc
	// total is the number of entries to restore (0 while they are counted), restored the ones restored so far: written
	// by the background restore, read by the ticker that shows the progress
	total, restored atomic.Int64
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
	descriptionTextView := tview.NewTextView().SetText(text).SetWordWrap(true)
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

	abortTextView := uiutil.CreateAttentionTextView("Press 'q' or Esc to cancel")
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

	d.sizeConstraints = DialogSizeConstraints{
		Title:        dialogTitle,
		Description:  text,
		StaticHeight: 4, // 3 for progress bar, 1 for actionPages
	}
	dialog := createResizingModal(dialogTitle, progressLayout, func() DialogSizeConstraints { return d.sizeConstraints })
	dialog.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if d.isRunning {
			if event.Key() == tcell.KeyEscape || event.Rune() == 'q' {
				d.cancelRestore()
			}
			// the dialog stays until the restore stopped, so it tells what was restored
			return nil
		}
		if event.Key() == tcell.KeyEscape || event.Key() == tcell.KeyEnter {
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

// cancelRestore stops the restore. It stops between entries, or while copying a file (which then stays as it was),
// and the dialog tells what was restored. Must be called on the UI thread.
func (d *RestoreFileProgressDialog) cancelRestore() {
	d.cancel()
	d.actionsHelpTextView.SetText(uiutil.CreateAttentionText("Cancelling…"))
}

func (d *RestoreFileProgressDialog) runAction(recursive bool) {
	ctx, cancel := context.WithCancel(context.Background())
	d.cancel = cancel
	go func() {
		defer close(d.done)
		defer cancel()
		snapshotFile := d.fileSelection.SnapshotFiles[0]
		d.finish(d.restore(ctx, snapshotFile.Snapshot, snapshotFile.Path, snapshotFile.OriginalPath, recursive))
	}()

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
				d.showProgress()
			})
		}
	}
	go progressUpdate()
}

// restore restores the file (or deletes the real one, if it is absent in the snapshot) and counts the entries
// restored. Runs in the background.
func (d *RestoreFileProgressDialog) restore(ctx context.Context, snapshot *zfs.Snapshot, srcFilePath string, dstFilePath string, recursive bool) error {
	if srcFilePath == "" {
		// The file is absent in the snapshot.
		// Restoring it means deleting the working copy!
		d.total.Store(1)
		if err := os.RemoveAll(dstFilePath); err != nil {
			return err
		}
		d.restored.Store(1)
		return nil
	}
	if !recursive {
		d.total.Store(1)
		if err := snapshot.Restore(ctx, srcFilePath); err != nil {
			return err
		}
		d.restored.Store(1)
		return nil
	}
	total, err := countRestoreEntries(ctx, srcFilePath)
	if err != nil {
		return err
	}
	d.total.Store(int64(total))
	return snapshot.RestoreRecursive(ctx, srcFilePath, func(done int) { d.restored.Store(int64(done)) })
}

// showProgress shows how many entries are restored. Must be called on the UI thread.
func (d *RestoreFileProgressDialog) showProgress() {
	total, restored := d.total.Load(), d.restored.Load()
	if total == 0 {
		// still counting them
		return
	}
	d.progress.SetMaxValue(int(total))
	d.progress.SetValue(int(restored))
	if total > 1 {
		d.progress.SetTitle(theme.CreateTitleText(fmt.Sprintf("%d of %d", restored, total)))
	}
}

// finish shows how the restore ended: done, cancelled or failed. Called from the background restore.
func (d *RestoreFileProgressDialog) finish(err error) {
	switch {
	case errors.Is(err, context.Canceled):
		d.handleCancelled()
	case err != nil:
		d.handleError(err)
	default:
		d.handleDone()
	}
}

// handleCancelled shows what was restored before the restore was cancelled. Called from the background restore.
func (d *RestoreFileProgressDialog) handleCancelled() {
	d.application.QueueUpdateDraw(func() {
		d.isRunning = false
		d.showProgress()
		total, restored := d.total.Load(), d.restored.Load()
		text := "Cancelled: nothing was restored."
		if restored > 0 {
			text = fmt.Sprintf("Cancelled after %d of %d entries: those stay restored, the others are as they were.", restored, total)
		}
		d.descriptionTextView.SetText(text).SetTextColor(theme.Colors.Info.Warning)
		d.sizeConstraints.Description = text
		d.progress.SetTitle(theme.CreateTitleText("Cancelled"))
		d.progress.SetTitleColor(theme.Colors.Info.Warning)
		d.actionPages.ShowPage("finished")
		d.application.SetFocus(d.closeTable)
	})
}

// handleError shows the error. Called from the background restore.
func (d *RestoreFileProgressDialog) handleError(err error) {
	logging.Error("Error during restore: %s", err.Error())
	d.application.QueueUpdateDraw(func() {
		d.isRunning = false
		d.descriptionTextView.SetText(err.Error()).SetTextColor(theme.Colors.Info.Error)
		// the error is usually longer than the description, e.g. with the path of a file
		d.sizeConstraints.Description = err.Error()
		d.progress.SetTitle(theme.CreateTitleText("Failed!"))
		d.progress.SetTitleColor(theme.Colors.Info.Error)
		d.actionPages.ShowPage("finished")
		d.application.SetFocus(d.closeTable)
	})
}

// handleDone shows the successful completion. Called from the background restore.
func (d *RestoreFileProgressDialog) handleDone() {
	d.application.QueueUpdateDraw(func() {
		d.isRunning = false
		finishedValue := d.progress.GetMaxValue()
		d.progress.SetValue(finishedValue)
		d.progress.SetTitle(theme.CreateTitleText("Done!"))
		d.progress.SetTitleColor(theme.Colors.Info.Ok)
		d.actionPages.ShowPage("finished")
		d.application.SetFocus(d.closeTable)
	})
}
