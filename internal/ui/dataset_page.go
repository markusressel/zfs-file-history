package ui

import (
	"fmt"
	"time"
	"zfs-file-history/internal/logging"
	"zfs-file-history/internal/ui/dataset_browser"
	"zfs-file-history/internal/ui/dataset_info"
	"zfs-file-history/internal/ui/dialog"
	"zfs-file-history/internal/ui/shortcut_helper"
	"zfs-file-history/internal/ui/snapshot_browser"
	"zfs-file-history/internal/ui/status_message"
	"zfs-file-history/internal/ui/theme"
	uiutil "zfs-file-history/internal/ui/util"
	"zfs-file-history/internal/zfs"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

type DatasetPage struct {
	application     *tview.Application
	pages           *tview.Pages
	header          *ApplicationHeaderComponent
	shortcutMap     *shortcut_helper.ShortcutMapComponent
	datasetBrowser  *dataset_browser.DatasetBrowserComponent
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

func NewDatasetPage(application *tview.Application, path string) *DatasetPage {

	datasetInfo := dataset_info.NewDatasetInfo(application)
	snapshotBrowser := snapshot_browser.NewSnapshotBrowser(application)

	datasetBrowser := dataset_browser.NewDatasetBrowser(application)

	datasetPage := &DatasetPage{
		application:     application,
		datasetBrowser:  datasetBrowser,
		datasetInfo:     datasetInfo,
		snapshotBrowser: snapshotBrowser,
	}

	snapshotBrowser.Events.Subscribe(func(event snapshot_browser.Event) {
		switch event := event.(type) {
		case snapshot_browser.StatusMessageEvent:
			datasetPage.showStatusMessage(event.Message)
		}
	})

	datasetBrowser.Events.Subscribe(func(event dataset_browser.Event) {
		switch e := event.(type) {
		case dataset_browser.PathChangedEvent:
			datasetInfo.SetPath(e.NewPath)
			snapshotBrowser.SetPath(e.NewPath, false)
		case dataset_browser.DatasetBrowserStatusEvent:
			datasetPage.showStatusMessage(e.Message)
		case dataset_browser.SelectedDatasetChangedEvent:
			snapshotBrowser.SetFileEntry(nil) // No file selected in dataset view
			if datasetBrowser.HasFocus() {
				datasetPage.updateShortcutMap(datasetBrowser)
			}
		case dataset_browser.RequestFileHistoryEvent:
			overlay := dialog.NewFileHistoryOverlay(datasetPage.application, e.FileEntry, datasetPage.snapshotBrowser.GetEntries())
			dialog.ShowDialogOnPages(datasetPage.application, datasetPage.pages, overlay, func() {
				datasetPage.datasetBrowser.Refresh(false)
			})
		}
	})

	snapshotBrowser.Events.Subscribe(func(event snapshot_browser.Event) {
		switch e := event.(type) {
		case snapshot_browser.SelectedSnapshotChanged:
			datasetBrowser.SetSelectedSnapshot(e.Snapshot)
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
			datasetPage.datasetInfo.SetPath(currentPath)
			datasetPage.snapshotBrowser.SetPath(currentPath, true)
			datasetBrowser.Refresh(false)
		}
	})

	datasetPage.layout.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		key := event.Key()
		switch key {
		case tcell.KeyCtrlN:
			datasetPage.CycleFocus(false)
		case tcell.KeyCtrlP:
			datasetPage.CycleFocus(true)
		case tcell.KeyF5:
			// also reloads the dataset list, via the zfs.DatasetsLoaded subscription
			zfs.RefreshZfsData()
		default:
		}
		return event
	})

	datasetBrowser.Events.Subscribe(func(event dataset_browser.Event) {
		switch e := event.(type) {
		case dataset_browser.RequestFocusEvent:
			application.SetFocus(e.Layout)
		case dataset_browser.CreateSnapshotEvent:
			name := e.SnapshotName
			err := datasetInfo.CreateSnapshot(name)
			if err != nil {
				logging.Error("Failed to create snapshot: %s", err)
				datasetPage.showStatusMessage(status_message.NewErrorStatusMessage(fmt.Sprintf("Failed to create snapshot: %s", err)))
			} else {
				snapshotBrowser.Refresh(true)
				snapshotBrowser.SelectLatest()
				datasetPage.showStatusMessage(status_message.NewSuccessStatusMessage(fmt.Sprintf("Snapshot '%s' created.", name)))
			}
		}
	})

	return datasetPage
}

