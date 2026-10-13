package pool_activity

import (
	"errors"
	"strings"
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

func TestFormatLabels(t *testing.T) {
	activities := []zfs.PoolActivity{
		{Pool: "tank", Kind: "resilver"},
		{Pool: "backup", Kind: "scrub"},
	}

	labels, width := formatLabels(activities)

	// " " + 8 + "  " + 6 + "  "
	assert.Equal(t, 19, width)
	require.Len(t, labels, 2)
	assert.Equal(t, " Resilver  tank    ", testutil.StripTags(labels[0]))
	assert.Equal(t, " Scrub     backup  ", testutil.StripTags(labels[1]))

	_, width = formatLabels(nil)
	assert.Equal(t, 5, width)
}

func TestFormatLabels_EscapesNames(t *testing.T) {
	labels, _ := formatLabels([]zfs.PoolActivity{{Pool: "[red]tank", Kind: "scrub"}})
	assert.Contains(t, testutil.StripTags(labels[0]), "[red]tank")
}

func TestBarText(t *testing.T) {
	activity := zfs.PoolActivity{Pool: "tank", Kind: "resilver", Progress: 31.77, Remaining: "02:10:31",
		Processed: "1.02T", Total: "3.21T", Rate: "294M/s"}
	tests := []struct {
		width    int
		expected string
	}{
		{100, " 31.8%  1.02T / 3.21T  294M/s  02:10:31 to go"},
		// too narrow: the amount is left out first, then the rate, then the time left
		{40, " 31.8%  294M/s  02:10:31 to go"},
		{25, " 31.8%  02:10:31 to go"},
		{10, " 31.8%"},
		{3, " 31.8%"},
	}
	for _, test := range tests {
		t.Run(test.expected, func(t *testing.T) {
			assert.Equal(t, test.expected, barText(activity, test.width))
		})
	}
}

func TestBarText_PartsNotKnown(t *testing.T) {
	// e.g. a scrub that just started
	assert.Equal(t, " 0.0%", barText(zfs.PoolActivity{Kind: "scrub"}, 80))
	assert.Equal(t, " 12.0%  2.00G / 4.00G", barText(zfs.PoolActivity{Kind: "trim", Progress: 12, Processed: "2.00G", Total: "4.00G"}, 80))
	// clamped
	assert.Equal(t, " 100.0%  1.00M/s", barText(zfs.PoolActivity{Progress: 150, Rate: "1.00M/s"}, 80))
}

// layoutActivities are a resilver with a time left, and a scrub without, on pools with names of different lengths.
var layoutActivities = []zfs.PoolActivity{
	{Pool: "tank", Kind: "resilver", Progress: 31.77, Remaining: "02:10:31", Processed: "1.02T", Total: "3.21T", Rate: "294M/s"},
	{Pool: "backup", Kind: "scrub", Progress: 10, Processed: "200G", Total: "2.00T", Rate: "1.10G/s"},
}

func TestLayoutLines(t *testing.T) {
	// label 19, time left "  02:10:31 to go" 16, padding 1: 44 for the bars
	lines := layoutLines(layoutActivities, 80)

	require.Len(t, lines, 2)
	for _, line := range lines {
		assert.Equal(t, 19, line.barStart, "the bars start at the same column")
		assert.Equal(t, 44, line.barWidth, "and end at the same column")
	}
	assert.Equal(t, " 31.8%  1.02T / 3.21T  294M/s", lines[0].barText, "the time left is behind the bar")
	assert.Equal(t, "  02:10:31 to go", testutil.StripTags(lines[0].remaining))
	assert.Equal(t, " 10.0%  200G / 2.00T  1.10G/s", lines[1].barText)
	assert.Empty(t, lines[1].remaining)
}

func TestLayoutLines_Wide(t *testing.T) {
	// the bars do not get wider than maxBarWidth, the rest of the line stays empty
	for _, line := range layoutLines(layoutActivities, 300) {
		assert.Equal(t, maxBarWidth, line.barWidth)
	}
}

func TestLayoutLines_Narrow(t *testing.T) {
	// 19 + 16 + 1 leave 14 for the bars, less than minBarWidthWithRemaining: the time left goes into the bars
	lines := layoutLines(layoutActivities, 50)

	assert.Equal(t, 30, lines[0].barWidth)
	assert.Empty(t, lines[0].remaining)
	assert.Equal(t, " 31.8%  294M/s  02:10:31 to go", lines[0].barText)
}

func TestLayoutLines_NoTimeLeft(t *testing.T) {
	// no column for it: label " Scrub  backup  " 16, padding 1
	lines := layoutLines(layoutActivities[1:], 40)

	assert.Equal(t, 40-16-1, lines[0].barWidth)
	assert.Empty(t, lines[0].remaining)
}

func TestFilledCells(t *testing.T) {
	assert.Equal(t, 0, filledCells(0, 10))
	assert.Equal(t, 5, filledCells(50, 10))
	assert.Equal(t, 9, filledCells(99.9, 10))
	assert.Equal(t, 10, filledCells(100, 10))
	assert.Equal(t, 0, filledCells(-5, 10))
	assert.Equal(t, 10, filledCells(150, 10))
}

func TestDrawProgressBar(t *testing.T) {
	screen := tcell.NewSimulationScreen("UTF-8")
	require.NoError(t, screen.Init())
	screen.SetSize(20, 1)

	drawProgressBar(screen, 2, 0, 10, 50, " 50.0%  fast")
	screen.Show()

	cells, width, _ := screen.GetContents()
	var text strings.Builder
	for x := 0; x < width; x++ {
		if runes := cells[x].Runes; len(runes) > 0 {
			text.WriteRune(runes[0])
		} else {
			text.WriteRune(' ')
		}
	}
	// cut off at the end of the bar
	assert.Equal(t, "   50.0%  fa        ", text.String())

	colors := theme.Colors.Activity
	for x := 2; x < 12; x++ {
		foreground, background, _ := cells[x].Style.Decompose()
		if x < 7 {
			assert.Equal(t, colors.BarFilled, background, "filled at %d", x)
			assert.Equal(t, colors.BarFilledText, foreground, "readable on the filled part at %d", x)
		} else {
			assert.Equal(t, colors.BarTrack, background, "not filled at %d", x)
			assert.Equal(t, colors.BarTrackText, foreground, "readable on the track at %d", x)
		}
	}
	_, background, _ := cells[12].Style.Decompose()
	assert.NotEqual(t, colors.BarTrack, background, "nothing drawn behind the bar")
}

func TestDrawProgressBar_NoWidth(t *testing.T) {
	screen := tcell.NewSimulationScreen("UTF-8")
	require.NoError(t, screen.Init())
	screen.SetSize(5, 1)

	// e.g. the labels take the whole width
	drawProgressBar(screen, 0, 0, 0, 50, " 50.0%")
	drawProgressBar(screen, 0, 0, -3, 50, " 50.0%")
	screen.Show()

	cells, _, _ := screen.GetContents()
	_, background, _ := cells[0].Style.Decompose()
	assert.NotEqual(t, theme.Colors.Activity.BarFilled, background)
}

func TestKindTitle(t *testing.T) {
	tests := map[string]string{
		"scrub":    "Scrub",
		"resilver": "Resilver",
		"trim":     "TRIM",
		"remove":   "Device removal",
		"expand":   "RAIDZ expansion",
		"other":    "Other",
		"":         "",
	}
	for kind, expected := range tests {
		assert.Equal(t, expected, kindTitle(kind), kind)
	}
}

// fakeStatuses returns the statuses set with set, counting the loads.
type fakeStatuses struct {
	mu       sync.Mutex
	loads    int
	statuses []*zfs.PoolStatus
	err      error
}

func (f *fakeStatuses) load() ([]*zfs.PoolStatus, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.loads++
	return f.statuses, f.err
}

func (f *fakeStatuses) set(statuses []*zfs.PoolStatus, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.statuses = statuses
	f.err = err
}

func (f *fakeStatuses) getLoads() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.loads
}

