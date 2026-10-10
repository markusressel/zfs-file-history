package ui

import (
	"fmt"
	"zfs-file-history/internal/data"
	"zfs-file-history/internal/state"
	"zfs-file-history/internal/ui/dialog"
	"zfs-file-history/internal/ui/file_browser"
	"zfs-file-history/internal/ui/path_overview"
	"zfs-file-history/internal/ui/shortcut_helper"
	"zfs-file-history/internal/ui/snapshot_browser"
	"zfs-file-history/internal/ui/status_message"
	uiutil "zfs-file-history/internal/ui/util"
	"zfs-file-history/internal/zfs"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// MainPage is the files page: the file browser with the path overview below it, next to the snapshots of the path.
type MainPage struct {
	basePage
	fileBrowser     *file_browser.FileBrowserComponent
	snapshotBrowser *snapshot_browser.SnapshotBrowserComponent
	// pathOverview is below the file browser, in leftLayout
	pathOverview *path_overview.PathOverviewComponent
	layout       *tview.Flex
	// split separates the file browser (with the overview) and the snapshots
	split      *uiutil.ResizableSplit
	leftLayout *tview.Flex

	wasInitialized bool
}

func NewMainPage(application *tview.Application, path string) *MainPage {
	snapshotBrowser := snapshot_browser.NewSnapshotBrowser(application)

	fileBrowser := file_browser.NewFileBrowser(application)
	pathOverview := path_overview.NewPathOverview(fileBrowser.DiffCounts)
	// about the path that is shown; the overview shows how the folder differs from now
	snapshotBrowser.UseColumnLayout(snapshot_browser.FilesLayout)
	snapshotBrowser.RequireFolderChanges()

	mainPage := &MainPage{
		basePage:        basePage{application: application, name: Main},
		fileBrowser:     fileBrowser,
		snapshotBrowser: snapshotBrowser,
		pathOverview:    pathOverview,
	}
	mainPage.focusableComponents = func() []FocusableUiComponent {
		return []FocusableUiComponent{fileBrowser, snapshotBrowser}
	}
	mainPage.pageShortcuts = func() []shortcut_helper.ShortcutEntry {
		return []shortcut_helper.ShortcutEntry{mainPage.overviewShortcut()}
	}

	fileBrowser.Events.Subscribe(func(event file_browser.Event) {
		switch e := event.(type) {
		case file_browser.EntriesLoadedEvent:
			// so the snapshot browser need not read the folder again to compare it with its snapshots
			snapshotBrowser.SetWorkingCopy(fileBrowser.WorkingCopyListing())
		case file_browser.PathChangedEvent:
			pathOverview.SetFolder(e.NewPath)
			snapshotBrowser.SetPath(e.NewPath, false)
		case file_browser.FileBrowserStatusEvent:
			mainPage.showStatusMessage(e.Message)
		case file_browser.SelectedTableEntryChangedEvent:
			pathOverview.SetEntry(e.FileEntry)
			snapshotBrowser.SetFileEntry(e.FileEntry)
			if fileBrowser.HasFocus() {
				mainPage.updateShortcutMap(fileBrowser)
			}
		case file_browser.RequestFileHistoryEvent:
			mainPage.showHistory(e.FileEntry, nil)
		case file_browser.RequestFocusEvent:
			application.SetFocus(e.Layout)
		case file_browser.SnapshotCreatedEvent:
			// emitted on the UI thread, after the snapshot was created in the background
			snapshotBrowser.SelectLatestOnNextLoad()
			snapshotBrowser.Refresh(true)
			mainPage.showStatusMessage(status_message.NewSuccessStatusMessage(fmt.Sprintf("Snapshot '%s' created.", e.SnapshotName)))
		}
	})

	// the snapshot browser opens the history of what h opens in the file browser, at the selected snapshot
	snapshotBrowser.SetHistoryTarget(fileBrowser.HistoryEntry)
	snapshotBrowser.Events.Subscribe(func(event snapshot_browser.Event) {
		switch e := event.(type) {
		case snapshot_browser.StatusMessageEvent:
			mainPage.showStatusMessage(e.Message)
		case snapshot_browser.RequestHistoryEvent:
			mainPage.showHistory(e.Entry, e.Snapshot.Snapshot)
		case snapshot_browser.FolderChangesLoaded:
			pathOverview.SetFolderChanges(e.FolderPath, e.BySnapshot)
		case snapshot_browser.PathVersionsLoaded:
			pathOverview.SetVersions(e.DatasetName, e.DatasetPath, e.FolderPath, e.Folder, e.Entry, e.EntryVersions)
		case snapshot_browser.SelectedSnapshotChanged:
			fileBrowser.SetSelectedSnapshot(e.Snapshot)
			if e.Snapshot != nil {
				pathOverview.SetSelectedSnapshot(e.Snapshot.Snapshot)
			} else {
				pathOverview.SetSelectedSnapshot(nil)
			}
			if snapshotBrowser.HasFocus() {
				mainPage.updateShortcutMap(snapshotBrowser)
			}
		}
	})

	mainPage.layout = mainPage.createLayout()

	if zfs.IsDatasetsLoaded() {
		mainPage.Init(path)
	}

	uiutil.SubscribeUI(zfs.DatasetsLoaded, application, func(_ struct{}) {
		if !mainPage.wasInitialized {
			mainPage.Init(path)
		} else {
			currentPath := fileBrowser.GetPath()
			mainPage.snapshotBrowser.SetPath(currentPath, true)
		}
	})

	mainPage.layout.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if mainPage.handleFocusKeys(event) {
			return nil
		}
		if event.Key() == tcell.KeyRune && event.Rune() == 'o' && !uiutil.IsTextInputActive(application.GetFocus()) {
			mainPage.toggleOverview()
			return nil
		}
		if event.Key() == tcell.KeyF5 {
			zfs.RefreshZfsData()
			fileBrowser.Refresh(false)
		}
		return event
	})

	return mainPage
}

