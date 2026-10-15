package ui

import (
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"
	"zfs-file-history/internal/testutil"
	"zfs-file-history/internal/ui/pool_activity"
	"zfs-file-history/internal/ui/pool_browser"
	"zfs-file-history/internal/ui/status_message"
	"zfs-file-history/internal/zfs"

	"github.com/rivo/tview"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newPoolPageApp(t *testing.T, width int, height int) (*tview.Application, *PoolPage) {
	app, _ := testutil.NewApp(testutil.WithSize(width, height))
	page := NewPoolPage(app, status_message.NewCenter(app), pool_activity.NewMonitor(app), noDisks{}, noDisks{})
	testutil.StartApp(t, app, page.layout)
	return app, page
}

// the pools, above the status of the selected one, above its devices (until the status is loaded, see
// TestPoolPage_ArrangedByTheAvailableWidth)
func TestPoolPage_Layout(t *testing.T) {
	app, page := newPoolPageApp(t, 120, 40)

	assert.Equal(t, []FocusableUiComponent{page.poolBrowser, page.poolStatus, page.poolDevices}, page.focusableComponents())
	var poolsY, statusY, devicesY, devicesHeight int
	testutil.OnUiThread(t, app, func() {
		app.ForceDraw()
		_, poolsY, _, _ = page.poolBrowser.GetLayout().GetRect()
		_, statusY, _, _ = page.poolStatus.GetLayout().GetRect()
		_, devicesY, _, devicesHeight = page.poolDevices.GetLayout().GetRect()
	})
	assert.Less(t, poolsY, statusY)
	assert.Less(t, statusY, devicesY)
	assert.Positive(t, devicesHeight)
}

// rects returns the positions and sizes of the pools, the status and the devices after drawing.
func rects(t *testing.T, app *tview.Application, page *PoolPage) (pools [4]int, status [4]int, devices [4]int) {
	testutil.OnUiThread(t, app, func() {
		app.ForceDraw()
		pools[0], pools[1], pools[2], pools[3] = page.poolBrowser.GetLayout().GetRect()
		status[0], status[1], status[2], status[3] = page.poolStatus.GetLayout().GetRect()
		devices[0], devices[1], devices[2], devices[3] = page.poolDevices.GetLayout().GetRect()
	})
	return pools, status, devices
}

// Like the dataset page: the pools next to the status and the devices, if there is enough space for them. If not,
// stacked. The pools fit their content.
func TestPoolPage_ArrangedByTheAvailableWidth(t *testing.T) {
	devices := &zfs.PoolStatus{Name: "tank", Devices: []*zfs.PoolDevice{
		{Name: "tank", State: zfs.PoolStateOnline},
		{Name: "/dev/disk/by-id/ata-WDC_WD40EFRX-68N32N0_WD-WCC7K0000001", Depth: 1, State: zfs.PoolStateOnline},
	}}
	for _, test := range []struct {
		width   int
		stacked bool
	}{{100, true}, {200, false}} {
		t.Run(fmt.Sprintf("width %d", test.width), func(t *testing.T) {
			app, page := newPoolPageApp(t, test.width, 40)
			testutil.OnUiThread(t, app, func() { page.poolDevices.SetStatus(devices) })
			poolsWidth, poolsHeight := 0, 0
			testutil.OnUiThread(t, app, func() { poolsWidth, poolsHeight = page.poolBrowser.ContentSize() })

			pools, status, devicesRect := rects(t, app, page)
			if test.stacked {
				assert.Less(t, pools[1], status[1], "above")
				assert.Equal(t, test.width, pools[2])
				assert.Equal(t, poolsHeight, pools[3], "fits its content")
			} else {
				assert.Equal(t, pools[1], status[1], "next to each other")
				assert.Less(t, pools[0], status[0])
				assert.Equal(t, poolsWidth, pools[2], "fits its content")
			}
			assert.Less(t, status[1], devicesRect[1], "the status is always above the devices")
		})
	}
}

// Until the status of a pool is loaded, its width is not known: the arrangement is kept, instead of switching back
// and forth while starting.
func TestPoolPage_KeepsTheArrangementWhileLoading(t *testing.T) {
	app, page := newPoolPageApp(t, 300, 40)
	pools, status, _ := rects(t, app, page)
	assert.Less(t, pools[1], status[1], "stacked")
}

// like on the dataset page: cycling the focus must not change the height of the shortcut map
func TestPoolPage_CyclingFocusKeepsTheLayout(t *testing.T) {
	for _, width := range []int{80, 120, 200} {
		t.Run(fmt.Sprintf("width %d", width), func(t *testing.T) {
			app, page := newPoolPageApp(t, width, 50)

			testutil.OnUiThread(t, app, func() {
				app.ForceDraw()
				page.poolBrowser.Focus()
				page.refreshShortcutMap()
				for range page.focusableComponents() {
					before := page.shortcutMap.CalculateHeightFromTerminal()
					page.CycleFocus(false)
					assert.Equal(t, before, page.shortcutMap.CalculateHeightFromTerminal())
				}
			})
		})
	}
}

// the focused component is remembered by its title, see session
func TestPoolPage_FocusComponent(t *testing.T) {
	app, page := newPoolPageApp(t, 120, 40)

	var titles []string
	page.setOnFocusChanged(func(title string) { titles = append(titles, title) })
	testutil.OnUiThread(t, app, func() {
		page.focusComponent("Devices")
		assert.True(t, page.poolDevices.HasFocus())
		page.CycleFocus(false)
		assert.True(t, page.poolBrowser.HasFocus())
		page.CycleFocus(false)
		assert.True(t, page.poolStatus.HasFocus())
	})
	assert.Equal(t, []string{"Devices", "Pools", "Pool status"}, titles)
}

// Selecting a pool shows its status and its devices.
func TestPoolPage_ShowsTheSelectedPool(t *testing.T) {
	testSystem.setPools(t, &zfs.PoolStatus{Name: "tank", State: zfs.PoolStateOnline, Devices: []*zfs.PoolDevice{
		{Name: "tank", State: zfs.PoolStateOnline},
		{Name: "sda", Depth: 1, State: zfs.PoolStateOnline},
	}})
	app, page := newPoolPageApp(t, 160, 50)

	testutil.OnUiThread(t, app, func() { page.Init() })

	require.Eventually(t, func() bool {
		var text string
		testutil.OnUiThread(t, app, func() { text = page.poolDevices.GetLayout().GetText(true) })
		return strings.Contains(text, "NAME")
	}, 5*time.Second, 20*time.Millisecond, "the devices of the selected pool are shown")

	testutil.OnUiThread(t, app, func() {
		selected := page.poolBrowser.GetSelectedPool()
		require.NotNil(t, selected)
		assert.Equal(t, selected.Name, page.poolStatus.GetPool())
		assert.Contains(t, page.poolDevices.GetLayout().GetText(true), selected.Name)
	})
}

// The pools are reloaded when the ZFS event watcher reports a change of their state, or of the datasets (e.g. F5).
func TestPoolPage_ReloadsOnChanges(t *testing.T) {
	app, page := newPoolPageApp(t, 120, 40)
	// each load ends with a selection (with pools) or an error message (without, see testSystem)
	var loads atomic.Int32
	page.poolBrowser.Events.Subscribe(func(event pool_browser.Event) {
		switch event.(type) {
		case pool_browser.SelectedPoolChangedEvent, pool_browser.PoolBrowserStatusEvent:
			loads.Add(1)
		}
	})

	testutil.OnUiThread(t, app, func() { page.Init() })
	require.Eventually(t, func() bool { return loads.Load() == 1 }, 5*time.Second, 10*time.Millisecond)

	zfs.PoolsChanged.Emit(struct{}{})
	require.Eventually(t, func() bool { return loads.Load() == 2 }, 5*time.Second, 10*time.Millisecond)

	zfs.DatasetsLoaded.Emit(struct{}{})
	require.Eventually(t, func() bool { return loads.Load() == 3 }, 5*time.Second, 10*time.Millisecond)
}