// scrubbing returns the status of a pool running a scrub.
func scrubbing(name string, progress float64) *zfs.PoolStatus {
	return &zfs.PoolStatus{Name: name, State: zfs.PoolStateOnline, Scan: "scrub in progress since Tue Oct 13 10:00:01 2026", ScanProgress: progress}
}

// idle returns the status of a pool on which nothing runs.
func idle(name string) *zfs.PoolStatus {
	return &zfs.PoolStatus{Name: name, State: zfs.PoolStateOnline, ScanProgress: -1}
}

// setupTest runs an application showing a bar in a layout below a line, like on the pages. The heights reported by
// the bar are recorded, and applied to the layout.
func setupTest(t *testing.T, statuses []*zfs.PoolStatus) (*tview.Application, *Monitor, *Bar, *fakeStatuses, func() []int) {
	fake := &fakeStatuses{statuses: statuses}
	originalLoad, originalActive, originalIdle := loadPoolStatuses, activeRefreshInterval, idleRefreshInterval
	loadPoolStatuses = fake.load
	activeRefreshInterval = 50 * time.Millisecond
	idleRefreshInterval = time.Hour
	t.Cleanup(func() {
		loadPoolStatuses, activeRefreshInterval, idleRefreshInterval = originalLoad, originalActive, originalIdle
	})

	app := tview.NewApplication()
	screen := tcell.NewSimulationScreen("UTF-8")
	app.SetScreen(screen)
	app.EnableMouse(true)
	screen.SetSize(80, 10)

	var mu sync.Mutex
	var heights []int
	monitor := NewMonitor(app)
	bar := NewBar(monitor)
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
	go func() { _ = app.Run() }()
	t.Cleanup(app.Stop)

	recorded := func() []int {
		mu.Lock()
		defer mu.Unlock()
		return append([]int{}, heights...)
	}
	return app, monitor, bar, fake, recorded
}

