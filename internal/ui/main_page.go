package ui

import (
	"fmt"
	"time"
	"zfs-file-history/internal/data"
	"zfs-file-history/internal/logging"
	"zfs-file-history/internal/ui/dialog"
	"zfs-file-history/internal/ui/file_browser"
	"zfs-file-history/internal/ui/path_overview"
	"zfs-file-history/internal/ui/shortcut_helper"
	"zfs-file-history/internal/ui/snapshot_browser"
	"zfs-file-history/internal/ui/status_message"
	"zfs-file-history/internal/ui/theme"
	uiutil "zfs-file-history/internal/ui/util"
	"zfs-file-history/internal/zfs"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

type dragType int

const (
	dragNone dragType = iota
	dragVertical
	dragHorizontal
)

type boundaryType int

const (
	boundaryNone boundaryType = iota
	boundaryVertical
	boundaryHorizontal
)

type MainPage struct {
	application     *tview.Application
	pages           *tview.Pages
	header          *ApplicationHeaderComponent
	shortcutMap     *shortcut_helper.ShortcutMapComponent
	fileBrowser     *file_browser.FileBrowserComponent
	snapshotBrowser *snapshot_browser.SnapshotBrowserComponent
	// pathOverview is below the file browser, in leftLayout
	pathOverview *path_overview.PathOverviewComponent
	layout       *tview.Flex
	windowLayout *tview.Flex
	leftLayout   *tview.Flex

	wasInitialized bool

	isDragging      bool
	dragType        dragType
	hoveredBoundary boundaryType
	lastDragRedraw  time.Time
	dragTimer       *time.Timer
}

func NewMainPage(application *tview.Application, path string) *MainPage {
	snapshotBrowser := snapshot_browser.NewSnapshotBrowser(application)

	fileBrowser := file_browser.NewFileBrowser(application)
	pathOverview := path_overview.NewPathOverview(application, fileBrowser.DiffCounts)

	mainPage := &MainPage{
		application:     application,
		fileBrowser:     fileBrowser,
		snapshotBrowser: snapshotBrowser,
		pathOverview:    pathOverview,
	}

	snapshotBrowser.Events.Subscribe(func(event snapshot_browser.Event) {
		switch event := event.(type) {
		case snapshot_browser.StatusMessageEvent:
			mainPage.showStatusMessage(event.Message)
		}
	})

	fileBrowser.Events.Subscribe(func(event file_browser.Event) {
		switch e := event.(type) {
		case file_browser.EntriesLoadedEvent:
			pathOverview.SetWorkingCopy(fileBrowser.WorkingCopyListing())
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
		}
	})

	// the snapshot browser opens the history of what h opens in the file browser, at the selected snapshot
	snapshotBrowser.SetHistoryTarget(fileBrowser.HistoryEntry)
	snapshotBrowser.Events.Subscribe(func(event snapshot_browser.Event) {
		switch e := event.(type) {
		case snapshot_browser.RequestHistoryEvent:
			mainPage.showHistory(e.Entry, e.Snapshot.Snapshot)
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
		key := event.Key()
		if (key == tcell.KeyTab || key == tcell.KeyBacktab) && event.Modifiers()&tcell.ModCtrl == 0 {
			mainPage.CycleFocus(key == tcell.KeyBacktab || event.Modifiers()&tcell.ModShift != 0)
			return nil
		}
		switch key {
		case tcell.KeyF5:
			zfs.RefreshZfsData()
			fileBrowser.Refresh(false)
		default:
		}
		return event
	})

	fileBrowser.Events.Subscribe(func(event file_browser.Event) {
		switch e := event.(type) {
		case file_browser.RequestFocusEvent:
			application.SetFocus(e.Layout)
		case file_browser.SnapshotCreatedEvent:
			// emitted on the UI thread, after the snapshot was created in the background
			snapshotBrowser.SelectLatestOnNextLoad()
			snapshotBrowser.Refresh(true)
			mainPage.showStatusMessage(status_message.NewSuccessStatusMessage(fmt.Sprintf("Snapshot '%s' created.", e.SnapshotName)))
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

	// the overview below the file browser describes its folder and selected entry
	leftLayout := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(mainPage.fileBrowser.GetLayout(), 0, 1, true).
		AddItem(mainPage.pathOverview.GetLayout(), path_overview.Height, 0, false)
	windowLayout.AddItem(leftLayout, 0, 2, true)
	// the dataset info is shown on the dataset page only, the snapshots use the whole height here
	windowLayout.AddItem(mainPage.snapshotBrowser.GetLayout(), 0, 1, false)

	mainPageLayout.AddItem(windowLayout, 0, 1, true)

	mainPage.windowLayout = windowLayout
	mainPage.leftLayout = leftLayout

	// Set mouse capture on the top-level layout to capture drags anywhere on the screen
	mainPageLayout.SetMouseCapture(func(action tview.MouseAction, event *tcell.EventMouse) (tview.MouseAction, *tcell.EventMouse) {
		if mainPage.pages != nil {
			frontPage, _ := mainPage.pages.GetFrontPage()
			if frontPage != string(Main) {
				// Reset any active hover/drag states
				mainPage.isDragging = false
				mainPage.dragType = dragNone
				mainPage.hoveredBoundary = boundaryNone
				return tview.MouseConsumed, nil
			}
		}

		mouseX, mouseY := event.Position()
		buttons := event.Buttons()

		sbX, sbY, _, sbH := mainPage.snapshotBrowser.GetLayout().GetRect()
		winX, _, winW, _ := windowLayout.GetRect()

		// 1. If currently dragging
		if mainPage.isDragging {
			if buttons == tcell.ButtonNone || action == tview.MouseLeftUp {
				mainPage.isDragging = false
				mainPage.dragType = dragNone
				mainPage.hoveredBoundary = boundaryNone
				if mainPage.dragTimer != nil {
					mainPage.dragTimer.Stop()
					mainPage.dragTimer = nil
				}
				mainPage.updateBorderHighlights()
				return tview.MouseConsumed, nil
			}

			// Rate limit updates to 30ms to prevent redraw flooding/input lag
			now := time.Now()
			if now.Sub(mainPage.lastDragRedraw) > 30*time.Millisecond {
				mainPage.lastDragRedraw = now
				if mainPage.dragTimer != nil {
					mainPage.dragTimer.Stop()
					mainPage.dragTimer = nil
				}
				mainPage.applyResize(mouseX, winX, winW)
				return tview.MouseConsumed, nil
			} else {
				// Schedule a trailing redraw for the final drag position
				if mainPage.dragTimer != nil {
					mainPage.dragTimer.Stop()
				}
				mainPage.dragTimer = time.AfterFunc(30*time.Millisecond, func() {
					mainPage.application.QueueUpdateDraw(func() {
						mainPage.applyResize(mouseX, winX, winW)
					})
				})
				return action, nil // consume event for children but do not trigger immediate screen redraw
			}
		}

		// 2. Not dragging: detect hovering the boundary between the file browser and the snapshots
		isOnVertical := mouseY >= sbY && mouseY < sbY+sbH && (mouseX == sbX || mouseX == sbX-1)

		newHover := boundaryNone
		if isOnVertical {
			newHover = boundaryVertical
		}

		if newHover != mainPage.hoveredBoundary {
			mainPage.hoveredBoundary = newHover
			mainPage.updateBorderHighlights()
			return tview.MouseConsumed, nil
		}

		// 3. Initiate dragging
		if buttons&tcell.Button1 != 0 && action == tview.MouseLeftDown && isOnVertical {
			mainPage.isDragging = true
			mainPage.dragType = dragVertical
			mainPage.lastDragRedraw = time.Now()
			return tview.MouseConsumed, nil
		}

		return action, event
	})

	mainPage.header = header

	// hidden with ? (see shortcut_helper.ToggleShortcuts)
	shortcutMap := shortcut_helper.NewShortcutMap(mainPage.application).SetCollapsible()
	shortcutMap.SetOnHeightChanged(func(height int) {
		mainPageLayout.ResizeItem(shortcutMap.GetLayout(), height, 0)
	})
	mainPageLayout.AddItem(shortcutMap.GetLayout(), 1, 0, false)
	mainPage.shortcutMap = shortcutMap

	return mainPageLayout
}

func (mainPage *MainPage) Init(path string) {
	mainPage.wasInitialized = true
	mainPage.pathOverview.SetFolder(path)
	mainPage.snapshotBrowser.SetPath(path, false)
	mainPage.fileBrowser.SetPath(path, false)
	mainPage.fileBrowser.SelectFirstEntryIfExists()
}

// focusableComponents returns the components that can be focused, in focus cycle order.
func (mainPage *MainPage) focusableComponents() []FocusableUiComponent {
	return []FocusableUiComponent{
		mainPage.fileBrowser,
		mainPage.snapshotBrowser,
	}
}

// refreshShortcutMap shows the shortcuts of the focused component (or the browser, if none has focus),
// e.g. after the page was switched to.
func (mainPage *MainPage) refreshShortcutMap() {
	for _, component := range mainPage.focusableComponents() {
		if component.HasFocus() {
			mainPage.updateShortcutMap(component)
			return
		}
	}
	mainPage.updateShortcutMap(mainPage.fileBrowser)
}

func (mainPage *MainPage) CycleFocus(reversed bool) {
	components := mainPage.focusableComponents()

	currentIndex := -1
	for i, component := range components {
		if component.HasFocus() {
			currentIndex = i
			break
		}
	}

	var nextIndex int
	if currentIndex == -1 {
		nextIndex = 0
		logging.Warning("Unexpected focus state")
	} else if reversed {
		nextIndex = (currentIndex - 1 + len(components)) % len(components)
	} else {
		nextIndex = (currentIndex + 1) % len(components)
	}

	nextFocusedComponent := components[nextIndex]
	nextFocusedComponent.Focus()
	mainPage.updateShortcutMap(nextFocusedComponent)
}

func (mainPage *MainPage) showStatusMessage(status *status_message.StatusMessage) {
	mainPage.header.SetStatus(status)
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

func (mainPage *MainPage) setShortcutMap(shortcutEntries []shortcut_helper.ShortcutEntry) {
	mainPage.shortcutMap.SetEntries(shortcutEntries)
}

func (mainPage *MainPage) clearShortcutMap() {
	mainPage.shortcutMap.Clear()
}

func (mainPage *MainPage) updateShortcutMap(component FocusableUiComponent) {
	if c, ok := component.(shortcut_helper.ShortcutMapProvider); ok {
		shortcutMap := c.GetShortcutMap()

		globalShortcutMapEntries := globalShortcuts()

		shortcutMap = append(shortcutMap, globalShortcutMapEntries...)
		mainPage.setShortcutMap(shortcutMap)
	} else {
		mainPage.clearShortcutMap()
	}
}

func (mainPage *MainPage) updateBorderHighlights() {
	// Redraw logic is handled by drawBoundaryHighlights based on the hoveredBoundary/isDragging states.
}

// applyResize moves the boundary between the file browser and the snapshots to mouseX.
func (mainPage *MainPage) applyResize(mouseX, winX, winW int) {
	if mainPage.dragType != dragVertical {
		return
	}
	minWidth := 10
	newLeftWidth := max(minWidth, min(mouseX-winX, winW-minWidth))
	mainPage.windowLayout.ResizeItem(mainPage.leftLayout, 0, newLeftWidth)
	mainPage.windowLayout.ResizeItem(mainPage.snapshotBrowser.GetLayout(), 0, winW-newLeftWidth)
}

func (mainPage *MainPage) SetPages(pages *tview.Pages) {
	mainPage.pages = pages
}

// drawBoundaryHighlights highlights the pane boundary that is hovered or dragged.
// Called after each draw while this page is in front (see CreateUi).
func (mainPage *MainPage) drawBoundaryHighlights(screen tcell.Screen) {
	if mainPage.hoveredBoundary != boundaryVertical && !(mainPage.isDragging && mainPage.dragType == dragVertical) {
		return
	}
	sbX, sbY, sbW, sbH := mainPage.snapshotBrowser.GetLayout().GetRect()
	if sbW <= 0 || sbH <= 0 {
		return
	}
	for y := sbY; y < sbY+sbH; y++ {
		for _, x := range []int{sbX - 1, sbX} {
			primary, combining, style, _ := screen.GetContent(x, y)
			screen.SetContent(x, y, primary, combining, style.Foreground(theme.Primary))
		}
	}
}
