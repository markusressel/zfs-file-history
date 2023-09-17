package ui

import (
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"time"
	"zfs-file-history/internal/data"
	"zfs-file-history/internal/logging"
	"zfs-file-history/internal/ui/dataset_info"
	"zfs-file-history/internal/ui/file_browser"
	snapshotBrowser2 "zfs-file-history/internal/ui/snapshot_browser"
	"zfs-file-history/internal/ui/status_message"
)

type MainPage struct {
	application     *tview.Application
	header          *ApplicationHeaderComponent
	fileBrowser     *file_browser.FileBrowserComponent
	datasetInfo     *dataset_info.DatasetInfoComponent
	snapshotBrowser *snapshotBrowser2.SnapshotBrowserComponent
	layout          *tview.Flex
	statusChannel   chan *status_message.StatusMessage
}

func NewMainPage(application *tview.Application, path string) *MainPage {
	statusChannel := make(chan *status_message.StatusMessage)

	fileBrowser := file_browser.NewFileBrowser(application, statusChannel, path)

	datasetInfo := dataset_info.NewDatasetInfo(application)
	datasetInfo.SetPath(path)

	snapshotBrowser := snapshotBrowser2.NewSnapshotBrowser(application, path)
	snapshotBrowser.SetPath(path)
	snapshotBrowser.SetFileEntry(fileBrowser.GetSelection())

	mainPage := &MainPage{
		application:     application,
		fileBrowser:     fileBrowser,
		datasetInfo:     datasetInfo,
		snapshotBrowser: snapshotBrowser,
		statusChannel:   statusChannel,
	}

	fileBrowser.SetPathChangedCallback(func(path string) {
		snapshotBrowser.SetPath(path)
		datasetInfo.SetPath(path)
	})
	fileBrowser.SetSelectedFileEntryChangedCallback(func(fileEntry *data.FileBrowserEntry) {
		snapshotBrowser.SetFileEntry(fileEntry)
	})

	snapshotBrowser.SetSelectedSnapshotChangedCallback(func(snapshot *snapshotBrowser2.SnapshotBrowserEntry) {
		fileBrowser.SetSelectedSnapshot(snapshot)
	})
	fileBrowser.SetSelectedSnapshot(snapshotBrowser.GetSelection())

	mainPage.layout = mainPage.createLayout()
	mainPage.layout.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		key := event.Key()
		if key == tcell.KeyTab || key == tcell.KeyBacktab {
			mainPage.ToggleFocus()
		} else if key == tcell.KeyCtrlR {
			fileBrowser.Refresh()
			snapshotBrowser.Refresh()
			fileBrowser.Refresh()
		}
		return event
	})

	// listen for selection changes within the file browser
	go func() {
		for {
			select {
			case statusMessage := <-statusChannel:
				mainPage.showStatusMessage(statusMessage)
			}
		}
	}()

	mainPage.SendStatusMessage("Ready")

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

func (mainPage *MainPage) SendStatusMessage(s string) {
	go func() {
		mainPage.statusChannel <- status_message.NewInfoStatusMessage(s).SetDuration(3 * time.Second)
	}()
}