func TestBar_ShowsRunningOperations(t *testing.T) {
	app, monitor, bar, _, heights := setupTest(t, []*zfs.PoolStatus{idle("rpool"), scrubbing("tank", 42)})

	require.Eventually(t, func() bool { return len(heights()) == 1 }, 2*time.Second, 10*time.Millisecond)
	assert.Equal(t, []int{1}, heights())
	testutil.OnUiThread(t, app, func() {
		assert.Equal(t, []zfs.PoolActivity{{Pool: "tank", Kind: "scrub", Progress: 42}}, monitor.Activities())
		assert.Equal(t, 1, bar.Height())
		_, y, _, height := bar.GetRect()
		assert.Equal(t, 1, y, "below the header")
		assert.Equal(t, 1, height)
	})
}

func TestBar_HiddenWhileNothingRuns(t *testing.T) {
	app, monitor, bar, fake, heights := setupTest(t, []*zfs.PoolStatus{idle("rpool")})

	require.Eventually(t, func() bool { return fake.getLoads() == 1 }, 2*time.Second, 10*time.Millisecond)
	testutil.OnUiThread(t, app, func() {
		assert.Empty(t, monitor.Activities())
		assert.Equal(t, 0, bar.Height())
	})
	assert.Empty(t, heights(), "the height did not change")
}

func TestMonitor_RefreshesWhileRunning(t *testing.T) {
	app, monitor, _, fake, heights := setupTest(t, []*zfs.PoolStatus{scrubbing("tank", 10), scrubbing("backup", 20)})

	require.Eventually(t, func() bool { return len(heights()) == 1 }, 2*time.Second, 10*time.Millisecond)
	assert.Equal(t, []int{2}, heights())

	// reloaded after activeRefreshInterval: the progress is updated, the height only changes once one finished
	fake.set([]*zfs.PoolStatus{scrubbing("tank", 60), idle("backup")}, nil)
	require.Eventually(t, func() bool { return len(heights()) == 2 }, 2*time.Second, 10*time.Millisecond)
	assert.Equal(t, []int{2, 1}, heights())
	testutil.OnUiThread(t, app, func() {
		assert.Equal(t, []zfs.PoolActivity{{Pool: "tank", Kind: "scrub", Progress: 60}}, monitor.Activities())
	})

	// all done: hidden, and no more reloads (until idleRefreshInterval)
	fake.set([]*zfs.PoolStatus{idle("tank"), idle("backup")}, nil)
	require.Eventually(t, func() bool { return len(heights()) == 3 }, 2*time.Second, 10*time.Millisecond)
	assert.Equal(t, []int{2, 1, 0}, heights())
	loads := fake.getLoads()
	time.Sleep(5 * activeRefreshInterval)
	assert.Equal(t, loads, fake.getLoads())
}

