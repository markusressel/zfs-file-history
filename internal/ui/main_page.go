package ui

import (
	"fmt"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"time"
	"zfs-file-history/internal/data"
	"zfs-file-history/internal/logging"
	"zfs-file-history/internal/ui/dataset_info"
	"zfs-file-history/internal/ui/file_browser"
	"zfs-file-history/internal/ui/snapshot_browser"
	"zfs-file-history/internal/ui/status_message"
	uiutil "zfs-file-history/internal/ui/util"
	"zfs-file-history/internal/zfs"
)

type MainPage struct {
	application     *tview.Application
	header          *ApplicationHeaderComponent
	fileBrowser     *file_browser.FileBrowserComponent
	datasetInfo     *dataset_info.DatasetInfoComponent
	snapshotBrowser *snapshot_browser.SnapshotBrowserComponent
	layout          *tview.Flex
}

func NewMainPage(application *tview.Application) *MainPage {

	datasetInfo := dataset_info.NewDatasetInfo(application)
	snapshotBrowser := snapshot_browser.NewSnapshotBrowser(application)

	fileBrowser := file_browser.NewFileBrowser(application)

	mainPage := &MainPage{
		application:     application,
		fileBrowser:     fileBrowser,
		datasetInfo:     datasetInfo,
		snapshotBrowser: snapshotBrowser,
	}

	snapshotBrowser.SetEventCallback(func(event snapshot_browser.SnapshotBrowserEvent) {
		switch event := event.(type) {
		case uiutil.StatusMessageEvent:
			mainPage.showStatusMessage(event.Message)
		case snapshot_browser.SnapshotCreated:
			mainPage.showStatusMessage(status_message.NewSuccessStatusMessage(fmt.Sprintf("Snapshot '%s' created.", event.SnapshotName)))
		case snapshot_browser.SnapshotDestroyed:
			mainPage.showStatusMessage(status_message.NewSuccessStatusMessage(fmt.Sprintf("Snapshot '%s' destroyed.", event.SnapshotName)))
		}
	})

	fileBrowser.SetStatusCallback(func(message *status_message.StatusMessage) {
		mainPage.showStatusMessage(message)
	})

	fileBrowser.SetPathChangedCallback(func(path string) {
		datasetInfo.SetPath(path)
		snapshotBrowser.SetPath(path, false)
	})
	fileBrowser.SetSelectedFileEntryChangedCallback(func(fileEntry *data.FileBrowserEntry) {
		snapshotBrowser.SetFileEntry(fileEntry)
	})

	snapshotBrowser.SetSelectedSnapshotChangedCallback(func(snapshot *data.SnapshotBrowserEntry) {
		fileBrowser.SetSelectedSnapshot(snapshot)
	})

	mainPage.layout = mainPage.createLayout()
	mainPage.layout.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		key := event.Key()
		if key == tcell.KeyTab || key == tcell.KeyBacktab {
			mainPage.ToggleFocus()
		} else if key == tcell.KeyCtrlR {
			fileBrowser.Refresh()
			snapshotBrowser.Refresh(true)
			fileBrowser.Refresh()
		}
		return event
	})

	fileBrowser.SetEventCallback(func(event file_browser.FileBrowserEvent) {
		switch event {
		case file_browser.CreateSnapshotEvent:
			name := fmt.Sprintf("zfh-%s", time.Now().Format(zfs.SnapshotTimeFormat))
			err := datasetInfo.CreateSnapshot(name)
			if err != nil {
				logging.Error("Failed to create snapshot: %s", err)
				mainPage.showStatusMessage(status_message.NewErrorStatusMessage(fmt.Sprintf("Failed to create snapshot: %s", err)))
			} else {
				snapshotBrowser.Refresh(true)
				snapshotBrowser.SelectLatest()
				mainPage.showStatusMessage(status_message.NewSuccessStatusMessage(fmt.Sprintf("Snapshot '%s' created.", name)))
			}
		}
	})

	return mainPage
}

func (mainPage *MainPage) createLayout() *tview.Flex {
	mainPageLayout := tview.NewFlex().SetDirection(tview.FlexRow)

	header := NewApplicationHeader(mainPage.application)
	mainPageLayout.AddItem(header.layout, 1, 0, false)

	windowLayout := tview.NewFlex().SetDirection(tview.FlexColumn)
	//dialog := createFileBrowserActionDialog()

	windowLayout.AddItem(mainPage.fileBrowser.GetLayout(), 0, 2, true)

	infoLayout := tview.NewFlex().SetDirection(tview.FlexRow)
	infoLayout.AddItem(mainPage.datasetInfo.GetLayout(), 0, 1, false)
	infoLayout.AddItem(mainPage.snapshotBrowser.GetLayout(), 0, 2, false)
	windowLayout.AddItem(infoLayout, 0, 1, false)

	mainPageLayout.AddItem(windowLayout, 0, 1, true)

	mainPage.header = header

	return mainPageLayout
}

func (mainPage *MainPage) Init(path string) {
	mainPage.datasetInfo.SetPath(path)
	mainPage.snapshotBrowser.SetPath(path, false)
	mainPage.fileBrowser.SetPath(path, false)
}

func (mainPage *MainPage) ToggleFocus() {
	if mainPage.fileBrowser.HasFocus() {
		mainPage.datasetInfo.Focus()
	} else if mainPage.snapshotBrowser.HasFocus() {
		mainPage.fileBrowser.Focus()
	} else if mainPage.datasetInfo.HasFocus() {
		mainPage.snapshotBrowser.Focus()
	} else {
		logging.Warning("Unexpected focus state")
	}
}

func (mainPage *MainPage) showStatusMessage(status *status_message.StatusMessage) {
	mainPage.header.SetStatus(status)
}

func (mainPage *MainPage) clearStatus() {
	mainPage.header.ClearStatus()
}
