package ui

import (
	"fmt"
	"time"
	"zfs-file-history/internal/logging"
	"zfs-file-history/internal/ui/dataset_info"
	"zfs-file-history/internal/ui/dialog"
	"zfs-file-history/internal/ui/file_browser"
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
	datasetInfo     *dataset_info.DatasetInfoComponent
	snapshotBrowser *snapshot_browser.SnapshotBrowserComponent
	layout          *tview.Flex
	windowLayout    *tview.Flex
	infoLayout      *tview.Flex

	wasInitialized bool

	isDragging      bool
	dragType        dragType
	hoveredBoundary boundaryType
	lastDragRedraw  time.Time
	dragTimer       *time.Timer
}

func NewMainPage(application *tview.Application, path string) *MainPage {

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
		case file_browser.RequestFileHistoryEvent:
			overlay := dialog.NewFileHistoryOverlay(mainPage.application, e.FileEntry, mainPage.snapshotBrowser.GetEntries())
			dialog.ShowDialogOnPages(mainPage.application, mainPage.pages, overlay, func() {
				mainPage.fileBrowser.Refresh(false)
			})
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

	if zfs.IsDatasetsLoaded() {
		mainPage.Init(path)
	}

	uiutil.SubscribeUI(zfs.DatasetsLoaded, application, func(_ struct{}) {
		if !mainPage.wasInitialized {
			mainPage.Init(path)
		} else {
			currentPath := fileBrowser.GetPath()
			mainPage.datasetInfo.SetPath(currentPath)
			mainPage.snapshotBrowser.SetPath(currentPath, true)
		}
	})

	mainPage.layout.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		key := event.Key()
		switch key {
		case tcell.KeyTab:
			mainPage.CycleFocus(false)
		case tcell.KeyBacktab:
			mainPage.CycleFocus(true)
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

	mainPage.windowLayout = windowLayout
	mainPage.infoLayout = infoLayout

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

		diX, diY, diW, diH := mainPage.datasetInfo.GetLayout().GetRect()
		_, sbY, _, sbH := mainPage.snapshotBrowser.GetLayout().GetRect()
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
				mainPage.applyResize(mouseX, mouseY, winX, winW, diY, diH, sbY, sbH)
				return tview.MouseConsumed, nil
			} else {
				// Schedule a trailing redraw for the final drag position
				if mainPage.dragTimer != nil {
					mainPage.dragTimer.Stop()
				}
				mainPage.dragTimer = time.AfterFunc(30*time.Millisecond, func() {
					mainPage.application.QueueUpdateDraw(func() {
						mainPage.applyResize(mouseX, mouseY, winX, winW, diY, diH, sbY, sbH)
					})
				})
				return action, nil // consume event for children but do not trigger immediate screen redraw
			}
		}

		// 2. Not dragging: detect hover boundaries
		isOnVertical := false
		if mouseY >= diY && mouseY < sbY+sbH {
			if mouseX == diX || mouseX == diX-1 {
				isOnVertical = true
			}
		}

		isOnHorizontal := false
		if mouseX >= diX && mouseX < diX+diW {
			if mouseY == sbY || mouseY == sbY-1 {
				isOnHorizontal = true
			}
		}

		newHover := boundaryNone
		if isOnHorizontal {
			newHover = boundaryHorizontal
		} else if isOnVertical {
			newHover = boundaryVertical
		}

		if newHover != mainPage.hoveredBoundary {
			mainPage.hoveredBoundary = newHover
			mainPage.updateBorderHighlights()
			return tview.MouseConsumed, nil
		}

		// 3. Initiate dragging
		if buttons&tcell.Button1 != 0 && action == tview.MouseLeftDown {
			if isOnHorizontal {
				mainPage.isDragging = true
				mainPage.dragType = dragHorizontal
				mainPage.lastDragRedraw = time.Now()
				return tview.MouseConsumed, nil
			} else if isOnVertical {
				mainPage.isDragging = true
				mainPage.dragType = dragVertical
				mainPage.lastDragRedraw = time.Now()
				return tview.MouseConsumed, nil
			}
		}

		return action, event
	})

	// Configure drawing of highlighted adjacent borders after the screen draws
	mainPage.application.SetAfterDrawFunc(func(screen tcell.Screen) {
		if mainPage.pages != nil {
			frontPage, _ := mainPage.pages.GetFrontPage()
			if frontPage != string(Main) {
				return
			}
		}

		// Highlight vertical boundary adjacent line segment
		if mainPage.hoveredBoundary == boundaryVertical || (mainPage.isDragging && mainPage.dragType == dragVertical) {
			_, diY, _, _ := mainPage.datasetInfo.GetLayout().GetRect()
			diX, _, diW, _ := mainPage.datasetInfo.GetLayout().GetRect()
			_, sbY, _, sbH := mainPage.snapshotBrowser.GetLayout().GetRect()

			if diW > 0 && sbH > 0 {
				highlightColor := theme.Primary
				for y := diY; y < sbY+sbH; y++ {
					for _, x := range []int{diX - 1, diX} {
						primary, combining, style, _ := screen.GetContent(x, y)
						newStyle := style.Foreground(highlightColor)
						screen.SetContent(x, y, primary, combining, newStyle)
					}
				}
			}
		}

		// Highlight horizontal boundary adjacent line segment
		if mainPage.hoveredBoundary == boundaryHorizontal || (mainPage.isDragging && mainPage.dragType == dragHorizontal) {
			diX, _, diW, _ := mainPage.datasetInfo.GetLayout().GetRect()
			_, sbY, _, sbH := mainPage.snapshotBrowser.GetLayout().GetRect()

			if diW > 0 && sbH > 0 {
				highlightColor := theme.Primary
				for x := diX; x < diX+diW; x++ {
					for _, y := range []int{sbY - 1, sbY} {
						primary, combining, style, _ := screen.GetContent(x, y)
						newStyle := style.Foreground(highlightColor)
						screen.SetContent(x, y, primary, combining, newStyle)
					}
				}
			}
		}
	})

	mainPage.header = header

	shortcutMap := shortcut_helper.NewShortcutMap(mainPage.application)
	shortcutMap.SetOnHeightChanged(func(height int) {
		mainPageLayout.ResizeItem(shortcutMap.GetLayout(), height, 0)
	})
	mainPageLayout.AddItem(shortcutMap.GetLayout(), 1, 0, false)
	mainPage.shortcutMap = shortcutMap

	return mainPageLayout
}