func TestMonitor_Error(t *testing.T) {
	app, monitor, _, fake, heights := setupTest(t, []*zfs.PoolStatus{scrubbing("tank", 10)})
	require.Eventually(t, func() bool { return len(heights()) == 1 }, 2*time.Second, 10*time.Millisecond)

	// e.g. zpool is gone: nothing is shown
	fake.set(nil, errors.New("zpool failed"))
	require.Eventually(t, func() bool { return len(heights()) == 2 }, 2*time.Second, 10*time.Millisecond)
	assert.Equal(t, []int{1, 0}, heights())
	testutil.OnUiThread(t, app, func() { assert.Empty(t, monitor.Activities()) })
}

func TestMonitor_Refresh(t *testing.T) {
	app, monitor, _, fake, heights := setupTest(t, []*zfs.PoolStatus{idle("tank")})
	require.Eventually(t, func() bool { return fake.getLoads() == 1 }, 2*time.Second, 10*time.Millisecond)

	// e.g. a scrub was started: not noticed before idleRefreshInterval
	fake.set([]*zfs.PoolStatus{scrubbing("tank", 0)}, nil)
	require.Never(t, func() bool { return len(heights()) > 0 }, 200*time.Millisecond, 10*time.Millisecond)

	// unless reported by the event watcher
	testutil.OnUiThread(t, app, monitor.Refresh)
	require.Eventually(t, func() bool { return len(heights()) == 1 }, 2*time.Second, 10*time.Millisecond)
}

// mouse sends a mouse event at the given position to the bar, on the UI thread, and returns whether it consumed it.
func mouse(t *testing.T, app *tview.Application, bar *Bar, action tview.MouseAction, x int, y int) (consumed bool) {
	testutil.OnUiThread(t, app, func() {
		event := tcell.NewEventMouse(x, y, tcell.ButtonNone, tcell.ModNone)
		consumed, _ = bar.MouseHandler()(action, event, func(tview.Primitive) { panic("the bar does not take the focus") })
	})
	return consumed
}

func TestBar_Click(t *testing.T) {
	app, _, bar, _, heights := setupTest(t, []*zfs.PoolStatus{scrubbing("tank", 10), scrubbing("backup", 20)})
	require.Eventually(t, func() bool { return len(heights()) == 1 }, 2*time.Second, 10*time.Millisecond)
	// laid out with the new height
	testutil.OnUiThread(t, app, func() {})

	var clicked []string
	enabled := true
	testutil.OnUiThread(t, app, func() {
		bar.SetOnClick(func(activity zfs.PoolActivity) { clicked = append(clicked, activity.Pool) })
		bar.SetEnabledFunc(func() bool { return enabled })
	})

	// the bar starts at line 1, below the header
	assert.True(t, mouse(t, app, bar, tview.MouseMove, 5, 2), "highlights the line")
	testutil.OnUiThread(t, app, func() { assert.Equal(t, 1, bar.hovered) })
	mouse(t, app, bar, tview.MouseLeftClick, 5, 2)
	mouse(t, app, bar, tview.MouseLeftClick, 70, 1)
	testutil.OnUiThread(t, app, func() { assert.Equal(t, []string{"backup", "tank"}, clicked) })

	// outside: the highlight goes away
	assert.True(t, mouse(t, app, bar, tview.MouseMove, 5, 5))
	testutil.OnUiThread(t, app, func() { assert.Equal(t, noLine, bar.hovered) })

	// e.g. a dialog in front
	testutil.OnUiThread(t, app, func() { enabled = false })
	assert.False(t, mouse(t, app, bar, tview.MouseLeftClick, 5, 1))
	testutil.OnUiThread(t, app, func() { assert.Equal(t, []string{"backup", "tank"}, clicked) })
}
