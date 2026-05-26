package ui

import (
	"fmt"
	"zfs-file-history/internal/logging"
	"zfs-file-history/internal/ui/dataset_info"
	"zfs-file-history/internal/ui/file_browser"
	"zfs-file-history/internal/ui/shortcut_helper"
	"zfs-file-history/internal/ui/snapshot_browser"
	"zfs-file-history/internal/ui/status_message"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

type MainPage struct {
	application     *tview.Application
	header          *ApplicationHeaderComponent
	shortcutMap     *shortcut_helper.ShortcutMapComponent
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

	snapshotBrowser.Events.Subscribe(func(event snapshot_browser.Event) {
		switch event := event.(type) {
		case snapshot_browser.StatusMessageEvent:
			mainPage.showStatusMessage(event.Message)
		case snapshot_browser.SnapshotCreated:
			mainPage.showStatusMessage(status_message.NewSuccessStatusMessage(fmt.Sprintf("Snapshot '%s' created.", event.SnapshotName)))
		case snapshot_browser.SnapshotDestroyed:
			mainPage.showStatusMessage(status_message.NewSuccessStatusMessage(fmt.Sprintf("Snapshot '%s' destroyed.", event.SnapshotName)))
		}
	})

	fileBrowser.Events.Subscribe(func(event file_browser.Event) {
		switch e := event.(type) {
		case file_browser.PathChangedEvent:
			datasetInfo.SetPath(e.NewPath)
			snapshotBrowser.SetPath(e.NewPath, false)
		case file_browser.FileBrowserStatusEvent:
			mainPage.showStatusMessage(e.Message)
		case file_browser.SelectedTableEntryChangedEvent:
			snapshotBrowser.SetFileEntry(e.FileEntry)
			if fileBrowser.HasFocus() {
				mainPage.updateShortcutMap(fileBrowser)
			}
		}
	})

	snapshotBrowser.Events.Subscribe(func(event snapshot_browser.Event) {
		switch e := event.(type) {
		case snapshot_browser.SelectedSnapshotChanged:
			fileBrowser.SetSelectedSnapshot(e.Snapshot)
			if snapshotBrowser.HasFocus() {
				mainPage.updateShortcutMap(snapshotBrowser)
			}
		}
	})

	mainPage.layout = mainPage.createLayout()
	mainPage.layout.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		key := event.Key()
		switch key {
		case tcell.KeyTab, tcell.KeyBacktab:
			mainPage.ToggleFocus()
		case tcell.KeyF5:
			fileBrowser.Refresh()
			snapshotBrowser.Refresh(true)
			fileBrowser.Refresh()
		default:
		}
		return event
	})

	fileBrowser.Events.Subscribe(func(event file_browser.Event) {
		switch e := event.(type) {
		case file_browser.RequestFocusEvent:
			application.SetFocus(e.Layout)
		case file_browser.CreateSnapshotEvent:
			name := e.SnapshotName
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

	shortcutMap := shortcut_helper.NewShortcutMap(mainPage.application)
	mainPageLayout.AddItem(shortcutMap.GetLayout(), 1, 0, false)
	mainPage.shortcutMap = shortcutMap

	return mainPageLayout
}

func (mainPage *MainPage) Init(path string) {
	mainPage.datasetInfo.SetPath(path)
	mainPage.snapshotBrowser.SetPath(path, false)
	mainPage.fileBrowser.SetPath(path, false)
	mainPage.fileBrowser.SelectFirstEntryIfExists()
}

func (mainPage *MainPage) ToggleFocus() {
	var nextFocusedComponent FocusableUiComponent
	if mainPage.fileBrowser.HasFocus() {
		nextFocusedComponent = mainPage.datasetInfo
	} else if mainPage.snapshotBrowser.HasFocus() {
		nextFocusedComponent = mainPage.fileBrowser
	} else if mainPage.datasetInfo.HasFocus() {
		nextFocusedComponent = mainPage.snapshotBrowser
	} else {
		nextFocusedComponent = mainPage.fileBrowser
		logging.Warning("Unexpected focus state")
	}

	nextFocusedComponent.Focus()
	mainPage.updateShortcutMap(nextFocusedComponent)
}

func (mainPage *MainPage) showStatusMessage(status *status_message.StatusMessage) {
	mainPage.header.SetStatus(status)
}

func (mainPage *MainPage) setShortcutMap(shortcutEntries []shortcut_helper.ShortcutEntry) {
	mainPage.shortcutMap.SetEntries(shortcutEntries)
}

func (mainPage *MainPage) clearShortcutMap() {
	mainPage.shortcutMap.Clear()
}

func (mainPage *MainPage) updateShortcutMap(component FocusableUiComponent) {
	if c, ok := component.(shortcut_helper.ShortcutMapProvider); ok {
		shortcutMap := c.GetShortcutMap()

		globalShortcutMapEntries := []shortcut_helper.ShortcutEntry{}

		shortcutMap = append(shortcutMap, globalShortcutMapEntries...)
		mainPage.setShortcutMap(shortcutMap)
	} else {
		mainPage.clearShortcutMap()
	}
}
