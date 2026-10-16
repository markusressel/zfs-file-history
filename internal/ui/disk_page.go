package ui

import (
	"zfs-file-history/internal/disk"
	"zfs-file-history/internal/ui/disk_browser"
	"zfs-file-history/internal/ui/pool_activity"
	"zfs-file-history/internal/ui/shortcut_helper"
	"zfs-file-history/internal/ui/status_message"
	uiutil "zfs-file-history/internal/ui/util"
	"zfs-file-history/internal/zfs"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// disksMaxPercent is the maximum share of the height of the disks, when they fit their content: the details and the
// attributes of the selected disk keep the rest
const disksMaxPercent = 50

// DiskPage is the disks page: the disks of all pools with their temperature and health, above the details and the
// SMART attributes of the selected one.
type DiskPage struct {
	basePage
	diskBrowser *disk_browser.DiskBrowserComponent
	diskDetails *disk_browser.DiskDetailsComponent
	layout      *tview.Flex
	// split separates the disks and the details
	split *uiutil.ResizableSplit
}

// NewDiskPage returns the disks page, reading the temperatures of the disks with the sampler and their SMART values
// with smart (the ones of the activities and the pools page, so the SMART values are not read twice). The disks are
// the ones of the statuses the activity monitor loads.
func NewDiskPage(application *tview.Application, messages *status_message.Center, activities *pool_activity.Monitor, sampler disk.TemperatureSampler, smart disk.SmartSource) *DiskPage {
	diskBrowser := disk_browser.NewDiskBrowser(application, sampler, smart)
	diskDetails := disk_browser.NewDiskDetails(application)

	diskPage := &DiskPage{
		basePage:    basePage{application: application, name: Disks, messages: messages, activities: activities},
		diskBrowser: diskBrowser,
		diskDetails: diskDetails,
	}
	diskBrowser.SetEnabledFunc(diskPage.isInFront)
	diskDetails.SetEnabledFunc(diskPage.isInFront)
	diskPage.focusableComponents = func() []FocusableUiComponent {
		return []FocusableUiComponent{diskBrowser, diskDetails}
	}
	diskPage.componentTitles = map[FocusableUiComponent]string{
		diskBrowser: "Disks", diskDetails: "Attributes",
	}

	diskBrowser.Events.Subscribe(func(event disk_browser.Event) {
		if e, ok := event.(disk_browser.SelectedDiskChangedEvent); ok {
			diskDetails.SetDisk(e.Disk)
		}
	})
	activities.OnStatusesLoaded(diskBrowser.SetStatuses)

	diskPage.layout = diskPage.createLayout()
	diskPage.layout.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if diskPage.handleFocusKeys(event) {
			return nil
		}
		if event.Key() == tcell.KeyF5 {
			diskPage.refresh()
		}
		return event
	})
	return diskPage
}

// refresh loads the statuses of the pools again, and with them the disks, and reads the SMART values again.
func (diskPage *DiskPage) refresh() {
	zfs.RefreshZfsData()
	diskPage.activities.Refresh()
	diskPage.diskBrowser.ReadSmart()
}

func (diskPage *DiskPage) createLayout() *tview.Flex {
	diskPageLayout := tview.NewFlex().SetDirection(tview.FlexRow)

	diskPage.header = NewApplicationHeader(diskPage.application, diskPage.messages)
	diskPageLayout.AddItem(diskPage.header.layout, 1, 0, false)
	diskPage.addPoolPanels(diskPageLayout)

	// the disks fit their content, the details and attributes get the rest (and scroll)
	diskPage.split = diskPage.newSplit(diskPage.diskBrowser.GetLayout(), diskPage.diskDetails.GetLayout(), 1, 2, true).
		SetAutoSize(func(size int) int {
			_, height := diskPage.diskBrowser.ContentSize()
			return uiutil.FitContent(size, height, disksMaxPercent)
		})
	diskPageLayout.AddItem(diskPage.split, 0, 1, true)
	captureSplitDrags(diskPageLayout, diskPage.split)

	// hidden with ? (see shortcut_helper.ToggleShortcuts)
	shortcutMap := shortcut_helper.NewShortcutMap(diskPage.application).SetCollapsible().SetEnabledFunc(diskPage.isInFront)
	shortcutMap.SetOnHeightChanged(func(height int) {
		diskPageLayout.ResizeItem(shortcutMap.GetLayout(), height, 0)
	})
	diskPageLayout.AddItem(shortcutMap.GetLayout(), 1, 0, false)
	diskPage.shortcutMap = shortcutMap
	diskPage.reserveShortcutMapHeight()

	return diskPageLayout
}

// SetPages sets the pages of the application, on which the dialogs of the components are shown as well.
func (diskPage *DiskPage) SetPages(pages *tview.Pages) {
	diskPage.basePage.SetPages(pages)
	diskPage.diskBrowser.SetDialogPages(pages)
	diskPage.diskDetails.SetDialogPages(pages)
}