func (mainPage *MainPage) Init(path string) {
	mainPage.wasInitialized = true
	mainPage.datasetInfo.SetPath(path)
	mainPage.snapshotBrowser.SetPath(path, false)
	mainPage.fileBrowser.SetPath(path, false)
	mainPage.fileBrowser.SelectFirstEntryIfExists()
}

func (mainPage *MainPage) CycleFocus(reversed bool) {
	components := []FocusableUiComponent{
		mainPage.fileBrowser,
		mainPage.datasetInfo,
		mainPage.snapshotBrowser,
	}

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

func (mainPage *MainPage) setShortcutMap(shortcutEntries []shortcut_helper.ShortcutEntry) {
	mainPage.shortcutMap.SetEntries(shortcutEntries)
}

func (mainPage *MainPage) clearShortcutMap() {
	mainPage.shortcutMap.Clear()
}

func (mainPage *MainPage) updateShortcutMap(component FocusableUiComponent) {
	if c, ok := component.(shortcut_helper.ShortcutMapProvider); ok {
		shortcutMap := c.GetShortcutMap()

		globalShortcutMapEntries := []shortcut_helper.ShortcutEntry{
			{KeyCombo: []string{"⭾", "shift+⭾"}, Name: "Cycle focus"},
			{KeyCombo: []string{"F5"}, Name: "Refresh"},
			{KeyCombo: []string{"ctrl+q"}, Name: "Quit"},
		}

		shortcutMap = append(shortcutMap, globalShortcutMapEntries...)
		mainPage.setShortcutMap(shortcutMap)
	} else {
		mainPage.clearShortcutMap()
	}
}

func (mainPage *MainPage) updateBorderHighlights() {
	// Redraw logic is handled by SetAfterDrawFunc based on the hoveredBoundary/isDragging states.
}

func (mainPage *MainPage) applyResize(mouseX, mouseY, winX, winW, diY, diH, sbY, sbH int) {
	if mainPage.dragType == dragVertical {
		newLeftWidth := mouseX - winX
		minWidth := 10
		if newLeftWidth < minWidth {
			newLeftWidth = minWidth
		}
		if newLeftWidth > winW-minWidth {
			newLeftWidth = winW - minWidth
		}
		newRightWidth := winW - newLeftWidth

		mainPage.windowLayout.ResizeItem(mainPage.fileBrowser.GetLayout(), 0, newLeftWidth)
		mainPage.windowLayout.ResizeItem(mainPage.infoLayout, 0, newRightWidth)
	} else if mainPage.dragType == dragHorizontal {
		infoH := diH + sbH
		infoY := diY
		newTopHeight := mouseY - infoY
		minTopHeight := 4
		minBottomHeight := 5
		if newTopHeight < minTopHeight {
			newTopHeight = minTopHeight
		}
		if newTopHeight > infoH-minBottomHeight {
			newTopHeight = infoH - minBottomHeight
		}
		newBottomHeight := infoH - newTopHeight

		mainPage.infoLayout.ResizeItem(mainPage.datasetInfo.GetLayout(), 0, newTopHeight)
		mainPage.infoLayout.ResizeItem(mainPage.snapshotBrowser.GetLayout(), 0, newBottomHeight)
	}
}

func (mainPage *MainPage) SetPages(pages *tview.Pages) {
	mainPage.pages = pages
}
