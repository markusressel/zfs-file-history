package ui

import (
	"zfs-file-history/internal/ui/pool_browser"
	"zfs-file-history/internal/ui/pool_status"
	"zfs-file-history/internal/ui/shortcut_helper"
	"zfs-file-history/internal/ui/status_message"
	uiutil "zfs-file-history/internal/ui/util"
	"zfs-file-history/internal/zfs"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// PoolPage is the pool page: the pools, next to the status and the devices of the selected one (like "zpool
// status"), like the dataset page. The status and the device names are wide, so if they do not fit next to the
// pools, the panes are stacked instead (see isStacked).
type PoolPage struct {
	basePage
	poolBrowser *pool_browser.PoolBrowserComponent
	poolStatus  *pool_status.PoolStatusComponent
	poolDevices *pool_status.PoolDevicesComponent
	layout      *tview.Flex
	// split separates the pools and statusSplit, statusSplit the status and the devices
	split       *uiutil.ResizableSplit
	statusSplit *uiutil.ResizableSplit
}

const (
	// poolsMaxPercent and statusMaxPercent are the maximum shares of the height of the pools and the status, when
	// they fit their content: the panes below them keep the rest
	poolsMaxPercent  = 50
	statusMaxPercent = 60
)

func NewPoolPage(application *tview.Application, messages *status_message.Center) *PoolPage {
	poolBrowser := pool_browser.NewPoolBrowser(application)
	poolStatus := pool_status.NewPoolStatus(application)
	poolDevices := pool_status.NewPoolDevices(application)

	poolPage := &PoolPage{
		basePage:    basePage{application: application, name: Pool, messages: messages},
		poolBrowser: poolBrowser,
		poolStatus:  poolStatus,
		poolDevices: poolDevices,
	}
	poolPage.focusableComponents = func() []FocusableUiComponent {
		return []FocusableUiComponent{poolBrowser, poolStatus, poolDevices}
	}
	poolPage.componentTitles = map[FocusableUiComponent]string{
		poolBrowser: "Pools", poolStatus: "Pool status", poolDevices: "Devices",
	}

	// like on the dataset page, loading the status of the selected pool waits until the selection rests
	selectionDebouncer := uiutil.NewDebouncer(application, uiutil.SelectionDebounceDelay)
	selectedPool := ""
	poolBrowser.Events.Subscribe(func(event pool_browser.Event) {
		switch e := event.(type) {
		case pool_browser.PoolBrowserStatusEvent:
			poolPage.showStatusMessage(e.Message)
		case pool_browser.SelectedPoolChangedEvent:
			selectedPool = ""
			if e.Pool != nil {
				selectedPool = e.Pool.Name
			}
			selectionDebouncer.Call(func() { poolStatus.SetPool(selectedPool) })
		}
	})
	poolStatus.Events.Subscribe(func(event pool_status.Event) {
		if e, ok := event.(pool_status.StatusLoadedEvent); ok {
			poolDevices.SetStatus(e.Status)
		}
	})

	poolPage.layout = poolPage.createLayout()

	// e.g. after F5, or a change reported by the ZFS event watcher (see zfs.WatchZpoolEvents)
	reload := func(_ struct{}) {
		poolBrowser.Refresh()
		poolStatus.Refresh()
	}
	uiutil.SubscribeUI(zfs.DatasetsLoaded, application, reload)
	uiutil.SubscribeUI(zfs.PoolsChanged, application, reload)

	poolPage.layout.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if poolPage.handleFocusKeys(event) {
			return nil
		}
		if event.Key() == tcell.KeyF5 {
			poolPage.refresh()
		}
		return event
	})

	return poolPage
}

// refresh reloads the ZFS data, and with it the pools (via the zfs.DatasetsLoaded subscription).
func (poolPage *PoolPage) refresh() {
	zfs.RefreshZfsData()
}

func (poolPage *PoolPage) createLayout() *tview.Flex {
	poolPageLayout := tview.NewFlex().SetDirection(tview.FlexRow)

	poolPage.header = NewApplicationHeader(poolPage.application, poolPage.messages)
	poolPageLayout.AddItem(poolPage.header.layout, 1, 0, false)

	// the pools and the status fit their content, the devices get the rest (and scroll)
	poolPage.statusSplit = poolPage.newSplit(poolPage.poolStatus.GetLayout(), poolPage.poolDevices.GetLayout(), 1, 1, true).
		SetAutoSize(func(size int) int {
			_, height := poolPage.poolStatus.ContentSize()
			return uiutil.FitContent(size, height, statusMaxPercent)
		})
	poolPage.split = poolPage.newSplit(poolPage.poolBrowser.GetLayout(), poolPage.statusSplit, 1, 4, true).
		SetProportions(false, 1, 2).
		SetVerticalFunc(func(width int, _ int) bool { return poolPage.isStacked(width) }).
		SetAutoSize(func(size int) int {
			width, height := poolPage.poolBrowser.ContentSize()
			if poolPage.split.IsVertical() {
				return uiutil.FitContent(size, height, poolsMaxPercent)
			}
			// side by side, everything fits (see isStacked)
			return width
		})
	poolPageLayout.AddItem(poolPage.split, 0, 1, true)
	captureSplitDrags(poolPageLayout, poolPage.split, poolPage.statusSplit)

	// hidden with ? (see shortcut_helper.ToggleShortcuts)
	shortcutMap := shortcut_helper.NewShortcutMap(poolPage.application).SetCollapsible().SetEnabledFunc(poolPage.isInFront)
	shortcutMap.SetOnHeightChanged(func(height int) {
		poolPageLayout.ResizeItem(shortcutMap.GetLayout(), height, 0)
	})
	poolPageLayout.AddItem(shortcutMap.GetLayout(), 1, 0, false)
	poolPage.shortcutMap = shortcutMap
	poolPage.reserveShortcutMapHeight()

	return poolPageLayout
}

// isStacked returns whether the pools are shown above the status and the devices, instead of next to them: if the
// widest of them does not fit next to the pools in the given width.
func (poolPage *PoolPage) isStacked(width int) bool {
	statusWidth, _ := poolPage.poolStatus.ContentSize()
	devicesWidth, _ := poolPage.poolDevices.ContentSize()
	if statusWidth == 0 && devicesWidth == 0 {
		// not loaded yet: keep the arrangement, instead of switching back and forth on startup
		return poolPage.split.IsVertical()
	}
	poolsWidth, _ := poolPage.poolBrowser.ContentSize()
	return width < poolsWidth+max(statusWidth, devicesWidth)
}

// Init loads the pools.
func (poolPage *PoolPage) Init() {
	poolPage.poolBrowser.Refresh()
}

// SetPages sets the pages of the application, on which the dialogs of the components are shown as well.
func (poolPage *PoolPage) SetPages(pages *tview.Pages) {
	poolPage.basePage.SetPages(pages)
	poolPage.poolBrowser.SetDialogPages(pages)
}
