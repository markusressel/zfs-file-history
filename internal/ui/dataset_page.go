package ui

import (
	"zfs-file-history/internal/ui/dataset_browser"
	"zfs-file-history/internal/ui/dataset_info"
	"zfs-file-history/internal/ui/shortcut_helper"
	"zfs-file-history/internal/ui/snapshot_browser"
	uiutil "zfs-file-history/internal/ui/util"
	"zfs-file-history/internal/zfs"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// DatasetPage is the dataset page: the datasets, next to the info and the snapshots of the selected one.
type DatasetPage struct {
	basePage
	datasetBrowser  *dataset_browser.DatasetBrowserComponent
	datasetInfo     *dataset_info.DatasetInfoComponent
	snapshotBrowser *snapshot_browser.SnapshotBrowserComponent
	layout          *tview.Flex
	// split separates the datasets and infoSplit, infoSplit the dataset info and the snapshots
	split     *uiutil.ResizableSplit
	infoSplit *uiutil.ResizableSplit

	wasInitialized bool
}

func NewDatasetPage(application *tview.Application, path string) *DatasetPage {

	datasetInfo := dataset_info.NewDatasetInfo(application)
	snapshotBrowser := snapshot_browser.NewSnapshotBrowser(application)
	// about the whole dataset
	snapshotBrowser.UseColumnLayout(snapshot_browser.DatasetsLayout)

	datasetBrowser := dataset_browser.NewDatasetBrowser(application)

	datasetPage := &DatasetPage{
		basePage:        basePage{application: application, name: Dataset},
		datasetBrowser:  datasetBrowser,
		datasetInfo:     datasetInfo,
		snapshotBrowser: snapshotBrowser,
	}
	datasetPage.focusableComponents = func() []FocusableUiComponent {
		return []FocusableUiComponent{datasetBrowser, datasetInfo, snapshotBrowser}
	}

	// Loading the dataset info and the snapshots of the selected dataset waits until the selection rests, so
	// holding an arrow key only moves the selection (like in the file browser).
	selectionDebouncer := uiutil.NewDebouncer(application, uiutil.SelectionDebounceDelay)
	var selectedDataset *zfs.DatasetListEntry
	var selectedPath string
	var pathChanged bool
	applySelection := func() {
		if selectedDataset != nil {
			// by name, so unmounted datasets can be shown as well
			datasetInfo.SetDatasetName(selectedDataset.Name, selectedDataset.MountPath)
		}
		snapshotBrowser.SetFileEntry(nil) // No file selected in dataset view
		if pathChanged {
			pathChanged = false
			snapshotBrowser.SetPath(selectedPath, false)
		}
	}

	datasetBrowser.Events.Subscribe(func(event dataset_browser.Event) {
		switch e := event.(type) {
		case dataset_browser.PathChangedEvent:
			selectedPath, pathChanged = e.NewPath, true
			selectionDebouncer.Call(applySelection)
		case dataset_browser.DatasetBrowserStatusEvent:
			datasetPage.showStatusMessage(e.Message)
		case dataset_browser.SelectedDatasetChangedEvent:
			selectedDataset = e.Dataset
			selectionDebouncer.Call(applySelection)
			if datasetBrowser.HasFocus() {
				datasetPage.updateShortcutMap(datasetBrowser)
			}
		}
	})

	snapshotBrowser.Events.Subscribe(func(event snapshot_browser.Event) {
		switch e := event.(type) {
		case snapshot_browser.StatusMessageEvent:
			datasetPage.showStatusMessage(e.Message)
		case snapshot_browser.SelectedSnapshotChanged:
			if snapshotBrowser.HasFocus() {
				datasetPage.updateShortcutMap(snapshotBrowser)
			}
		}
	})

	datasetPage.layout = datasetPage.createLayout()

	if zfs.IsDatasetsLoaded() {
		datasetPage.Init(path)
	}

	uiutil.SubscribeUI(zfs.DatasetsLoaded, application, func(_ struct{}) {
		if !datasetPage.wasInitialized {
			datasetPage.Init(path)
		} else {
			currentPath := datasetBrowser.GetPath()
			datasetPage.datasetInfo.Refresh()
			datasetPage.snapshotBrowser.SetPath(currentPath, true)
			datasetBrowser.Refresh(false)
		}
	})

	datasetPage.layout.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if datasetPage.handleFocusKeys(event) {
			return nil
		}
		if event.Key() == tcell.KeyF5 {
			// also reloads the dataset list, via the zfs.DatasetsLoaded subscription
			zfs.RefreshZfsData()
		}
		return event
	})

	return datasetPage
}

func (datasetPage *DatasetPage) createLayout() *tview.Flex {
	datasetPageLayout := tview.NewFlex().SetDirection(tview.FlexRow)

	datasetPage.header = NewApplicationHeader(datasetPage.application)
	datasetPageLayout.AddItem(datasetPage.header.layout, 1, 0, false)

	datasetPage.infoSplit = datasetPage.newSplit(datasetPage.datasetInfo.GetLayout(), datasetPage.snapshotBrowser.GetLayout(), 1, 2, true)
	datasetPage.split = datasetPage.newSplit(datasetPage.datasetBrowser.GetLayout(), datasetPage.infoSplit, 2, 1, false)
	datasetPageLayout.AddItem(datasetPage.split, 0, 1, true)
	captureSplitDrags(datasetPageLayout, datasetPage.split, datasetPage.infoSplit)

	// hidden with ? (see shortcut_helper.ToggleShortcuts)
	shortcutMap := shortcut_helper.NewShortcutMap(datasetPage.application).SetCollapsible()
	shortcutMap.SetOnHeightChanged(func(height int) {
		datasetPageLayout.ResizeItem(shortcutMap.GetLayout(), height, 0)
	})
	datasetPageLayout.AddItem(shortcutMap.GetLayout(), 1, 0, false)
	datasetPage.shortcutMap = shortcutMap

	return datasetPageLayout
}

func (datasetPage *DatasetPage) Init(path string) {
	datasetPage.wasInitialized = true
	datasetPage.datasetInfo.SetPath(path)
	datasetPage.snapshotBrowser.SetPath(path, false)
	datasetPage.datasetBrowser.SetPath(path, false)
	datasetPage.datasetBrowser.Refresh(false)
}
