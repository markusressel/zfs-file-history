package ui

import (
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"
	"zfs-file-history/internal/testutil"
	"zfs-file-history/internal/ui/pool_browser"
	"zfs-file-history/internal/ui/status_message"
	"zfs-file-history/internal/zfs"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newPoolPageApp(t *testing.T, width int, height int) (*tview.Application, *PoolPage) {
	app := tview.NewApplication()
	screen := tcell.NewSimulationScreen("UTF-8")
	app.SetScreen(screen)
	screen.SetSize(width, height)
	page := NewPoolPage(app, status_message.NewCenter(app))
	app.SetRoot(page.layout, true)
	go func() { _ = app.Run() }()
	t.Cleanup(app.Stop)
	return app, page
}

// the pools, above the status of the selected one, above its devices
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

// Selecting a pool shows its status and its devices. With ZFS, this uses the pools of the system; without it,
// loading the pools fails, which is shown as a message.
func TestPoolPage_ShowsTheSelectedPool(t *testing.T) {
	app, page := newPoolPageApp(t, 160, 50)
	pools, poolsErr := zfs.ListPools()

	testutil.OnUiThread(t, app, func() { page.Init() })

	if poolsErr != nil || len(pools) == 0 {
		require.Eventually(t, func() bool {
			loading := true
			testutil.OnUiThread(t, app, func() { loading = page.poolBrowser.IsLoading() })
			return !loading
		}, 5*time.Second, 20*time.Millisecond)
		t.Skip("no pools on this system")
	}

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
	// each load ends with a selection (with ZFS) or an error message (without)
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
