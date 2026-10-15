package pool_activity

import (
	"errors"
	"sync"
	"testing"
	"time"
	"zfs-file-history/internal/testutil"
	"zfs-file-history/internal/ui/theme"
	"zfs-file-history/internal/zfs"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFormatAlerts(t *testing.T) {
	lines := formatAlerts([]zfs.PoolAlert{
		{Pool: "rpool", Severity: zfs.PoolAlertError, Message: "18 data errors in files or metadata"},
		{Pool: "tank", Severity: zfs.PoolAlertWarning, Message: "Pool is DEGRADED"},
		{Pool: "[red]x", Severity: zfs.PoolAlertWarning, Message: "Device errors: [sda] 1 read"},
	}, 80)

	require.Len(t, lines, 3)
	assert.Equal(t, " Error    rpool   18 data errors in files or metadata", testutil.StripTags(lines[0]))
	assert.Equal(t, " Warning  tank    Pool is DEGRADED", testutil.StripTags(lines[1]))
	// escaped
	assert.Equal(t, " Warning  [red]x  Device errors: [sda] 1 read", testutil.StripTags(lines[2]))
}

func TestFormatAlerts_Narrow(t *testing.T) {
	// " Error  rpool  " leaves 10 columns for the message
	lines := formatAlerts([]zfs.PoolAlert{
		{Pool: "rpool", Severity: zfs.PoolAlertError, Message: "Device errors: sda 1 read; sdb 2 read"},
		{Pool: "rpool", Severity: zfs.PoolAlertError, Message: "Fits fine"},
	}, 25)

	assert.Equal(t, " Error  rpool  Device er…", testutil.StripTags(lines[0]))
	assert.Equal(t, " Error  rpool  Fits fine", testutil.StripTags(lines[1]))
}

func TestTruncate(t *testing.T) {
	assert.Equal(t, "abc", truncate("abc", 3))
	assert.Equal(t, "ab…", truncate("abcd", 3))
	assert.Equal(t, "…", truncate("abcd", 1))
	assert.Equal(t, "", truncate("abcd", 0))
	assert.Equal(t, "", truncate("abcd", -2))
}

func TestSeverityColor(t *testing.T) {
	assert.Equal(t, theme.Colors.Alert.Error, severityColor(zfs.PoolAlertError))
	assert.Equal(t, theme.Colors.Alert.Warning, severityColor(zfs.PoolAlertWarning))
}

// withDataErrors returns the status of a pool with permanent data errors.
func withDataErrors(name string) *zfs.PoolStatus {
	return &zfs.PoolStatus{Name: name, State: zfs.PoolStateOnline, ScanProgress: -1, Errors: "18 data errors, use '-v' for a list"}
}

// setupAlertTest runs an application showing an alert bar in a layout below a line, like on the pages. The heights
// reported by the bar are recorded, and applied to the layout.
func setupAlertTest(t *testing.T, statuses []*zfs.PoolStatus) (*tview.Application, *Monitor, *AlertBar, *fakeStatuses, func() []int) {
	return setupAlertTestWith(t, statuses, noDataErrors)
}

// noDataErrors lists no damaged files, so the tests do not run zpool.
func noDataErrors() (*zfs.DataErrorIndex, error) {
	return nil, nil
}

// setupAlertTestWith is setupAlertTest with the given function that lists the damaged files.
func setupAlertTestWith(t *testing.T, statuses []*zfs.PoolStatus, listDataErrors func() (*zfs.DataErrorIndex, error)) (*tview.Application, *Monitor, *AlertBar, *fakeStatuses, func() []int) {
	fake := &fakeStatuses{statuses: statuses}
	originalLoad, originalActive, originalIdle, originalDataErrors := loadPoolStatuses, activeRefreshInterval, idleRefreshInterval, loadDataErrors
	loadPoolStatuses = fake.load
	loadDataErrors = listDataErrors
	activeRefreshInterval = 50 * time.Millisecond
	idleRefreshInterval = time.Hour
	originalDataErrorsInterval := dataErrorsRefreshInterval
	dataErrorsRefreshInterval = 0
	t.Cleanup(func() {
		dataErrorsRefreshInterval = originalDataErrorsInterval
		loadPoolStatuses, activeRefreshInterval, idleRefreshInterval, loadDataErrors = originalLoad, originalActive, originalIdle, originalDataErrors
	})

	app, _ := testutil.NewApp(testutil.WithSize(80, 10))

	var mu sync.Mutex
	var heights []int
	monitor := NewMonitor(app)
	bar := NewAlertBar(monitor)
	layout := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(tview.NewTextView().SetText("header"), 1, 0, false).
		AddItem(bar, 0, 0, false).
		AddItem(tview.NewBox(), 0, 1, true)
	bar.SetOnHeightChanged(func(height int) {
		mu.Lock()
		heights = append(heights, height)
		mu.Unlock()
		layout.ResizeItem(bar, height, 0)
	})
	app.SetRoot(layout, true)
	monitor.Refresh()
	testutil.RunApp(t, app)

	recorded := func() []int {
		mu.Lock()
		defer mu.Unlock()
		return append([]int{}, heights...)
	}
	return app, monitor, bar, fake, recorded
}

func TestAlertBar_ShowsProblems(t *testing.T) {
	degraded := idle("tank")
	degraded.State = zfs.PoolStateDegraded
	app, monitor, bar, _, heights := setupAlertTest(t, []*zfs.PoolStatus{idle("bpool"), degraded, withDataErrors("rpool")})

	require.Eventually(t, func() bool { return len(heights()) == 1 }, 2*time.Second, 10*time.Millisecond)
	// two lines, and the border
	assert.Equal(t, []int{4}, heights())
	testutil.OnUiThread(t, app, func() {
		// the most severe first, also across pools
		assert.Equal(t, []zfs.PoolAlert{
			{Pool: "rpool", Severity: zfs.PoolAlertError, Kind: zfs.PoolAlertDataErrors, Message: "18 data errors in files or metadata"},
			{Pool: "tank", Severity: zfs.PoolAlertWarning, Kind: zfs.PoolAlertState, Message: "Pool is DEGRADED"},
		}, monitor.Alerts())
		assert.Empty(t, monitor.Activities())
		_, y, _, height := bar.GetRect()
		assert.Equal(t, 1, y, "below the header")
		assert.Equal(t, 4, height)
	})
	// drawn with the border of the most severe alert
	testutil.OnUiThread(t, app, func() {})
	testutil.OnUiThread(t, app, func() { assert.Equal(t, theme.Colors.Alert.Error, bar.GetBorderColor()) })
}

func TestAlertBar_ComesUpAndGoesAway(t *testing.T) {
	app, monitor, bar, fake, heights := setupAlertTest(t, []*zfs.PoolStatus{idle("rpool")})
	require.Eventually(t, func() bool { return fake.getLoads() == 1 }, 2*time.Second, 10*time.Millisecond)
	testutil.OnUiThread(t, app, func() { assert.Equal(t, 0, bar.Height(), "healthy: hidden") })

	// e.g. a scrub found data errors, reported by the event watcher
	fake.set([]*zfs.PoolStatus{withDataErrors("rpool")}, nil)
	testutil.OnUiThread(t, app, monitor.Refresh)
	require.Eventually(t, func() bool { return len(heights()) == 1 }, 2*time.Second, 10*time.Millisecond)
	assert.Equal(t, []int{3}, heights())

	// e.g. cleared
	fake.set([]*zfs.PoolStatus{idle("rpool")}, nil)
	testutil.OnUiThread(t, app, monitor.Refresh)
	require.Eventually(t, func() bool { return len(heights()) == 2 }, 2*time.Second, 10*time.Millisecond)
	assert.Equal(t, []int{3, 0}, heights())
	testutil.OnUiThread(t, app, func() { assert.Empty(t, monitor.Alerts()) })
}

func TestAlertBar_Click(t *testing.T) {
	app, _, bar, _, heights := setupAlertTest(t, []*zfs.PoolStatus{withDataErrors("rpool"), withDataErrors("tank")})
	require.Eventually(t, func() bool { return len(heights()) == 1 }, 2*time.Second, 10*time.Millisecond)
	// laid out with the new height
	testutil.OnUiThread(t, app, func() {})

	var clicked []string
	enabled := true
	testutil.OnUiThread(t, app, func() {
		bar.SetOnClick(func(alert zfs.PoolAlert) { clicked = append(clicked, alert.Pool) })
		bar.SetEnabledFunc(func() bool { return enabled })
		// the regions are added when drawn
		app.ForceDraw()
	})

	// the bar starts at line 1, below the header, its lines below its border at line 2
	consumed := false
	click := func(action tview.MouseAction, x int, y int) {
		testutil.OnUiThread(t, app, func() {
			event := tcell.NewEventMouse(x, y, tcell.ButtonNone, tcell.ModNone)
			consumed, _ = bar.MouseHandler()(action, event, func(tview.Primitive) { panic("the bar does not take the focus") })
		})
	}
	click(tview.MouseMove, 5, 3)
	assert.True(t, consumed, "highlights the line")
	click(tview.MouseLeftClick, 5, 3)
	click(tview.MouseLeftClick, 5, 2)
	// the border is no line
	click(tview.MouseLeftClick, 5, 4)
	testutil.OnUiThread(t, app, func() { assert.Equal(t, []string{"tank", "rpool"}, clicked) })

	// e.g. a dialog in front
	testutil.OnUiThread(t, app, func() { enabled = false })
	click(tview.MouseLeftClick, 5, 2)
	assert.False(t, consumed)
	testutil.OnUiThread(t, app, func() { assert.Equal(t, []string{"tank", "rpool"}, clicked) })
}

func TestMonitor_KeepsAlertsOnError(t *testing.T) {
	app, monitor, _, fake, heights := setupAlertTest(t, []*zfs.PoolStatus{withDataErrors("rpool")})
	require.Eventually(t, func() bool { return len(heights()) == 1 }, 2*time.Second, 10*time.Millisecond)

	// Regression: a failed load (e.g. while a pool is suspended) hid the alerts, as if the problems were solved
	fake.set(nil, errors.New("zpool failed"))
	loads := fake.getLoads()
	testutil.OnUiThread(t, app, monitor.Refresh)
	require.Eventually(t, func() bool { return fake.getLoads() > loads }, 2*time.Second, 10*time.Millisecond)
	testutil.OnUiThread(t, app, func() { assert.Len(t, monitor.Alerts(), 1) })
	assert.Equal(t, []int{3}, heights())
}