func (mainPage *MainPage) createLayout() *tview.Flex {
	mainPageLayout := tview.NewFlex().SetDirection(tview.FlexRow)

	mainPage.header = NewApplicationHeader(mainPage.application)
	mainPageLayout.AddItem(mainPage.header.layout, 1, 0, false)

	// the overview below the file browser describes its folder and selected entry
	mainPage.leftLayout = tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(mainPage.fileBrowser.GetLayout(), 0, 1, true).
		AddItem(mainPage.pathOverview.GetLayout(), path_overview.Height, 0, false)
	mainPage.applyOverviewVisibility()
	// the dataset info is shown on the dataset page only, the snapshots use the whole height here
	mainPage.split = mainPage.newSplit(mainPage.leftLayout, mainPage.snapshotBrowser.GetLayout(), 2, 1, false)
	mainPageLayout.AddItem(mainPage.split, 0, 1, true)
	captureSplitDrags(mainPageLayout, mainPage.split)

	// hidden with ? (see shortcut_helper.ToggleShortcuts)
	shortcutMap := shortcut_helper.NewShortcutMap(mainPage.application).SetCollapsible()
	shortcutMap.SetOnHeightChanged(func(height int) {
		mainPageLayout.ResizeItem(shortcutMap.GetLayout(), height, 0)
	})
	mainPageLayout.AddItem(shortcutMap.GetLayout(), 1, 0, false)
	mainPage.shortcutMap = shortcutMap
	mainPage.reserveShortcutMapHeight()

	return mainPageLayout
}

func (mainPage *MainPage) Init(path string) {
	mainPage.wasInitialized = true
	mainPage.pathOverview.SetFolder(path)
	mainPage.snapshotBrowser.SetPath(path, false)
	mainPage.fileBrowser.SetPath(path, false)
	mainPage.fileBrowser.SelectFirstEntryIfExists()
}

// showHistory opens the file or folder history of entry. If snapshot is set, the version that was current in it is
// selected, otherwise the newest one.
func (mainPage *MainPage) showHistory(entry *data.FileBrowserEntry, snapshot *zfs.Snapshot) {
	var overlay dialog.Dialog
	if entry.Type == data.Directory {
		folderHistory := dialog.NewFolderHistoryOverlay(mainPage.application, entry.GetRealPath(), mainPage.snapshotBrowser.GetAllEntries())
		if snapshot != nil {
			folderHistory.SelectSnapshot(snapshot)
		}
		overlay = folderHistory
	} else {
		fileHistory := dialog.NewFileHistoryOverlay(mainPage.application, entry, mainPage.snapshotBrowser.GetAllEntries())
		if snapshot != nil {
			fileHistory.SelectSnapshot(snapshot)
		}
		overlay = fileHistory
	}
	dialog.ShowDialogOnPages(mainPage.application, mainPage.pages, overlay, func() {
		mainPage.fileBrowser.Refresh(false)
	})
}

// toggleHideOverview is the key of the setting in state.Current.
const toggleHideOverview = "filesPage.hideOverview"

// isOverviewHidden returns whether the path overview below the file browser is hidden, to make room.
func (mainPage *MainPage) isOverviewHidden() bool {
	return state.Current.Toggle(toggleHideOverview, false)
}

// toggleOverview hides or shows the path overview, and remembers the setting. Must be called on the UI thread.
func (mainPage *MainPage) toggleOverview() {
	state.Current.SetToggle(toggleHideOverview, !mainPage.isOverviewHidden())
	mainPage.applyOverviewVisibility()
	mainPage.refreshShortcutMap()
}

// applyOverviewVisibility sizes the path overview: hidden, it takes no space.
func (mainPage *MainPage) applyOverviewVisibility() {
	height := path_overview.Height
	if mainPage.isOverviewHidden() {
		height = 0
	}
	mainPage.leftLayout.ResizeItem(mainPage.pathOverview.GetLayout(), height, 0)
}

// overviewShortcut is the shortcut that hides or shows the path overview.
func (mainPage *MainPage) overviewShortcut() shortcut_helper.ShortcutEntry {
	name := "Hide overview"
	if mainPage.isOverviewHidden() {
		name = "Show overview"
	}
	return shortcut_helper.ShortcutEntry{KeyCombo: []string{"o"}, Name: name, Group: shortcut_helper.GroupView}
}

// SetPages sets the pages of the application, on which the dialogs of the components are shown as well.
func (mainPage *MainPage) SetPages(pages *tview.Pages) {
	mainPage.basePage.SetPages(pages)
	mainPage.fileBrowser.SetDialogPages(pages)
	mainPage.snapshotBrowser.SetDialogPages(pages)
}