func (datasetPage *DatasetPage) createLayout() *tview.Flex {
	datasetPageLayout := tview.NewFlex().SetDirection(tview.FlexRow)

	header := NewApplicationHeader(datasetPage.application)
	datasetPageLayout.AddItem(header.layout, 1, 0, false)

	windowLayout := tview.NewFlex().SetDirection(tview.FlexColumn)
	//dialog := createDatasetBrowserActionDialog()

	windowLayout.AddItem(datasetPage.datasetBrowser.GetLayout(), 0, 2, true)

	infoLayout := tview.NewFlex().SetDirection(tview.FlexRow)
	infoLayout.AddItem(datasetPage.datasetInfo.GetLayout(), 0, 1, false)
	infoLayout.AddItem(datasetPage.snapshotBrowser.GetLayout(), 0, 2, false)
	windowLayout.AddItem(infoLayout, 0, 1, false)

	datasetPageLayout.AddItem(windowLayout, 0, 1, true)

	datasetPage.windowLayout = windowLayout
	datasetPage.infoLayout = infoLayout

	// Set mouse capture on the top-level layout to capture drags anywhere on the screen
	datasetPageLayout.SetMouseCapture(func(action tview.MouseAction, event *tcell.EventMouse) (tview.MouseAction, *tcell.EventMouse) {
		if datasetPage.pages != nil {
			frontPage, _ := datasetPage.pages.GetFrontPage()
			if frontPage != string(Main) {
				// Reset any active hover/drag states
				datasetPage.isDragging = false
				datasetPage.dragType = dragNone
				datasetPage.hoveredBoundary = boundaryNone
				return tview.MouseConsumed, nil
			}
		}

		mouseX, mouseY := event.Position()
		buttons := event.Buttons()

		diX, diY, diW, diH := datasetPage.datasetInfo.GetLayout().GetRect()
		_, sbY, _, sbH := datasetPage.snapshotBrowser.GetLayout().GetRect()
		winX, _, winW, _ := windowLayout.GetRect()

		// 1. If currently dragging
		if datasetPage.isDragging {
			if buttons == tcell.ButtonNone || action == tview.MouseLeftUp {
				datasetPage.isDragging = false
				datasetPage.dragType = dragNone
				datasetPage.hoveredBoundary = boundaryNone
				if datasetPage.dragTimer != nil {
					datasetPage.dragTimer.Stop()
					datasetPage.dragTimer = nil
				}
				datasetPage.updateBorderHighlights()
				return tview.MouseConsumed, nil
			}

			// Rate limit updates to 30ms to prevent redraw flooding/input lag
			now := time.Now()
			if now.Sub(datasetPage.lastDragRedraw) > 30*time.Millisecond {
				datasetPage.lastDragRedraw = now
				if datasetPage.dragTimer != nil {
					datasetPage.dragTimer.Stop()
					datasetPage.dragTimer = nil
				}
				datasetPage.applyResize(mouseX, mouseY, winX, winW, diY, diH, sbY, sbH)
				return tview.MouseConsumed, nil
			} else {
				// Schedule a trailing redraw for the final drag position
				if datasetPage.dragTimer != nil {
					datasetPage.dragTimer.Stop()
				}
				datasetPage.dragTimer = time.AfterFunc(30*time.Millisecond, func() {
					datasetPage.application.QueueUpdateDraw(func() {
						datasetPage.applyResize(mouseX, mouseY, winX, winW, diY, diH, sbY, sbH)
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

		if newHover != datasetPage.hoveredBoundary {
			datasetPage.hoveredBoundary = newHover
			datasetPage.updateBorderHighlights()
			return tview.MouseConsumed, nil
		}

		// 3. Initiate dragging
		if buttons&tcell.Button1 != 0 && action == tview.MouseLeftDown {
			if isOnHorizontal {
				datasetPage.isDragging = true
				datasetPage.dragType = dragHorizontal
				datasetPage.lastDragRedraw = time.Now()
				return tview.MouseConsumed, nil
			} else if isOnVertical {
				datasetPage.isDragging = true
				datasetPage.dragType = dragVertical
				datasetPage.lastDragRedraw = time.Now()
				return tview.MouseConsumed, nil
			}
		}

		return action, event
	})

	// Configure drawing of highlighted adjacent borders after the screen draws
	datasetPage.application.SetAfterDrawFunc(func(screen tcell.Screen) {
		if datasetPage.pages != nil {
			frontPage, _ := datasetPage.pages.GetFrontPage()
			if frontPage != string(Main) {
				return
			}
		}

		// Highlight vertical boundary adjacent line segment
		if datasetPage.hoveredBoundary == boundaryVertical || (datasetPage.isDragging && datasetPage.dragType == dragVertical) {
			_, diY, _, _ := datasetPage.datasetInfo.GetLayout().GetRect()
			diX, _, diW, _ := datasetPage.datasetInfo.GetLayout().GetRect()
			_, sbY, _, sbH := datasetPage.snapshotBrowser.GetLayout().GetRect()

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
		if datasetPage.hoveredBoundary == boundaryHorizontal || (datasetPage.isDragging && datasetPage.dragType == dragHorizontal) {
			diX, _, diW, _ := datasetPage.datasetInfo.GetLayout().GetRect()
			_, sbY, _, sbH := datasetPage.snapshotBrowser.GetLayout().GetRect()

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

	datasetPage.header = header

	shortcutMap := shortcut_helper.NewShortcutMap(datasetPage.application)
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

func (datasetPage *DatasetPage) CycleFocus(reversed bool) {
	components := []FocusableUiComponent{
		datasetPage.datasetBrowser,
		datasetPage.datasetInfo,
		datasetPage.snapshotBrowser,
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
	datasetPage.updateShortcutMap(nextFocusedComponent)
}

func (datasetPage *DatasetPage) showStatusMessage(status *status_message.StatusMessage) {
	datasetPage.header.SetStatus(status)
}

func (datasetPage *DatasetPage) setShortcutMap(shortcutEntries []shortcut_helper.ShortcutEntry) {
	datasetPage.shortcutMap.SetEntries(shortcutEntries)
}

func (datasetPage *DatasetPage) clearShortcutMap() {
	datasetPage.shortcutMap.Clear()
}

func (datasetPage *DatasetPage) updateShortcutMap(component FocusableUiComponent) {
	if c, ok := component.(shortcut_helper.ShortcutMapProvider); ok {
		shortcutMap := c.GetShortcutMap()

		globalShortcutMapEntries := []shortcut_helper.ShortcutEntry{
			{KeyCombo: []string{"ctrl+n", "ctrl+p"}, Name: "Cycle focus"},
			{KeyCombo: []string{"⭾", "shift+⭾"}, Name: "Switch page"},
			{KeyCombo: []string{"F5"}, Name: "Refresh"},
			{KeyCombo: []string{"ctrl+q"}, Name: "Quit"},
		}

		shortcutMap = append(shortcutMap, globalShortcutMapEntries...)
		datasetPage.setShortcutMap(shortcutMap)
	} else {
		datasetPage.clearShortcutMap()
	}
}

func (datasetPage *DatasetPage) updateBorderHighlights() {
	// Redraw logic is handled by SetAfterDrawFunc based on the hoveredBoundary/isDragging states.
}

func (datasetPage *DatasetPage) applyResize(mouseX, mouseY, winX, winW, diY, diH, sbY, sbH int) {
	if datasetPage.dragType == dragVertical {
		newLeftWidth := mouseX - winX
		minWidth := 10
		if newLeftWidth < minWidth {
			newLeftWidth = minWidth
		}
		if newLeftWidth > winW-minWidth {
			newLeftWidth = winW - minWidth
		}
		newRightWidth := winW - newLeftWidth

		datasetPage.windowLayout.ResizeItem(datasetPage.datasetBrowser.GetLayout(), 0, newLeftWidth)
		datasetPage.windowLayout.ResizeItem(datasetPage.infoLayout, 0, newRightWidth)
	} else if datasetPage.dragType == dragHorizontal {
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

		datasetPage.infoLayout.ResizeItem(datasetPage.datasetInfo.GetLayout(), 0, newTopHeight)
		datasetPage.infoLayout.ResizeItem(datasetPage.snapshotBrowser.GetLayout(), 0, newBottomHeight)
	}
}

func (datasetPage *DatasetPage) SetPages(pages *tview.Pages) {
	datasetPage.pages = pages
}
