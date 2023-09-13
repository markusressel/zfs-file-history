package dialog

import (
	"fmt"
	"github.com/gdamore/tcell/v2"
	"github.com/navidys/tvxwidgets"
	"github.com/rivo/tview"
	"io"
	"os"
	"syscall"
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
	progress            *tvxwidgets.PercentageModeGauge

	running chan bool
}

func NewRestoreFileProgressDialog(application *tview.Application, fileSelection *data.FileBrowserEntry) *RestoreFileProgressDialog {
	dialog := &RestoreFileProgressDialog{
		application:   application,
		fileSelection: fileSelection,
		actionChannel: make(chan DialogAction),
		running:       make(chan bool),
	}

	dialog.createLayout()
	dialog.runAction()

	return dialog
}

func (d *RestoreFileProgressDialog) createLayout() {
	dialogTitle := " Restoring... "

	fileToRestore := d.fileSelection.SnapshotFiles[0]

	text := fmt.Sprintf("Restoring '%s' from snapshot '%s'", d.fileSelection.Name, fileToRestore.Snapshot.Name)
	descriptionTextView := tview.NewTextView().SetText(text)
	d.descriptionTextView = descriptionTextView

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

func (d *RestoreFileProgressDialog) GetActionChannel() chan DialogAction {
	return d.actionChannel
}

func (d *RestoreFileProgressDialog) Close() {
	go func() {
		d.actionChannel <- ActionClose
	}()
}

func (d *RestoreFileProgressDialog) runAction() {
	go func() {
		snapshotFile := d.fileSelection.SnapshotFiles[0]
		srcFilePath := snapshotFile.Path
		dstFilePath := snapshotFile.OriginalPath

		if snapshotFile.Stat.IsDir() {
			err := d.copyDir(snapshotFile, srcFilePath, dstFilePath)
			d.handleError(err)
			if err != nil {
				logging.Error(err.Error())
				return
			}
		} else {
			err := d.copyFile(snapshotFile, srcFilePath, dstFilePath)
			d.handleError(err)
			if err != nil {
				logging.Error(err.Error())
				return
			}
		}

		d.handleDone()
	}()
	value := 0
	d.progress.SetMaxValue(100)

	progressUpdate := func() {
		tick := time.NewTicker(100 * time.Millisecond)
		for {
			select {
			case <-tick.C:
				if value > d.progress.GetMaxValue() {
					d.handleDone()
				} else {
					value = value + 1
				}
				d.progress.SetValue(value)
				d.application.Draw()
			case isRunning := <-d.running:
				if !isRunning {
					break
				}
			}
		}
	}
	go progressUpdate()
}

func (d *RestoreFileProgressDialog) handleError(err error) {
	if err != nil {
		d.descriptionTextView.SetText(err.Error()).SetTextColor(tcell.ColorRed)
		d.progress.SetTitle(theme.CreateTitleText("Failed!"))
		d.progress.SetTitleColor(tcell.ColorRed)
		go func() {
			d.running <- false
		}()
	}
}

func (d *RestoreFileProgressDialog) handleDone() {
	go func() {
		d.running <- false
	}()
	d.progress.SetValue(100)
	d.progress.SetTitle(theme.CreateTitleText("Done!"))
	d.progress.SetTitleColor(tcell.ColorGreen)
	d.application.Draw()
}

func (d *RestoreFileProgressDialog) copyDir(snapshotFile *data.SnapshotFile, srcPath string, dstPath string) error {
	err := os.MkdirAll(dstPath, snapshotFile.Stat.Mode())
	if err != nil {
		return err
	}

	return nil
}

func (d *RestoreFileProgressDialog) copyFile(snapshotFile *data.SnapshotFile, srcPath string, dstPath string) error {
	srcFile, err := os.Open(srcPath)
	d.handleError(err)
	defer srcFile.Close()

	destFile, err := os.Create(dstPath) // creates if file doesn't exist
	if err != nil {
		return err
	}
	defer destFile.Close()

	_, err = io.Copy(destFile, srcFile) // check first var for number of bytes copied
	if err != nil {
		return err
	}

	err = destFile.Sync()
	if err != nil {
		return err
	}

	err = os.Chmod(dstPath, snapshotFile.Stat.Mode())
	if err != nil {
		return err
	}

	if stat, ok := snapshotFile.Stat.Sys().(*syscall.Stat_t); ok {
		err = os.Chown(dstPath, int(stat.Uid), int(stat.Gid))
		if err != nil {
			return err
		}
	}

	// TODO: sync timestamps

	return err
}
