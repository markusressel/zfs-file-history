package dialog

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"
	"zfs-file-history/internal/testutil"
	"zfs-file-history/internal/ui/scrollbar"
	"zfs-file-history/internal/ui/table"
	"zfs-file-history/internal/zfs"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// historyAt returns the time of a record of the test history, on the 2nd of January 2024 at the hour.
func historyAt(hour int) time.Time {
	return time.Date(2024, 1, 2, hour, 0, 0, 0, time.Local)
}

// testHistory is the history of two pools, oldest first, like zfs.PoolHistory returns it. Grouped: a command with its
// event, a command, a scrub.
var testHistory = []zfs.PoolHistoryEntry{
	{Pool: "tank", Time: historyAt(1), Internal: true, Event: "snapshot", Datasets: []string{"tank/data/home@daily"}, Txg: 7, Host: "host"},
	{Pool: "tank", Time: historyAt(1), Event: "zfs snapshot", Datasets: []string{"tank/data@daily"}, Text: "zfs snapshot -r tank/data@daily", User: "root", Host: "host"},
	{Pool: "other", Time: historyAt(2), Event: "zpool scrub", Text: "zpool scrub other", User: "root", Host: "host"},
	{Pool: "tank", Time: historyAt(3), Event: "zfs destroy", Datasets: []string{"tank/data@old"}, Text: "zfs destroy tank/data@old", User: "alice", Host: "host"},
	{Pool: "tank", Time: historyAt(4), Event: "zpool scrub", Text: "zpool scrub tank", User: "root", Host: "host"},
	{Pool: "tank", Time: historyAt(5), Internal: true, Event: "scan done", Text: "errors=0", Txg: 9, Host: "host"},
}

type historyDialogTest struct {
	t      *testing.T
	app    *tview.Application
	screen tcell.SimulationScreen
	pages  *tview.Pages
	dialog *PoolHistoryDialog
	cache  *PoolHistoryCache
}

// historyCacheOf returns a cache that reads the records of the pool from the ones load returns (of all pools).
func historyCacheOf(load func() ([]zfs.PoolHistoryEntry, error)) *PoolHistoryCache {
	return NewPoolHistoryCache(func(ctx context.Context, pool string, progress func(zfs.PoolHistoryProgress)) ([]zfs.PoolHistoryEntry, error) {
		entries, err := load()
		var own []zfs.PoolHistoryEntry
		for _, entry := range entries {
			if entry.Pool == pool {
				own = append(own, entry)
			}
		}
		return own, err
	})
}

// newHistoryDialogTest shows the history of the pool or dataset, read by load.
func newHistoryDialogTest(t *testing.T, pool string, dataset string, load func() ([]zfs.PoolHistoryEntry, error)) *historyDialogTest {
	return newHistoryDialogTestWithCache(t, pool, dataset, historyCacheOf(load))
}

// newHistoryDialogTestWithCache shows the history of the pool or dataset from the cache.
func newHistoryDialogTestWithCache(t *testing.T, pool string, dataset string, cache *PoolHistoryCache) *historyDialogTest {
	dt := &historyDialogTest{t: t, cache: cache}
	dt.app, dt.screen = testutil.NewApp(testutil.WithSize(140, 40))
	dt.pages = tview.NewPages().AddPage("background", tview.NewBox(), true, true)
	testutil.StartApp(t, dt.app, dt.pages)
	testutil.OnUiThread(t, dt.app, func() {
		dt.dialog = NewPoolHistoryDialog(dt.app, pool, dataset, cache)
		ShowDialogOnPages(dt.app, dt.pages, dt.dialog, nil)
	})
	return dt
}

func (dt *historyDialogTest) screenText() string {
	var text strings.Builder
	testutil.OnUiThread(dt.t, dt.app, func() {
		dt.app.ForceDraw()
		cells, width, height := dt.screen.GetContents()
		for y := 0; y < height; y++ {
			for x := 0; x < width; x++ {
				if runes := cells[y*width+x].Runes; len(runes) > 0 {
					text.WriteRune(runes[0])
				}
			}
			text.WriteRune('\n')
		}
	})
	return text.String()
}

func (dt *historyDialogTest) waitForText(text string) {
	require.Eventually(dt.t, func() bool { return strings.Contains(dt.screenText(), text) }, 3*time.Second, 10*time.Millisecond, "text %q", text)
}

// shownEvents returns the rows shown, from the top: the event of a record (indented below its group), the title of a
// group (with ▸ if it is collapsed).
func (dt *historyDialogTest) shownEvents() []string {
	var events []string
	testutil.OnUiThread(dt.t, dt.app, func() {
		for _, row := range dt.dialog.content.rows {
			events = append(events, describeHistoryRow(row))
		}
	})
	return events
}

// selected returns the selected row, like shownEvents.
func (dt *historyDialogTest) selected() string {
	var selected string
	testutil.OnUiThread(dt.t, dt.app, func() { selected = describeHistoryRow(dt.dialog.selectedRow()) })
	return selected
}

func describeHistoryRow(row poolHistoryRow) string {
	switch {
	case row.entry == nil && row.group == nil:
		return "header"
	case row.entry == nil && row.expanded:
		return poolHistoryGroupTitle(row.group)
	case row.entry == nil:
		return poolHistoryGroupTitle(row.group) + " ▸"
	case row.child:
		return "  " + row.entry.Event
	}
	return row.entry.Event
}

// waitForRows waits until the rows are shown, see shownEvents.
func (dt *historyDialogTest) waitForRows(rows ...string) {
	dt.t.Helper()
	require.EventuallyWithT(dt.t, func(c *assert.CollectT) { assert.Equal(c, rows, dt.shownEvents()) }, 3*time.Second, 10*time.Millisecond)
}

// waitForSelected waits until the row is selected, see shownEvents.
func (dt *historyDialogTest) waitForSelected(row string) {
	dt.t.Helper()
	require.EventuallyWithT(dt.t, func(c *assert.CollectT) { assert.Equal(c, row, dt.selected()) }, 3*time.Second, 10*time.Millisecond)
}

func (dt *historyDialogTest) keys(keys string) {
	for _, r := range keys {
		dt.key(tcell.KeyRune, r)
	}
}

func (dt *historyDialogTest) key(key tcell.Key, r rune) {
	dt.screen.InjectKey(key, r, tcell.ModNone)
}

func TestPoolHistoryDialog_Pool(t *testing.T) {
	dt := newHistoryDialogTest(t, "tank", "", func() ([]zfs.PoolHistoryEntry, error) { return testHistory, nil })

	dt.waitForText("History of tank")
	// grouped, newest first: a scrub from its start to its end, a command, a command with the event ZFS logged for it
	dt.waitForText("3 of 3 groups (5 records)")
	dt.waitForRows("Scrub ▸", "zfs destroy", "zfs snapshot ▸")
	dt.waitForText("Scrub ▸ +2")
	dt.waitForText("done in 1h 00m, no errors")
	dt.waitForText("2024-01-02 04:00:00 – 05:00:00, 2 records, root: Scrub, done in 1h 00m, no errors")
	dt.waitForText("-r tank/data@daily · 1 event")

	// each record, in the order they were logged; the events logged by ZFS only on request
	dt.key(tcell.KeyDown, 0)
	dt.key(tcell.KeyRune, 'b')
	dt.waitForText("3 of 5 records")
	dt.waitForRows("zpool scrub", "zfs destroy", "zfs snapshot")
	// the arguments of a command besides its event, the whole record of the selected one below the list
	dt.waitForText("2024-01-02 03:00:00, Command, alice on host: zfs destroy tank/data@old")

	dt.key(tcell.KeyRune, 'i')
	dt.waitForText("5 of 5 records")
	dt.waitForRows("scan done", "zpool scrub", "zfs destroy", "zfs snapshot", "snapshot")
	// the selected record stays selected
	dt.waitForText("Command, alice on host: zfs destroy tank/data@old")

	dt.key(tcell.KeyDown, 0)
	dt.key(tcell.KeyDown, 0)
	dt.waitForText("Logged by ZFS (txg 7), host: snapshot tank/data/home@daily")
}

// Enter, + and - expand and collapse the groups like the tree of the datasets, * all of them
func TestPoolHistoryDialog_Expand(t *testing.T) {
	dt := newHistoryDialogTest(t, "tank", "", func() ([]zfs.PoolHistoryEntry, error) { return testHistory, nil })
	dt.waitForText("3 of 3 groups")

	dt.key(tcell.KeyEnter, 0)
	dt.waitForRows("Scrub", "  scan done", "  zpool scrub", "zfs destroy", "zfs snapshot ▸")
	dt.waitForText("├─ scan done")
	dt.waitForText("└─ zpool scrub")
	dt.key(tcell.KeyRune, '+')
	dt.waitForSelected("  scan done")
	dt.key(tcell.KeyRune, '-')
	dt.waitForSelected("Scrub")
	dt.key(tcell.KeyRune, '-')
	dt.waitForRows("Scrub ▸", "zfs destroy", "zfs snapshot ▸")
	dt.key(tcell.KeyRune, '+')
	dt.waitForSelected("Scrub")
	dt.key(tcell.KeyEnter, 0)
	dt.waitForSelected("Scrub ▸")

	dt.key(tcell.KeyRune, '*')
	dt.waitForRows("Scrub", "  scan done", "  zpool scrub", "zfs destroy", "zfs snapshot", "  zfs snapshot", "  snapshot")
	dt.key(tcell.KeyDown, 0)
	dt.key(tcell.KeyRune, '*')
	dt.waitForRows("Scrub ▸", "zfs destroy", "zfs snapshot ▸")
	dt.waitForSelected("Scrub ▸")
}

// b switches between the groups and each record, the selected record stays selected
func TestPoolHistoryDialog_SwitchView(t *testing.T) {
	history := append(slices.Clone(testHistory),
		zfs.PoolHistoryEntry{Pool: "tank", Time: historyAt(7), Event: "zfs rename", Text: "zfs rename tank/a tank/b", User: "root", Host: "host"})
	dt := newHistoryDialogTest(t, "tank", "", func() ([]zfs.PoolHistoryEntry, error) { return history, nil })
	dt.waitForText("4 of 4 groups")

	// a group selects its newest command (its newest record is an event, which is not shown)
	dt.key(tcell.KeyDown, 0)
	dt.waitForSelected("Scrub ▸")
	dt.key(tcell.KeyRune, 'b')
	dt.waitForText("4 of 6 records")
	dt.waitForSelected("zpool scrub")
	// a record selects its group, expanded
	dt.key(tcell.KeyRune, 'b')
	dt.waitForText("of 4 groups")
	dt.waitForSelected("  zpool scrub")
	dt.waitForRows("zfs rename", "Scrub", "  scan done", "  zpool scrub", "zfs destroy", "zfs snapshot ▸")

	// an event shows the events logged by ZFS
	dt.key(tcell.KeyUp, 0)
	dt.key(tcell.KeyRune, 'b')
	dt.waitForText("6 of 6 records")
	dt.waitForSelected("scan done")
}

func TestPoolHistoryDialog_Filter(t *testing.T) {
	dt := newHistoryDialogTest(t, "tank", "", func() ([]zfs.PoolHistoryEntry, error) { return testHistory, nil })
	dt.waitForText("3 of 3 groups")

	// in the event, datasets, details or user, ignoring case
	dt.key(tcell.KeyRune, '/')
	dt.keys("ALICE")
	dt.waitForText("1 of 3 groups")
	dt.waitForRows("zfs destroy")
	// a group if its summary or one of its records matches
	for range "ALICE" {
		dt.key(tcell.KeyBackspace2, 0)
	}
	dt.keys("home")
	dt.waitForText("1 of 3 groups")
	dt.waitForRows("zfs snapshot ▸")
	for range "home" {
		dt.key(tcell.KeyBackspace2, 0)
	}
	dt.keys("no errors")
	dt.waitForText("1 of 3 groups")
	dt.waitForRows("Scrub ▸")

	// Esc clears the filter, the next one closes the dialog
	dt.key(tcell.KeyEscape, 0)
	dt.waitForText("3 of 3 groups")
	dt.key(tcell.KeyRune, 'q')
	require.Eventually(t, func() bool {
		shown := true
		testutil.OnUiThread(t, dt.app, func() { shown = dt.pages.HasPage(string(PoolHistoryDialogPage)) })
		return !shown
	}, 3*time.Second, 10*time.Millisecond)
}

// a dataset: its records, of its children and snapshots, with the events logged by ZFS, which name each dataset a
// recursive command changed
func TestPoolHistoryDialog_Dataset(t *testing.T) {
	dt := newHistoryDialogTest(t, "tank", "tank/data/home", func() ([]zfs.PoolHistoryEntry, error) { return testHistory, nil })

	dt.waitForText("History of tank/data/home")
	dt.waitForText("1 of 1 groups (1 records)")
	dt.waitForRows("snapshot")
}

func TestPoolHistoryDialog_Error(t *testing.T) {
	dt := newHistoryDialogTest(t, "tank", "", func() ([]zfs.PoolHistoryEntry, error) {
		return nil, errors.New("sudo: a password is required")
	})

	dt.waitForText("Could not read the history: sudo: a password is required")
}

func TestContainsFold(t *testing.T) {
	assert.True(t, containsFold("zfs Destroy tank/data@old", "destroy"))
	assert.True(t, containsFold("zfs destroy", "ZFS"))
	assert.False(t, containsFold("zfs", "zfs snapshot"))
	assert.True(t, containsFold("org:note=Größe", "GRÖ"))
	assert.False(t, containsFold("org:note=Grosse", "GRÖ"))
	assert.True(t, containsFold("anything", ""))
}

// Regression: the list was drawn empty while loading, so the table followed its end, and showed the oldest records
// while the newest one was selected.
func TestPoolHistoryDialog_StartsAtTheNewest(t *testing.T) {
	loaded := make(chan struct{})
	var many []zfs.PoolHistoryEntry
	for i := range 200 {
		// apart, so they are not grouped as a run
		many = append(many, zfs.PoolHistoryEntry{Pool: "tank", Time: historyAt(0).Add(time.Duration(i) * 2 * time.Minute), Event: "zfs snapshot"})
	}
	dt := newHistoryDialogTest(t, "tank", "", func() ([]zfs.PoolHistoryEntry, error) {
		<-loaded
		return many, nil
	})
	dt.waitForText("Reading the history (zpool history)…")
	close(loaded)
	dt.waitForText("200 of 200 groups")

	dt.waitForText("2024-01-02 06:38:00")
	offset := func() (row int) {
		testutil.OnUiThread(t, dt.app, func() { row, _ = dt.dialog.table.GetOffset() })
		return row
	}
	assert.Equal(t, 0, offset())

	// the same with a filter that matches nothing, then cleared
	dt.key(tcell.KeyRune, '/')
	dt.key(tcell.KeyRune, 'x')
	dt.waitForRows()
	dt.key(tcell.KeyEscape, 0)
	dt.waitForText("200 of 200 groups")
	dt.waitForText("2024-01-02 06:38:00")
	assert.Equal(t, 0, offset())
}

// a scrollbar right of the list while not all rows fit
func TestPoolHistoryDialog_Scrollbar(t *testing.T) {
	var many []zfs.PoolHistoryEntry
	for i := range 200 {
		many = append(many, zfs.PoolHistoryEntry{Pool: "tank", Time: historyAt(0).Add(time.Duration(i) * 2 * time.Minute), Event: "zfs snapshot"})
	}
	dt := newHistoryDialogTest(t, "tank", "", func() ([]zfs.PoolHistoryEntry, error) { return many, nil })
	dt.waitForText("200 of 200 groups")
	dt.waitForText(scrollbar.ScrollIndicatorBottom)

	dt.key(tcell.KeyRune, '/')
	dt.keys("x")
	dt.waitForRows()
	assert.NotContains(t, dt.screenText(), scrollbar.ScrollIndicatorBottom)
}

// long arguments of a command do not push the user out of the list
func TestPoolHistoryDialog_LongDetails(t *testing.T) {
	long := []zfs.PoolHistoryEntry{{Pool: "tank", Time: historyAt(1), Event: "zpool create",
		Text: "zpool create -o ashift=12 tank mirror " + strings.Repeat("/dev/disk/by-id/nvme-Some_Long_Device_Name-part3 ", 4),
		User: "alice", Host: "host"}}
	dt := newHistoryDialogTest(t, "tank", "", func() ([]zfs.PoolHistoryEntry, error) { return long, nil })
	dt.waitForText("1 of 1 groups")

	dt.waitForText("User")
	dt.waitForText("alice")
}

// Regression: a group expanded further down the list stayed selected, but was scrolled to the last line of the list,
// its records out of sight.
func TestPoolHistoryDialog_ExpandKeepsTheLine(t *testing.T) {
	var runs []zfs.PoolHistoryEntry
	for i := range 100 {
		// two commands of a user less than a minute apart are a run, the runs are apart
		start := historyAt(0).Add(time.Duration(i) * 10 * time.Minute)
		runs = append(runs,
			zfs.PoolHistoryEntry{Pool: "tank", Time: start, Event: "zfs snapshot", User: "root"},
			zfs.PoolHistoryEntry{Pool: "tank", Time: start.Add(30 * time.Second), Event: "zfs destroy", User: "root"})
	}
	dt := newHistoryDialogTest(t, "tank", "", func() ([]zfs.PoolHistoryEntry, error) { return runs, nil })
	dt.waitForText("100 of 100 groups")
	line := func() (line int) {
		dt.screenText()
		testutil.OnUiThread(t, dt.app, func() {
			row, _ := dt.dialog.table.GetSelection()
			offset, _ := dt.dialog.table.GetOffset()
			line = row - offset
		})
		return line
	}
	waitForSelectedRow := func(row int) {
		require.EventuallyWithT(t, func(c *assert.CollectT) {
			selected := 0
			testutil.OnUiThread(t, dt.app, func() { selected, _ = dt.dialog.table.GetSelection() })
			assert.Equal(c, row, selected)
		}, 3*time.Second, 10*time.Millisecond)
	}
	// down far enough to scroll, then up, so the selection is in the middle of the list
	for range 60 {
		dt.key(tcell.KeyDown, 0)
	}
	waitForSelectedRow(61)
	dt.screenText()
	for range 10 {
		dt.key(tcell.KeyUp, 0)
	}
	waitForSelectedRow(51)
	before := line()
	require.Greater(t, before, 1)

	dt.key(tcell.KeyEnter, 0)
	require.EventuallyWithT(t, func(c *assert.CollectT) { assert.Equal(c, 102, len(dt.shownEvents())) }, 3*time.Second, 10*time.Millisecond)
	assert.Equal(t, before, line(), "the group stays on its line")

	dt.key(tcell.KeyEnter, 0)
	require.EventuallyWithT(t, func(c *assert.CollectT) { assert.Equal(c, 100, len(dt.shownEvents())) }, 3*time.Second, 10*time.Millisecond)
	assert.Equal(t, before, line(), "also when collapsed")
}

// screenPosition returns where the text is on the screen, see screenText.
func (dt *historyDialogTest) screenPosition(text string) (x int, y int) {
	dt.t.Helper()
	for y, line := range strings.Split(dt.screenText(), "\n") {
		if index := strings.Index(line, text); index >= 0 {
			return len([]rune(line[:index])), y
		}
	}
	require.Fail(dt.t, "not on the screen", text)
	return 0, 0
}

// click clicks the text on the screen.
func (dt *historyDialogTest) click(text string) {
	x, y := dt.screenPosition(text)
	dt.screen.InjectMouse(x, y, tcell.Button1, tcell.ModNone)
	dt.screen.InjectMouse(x, y, tcell.ButtonNone, tcell.ModNone)
}

// the header row sorts like in the other tables: ← and → choose the column, Enter the direction; saved
func TestPoolHistoryDialog_Sort(t *testing.T) {
	store := useTestState(t)
	dt := newHistoryDialogTest(t, "tank", "", func() ([]zfs.PoolHistoryEntry, error) { return testHistory, nil })
	dt.waitForRows("Scrub ▸", "zfs destroy", "zfs snapshot ▸")
	dt.waitForText("Time ↓")

	dt.key(tcell.KeyUp, 0)
	dt.waitForText("←/→: sort column")
	dt.key(tcell.KeyRight, 0)
	dt.waitForRows("zfs snapshot ▸", "zfs destroy", "Scrub ▸")
	dt.waitForText("Event ↓")
	dt.key(tcell.KeyEnter, 0)
	dt.waitForRows("Scrub ▸", "zfs destroy", "zfs snapshot ▸")
	dt.waitForText("Event ↑")

	// by the time, the oldest first: also the records of a group
	dt.key(tcell.KeyLeft, 0)
	dt.waitForRows("zfs snapshot ▸", "zfs destroy", "Scrub ▸")
	dt.key(tcell.KeyDown, 0)
	dt.waitForSelected("zfs snapshot ▸")
	dt.key(tcell.KeyEnter, 0)
	dt.waitForRows("zfs snapshot", "  snapshot", "  zfs snapshot", "zfs destroy", "Scrub ▸")

	// the records one by one, in the same order
	dt.keys("bi")
	dt.waitForRows("snapshot", "zfs snapshot", "zfs destroy", "zpool scrub", "scan done")

	saved, ok := table.LoadColumnLayout(store, stateKeyPoolHistoryTable, poolHistoryColumns)
	require.True(t, ok)
	assert.Equal(t, poolHistoryColumnTime, saved.SortColumn)
	assert.False(t, saved.SortInverted)
	other := newHistoryDialogTest(t, "tank", "", func() ([]zfs.PoolHistoryEntry, error) { return testHistory, nil })
	other.waitForRows("zfs snapshot ▸", "zfs destroy", "Scrub ▸")
}

// the first click on the header row selects it, the next ones sort by the clicked column
func TestPoolHistoryDialog_SortByClick(t *testing.T) {
	useTestState(t)
	dt := newHistoryDialogTest(t, "tank", "", func() ([]zfs.PoolHistoryEntry, error) { return testHistory, nil })
	dt.waitForRows("Scrub ▸", "zfs destroy", "zfs snapshot ▸")

	dt.click("Event")
	dt.waitForText("←/→: sort column")
	dt.waitForRows("Scrub ▸", "zfs destroy", "zfs snapshot ▸")
	dt.click("Event")
	dt.waitForText("Event ↓")
	dt.waitForRows("zfs snapshot ▸", "zfs destroy", "Scrub ▸")
	dt.click("Event")
	dt.waitForText("Event ↑")
	dt.waitForRows("Scrub ▸", "zfs destroy", "zfs snapshot ▸")
}

// F2 chooses the columns, like in the other tables; saved
func TestPoolHistoryDialog_Columns(t *testing.T) {
	store := useTestState(t)
	dt := newHistoryDialogTest(t, "tank", "", func() ([]zfs.PoolHistoryEntry, error) { return testHistory, nil })
	dt.waitForRows("Scrub ▸", "zfs destroy", "zfs snapshot ▸")

	dt.key(tcell.KeyF2, 0)
	dt.waitForText("Configure History Columns")
	// removes the first column, the time: the rows are sorted by the next one
	dt.key(tcell.KeyEnter, 0)
	dt.key(tcell.KeyEscape, 0)
	require.Eventually(t, func() bool { return !strings.Contains(dt.screenText(), "Configure History Columns") }, 3*time.Second, 10*time.Millisecond)
	assert.NotContains(t, dt.screenText(), "Time")
	dt.waitForText("Event ↓")
	dt.waitForRows("zfs snapshot ▸", "zfs destroy", "Scrub ▸")

	// the keys reach the list again: its records one by one, by the event
	dt.keys("b")
	dt.waitForRows("zpool scrub", "zfs snapshot", "zfs destroy")

	saved, ok := table.LoadColumnLayout(store, stateKeyPoolHistoryTable, poolHistoryColumns)
	require.True(t, ok)
	assert.Equal(t, []*table.Column{poolHistoryColumnEvent, poolHistoryColumnDataset, poolHistoryColumnUser, poolHistoryColumnDetails}, saved.Columns)
}

// reopen closes the dialog and shows it again, with the same cache.
func (dt *historyDialogTest) reopen(pool string, dataset string) {
	dt.t.Helper()
	testutil.OnUiThread(dt.t, dt.app, func() { dt.dialog.Close() })
	require.Eventually(dt.t, func() bool {
		var name string
		testutil.OnUiThread(dt.t, dt.app, func() { name, _ = dt.pages.GetFrontPage() })
		return name == "background"
	}, 3*time.Second, 10*time.Millisecond)
	testutil.OnUiThread(dt.t, dt.app, func() {
		dt.dialog = NewPoolHistoryDialog(dt.app, pool, dataset, dt.cache)
		ShowDialogOnPages(dt.app, dt.pages, dt.dialog, nil)
	})
}

// Shown again, the history read before is shown at once while it is read again, with its progress; the history read
// then replaces it, with the same row selected and the same groups expanded.
func TestPoolHistoryDialog_ShownAgainFromTheCache(t *testing.T) {
	reads := 0
	release := make(chan struct{})
	progressed := make(chan struct{})
	history := slices.Clone(testHistory)
	cache := NewPoolHistoryCache(func(ctx context.Context, pool string, progress func(zfs.PoolHistoryProgress)) ([]zfs.PoolHistoryEntry, error) {
		reads++
		if reads > 1 {
			progress(zfs.PoolHistoryProgress{Bytes: 512, Records: 2})
			close(progressed)
			<-release
		}
		var own []zfs.PoolHistoryEntry
		for _, entry := range history {
			if entry.Pool == pool {
				own = append(own, entry)
			}
		}
		progress(zfs.PoolHistoryProgress{Bytes: 1024, Records: len(own)})
		return own, nil
	})
	dt := newHistoryDialogTestWithCache(t, "tank", "", cache)
	dt.waitForRows("Scrub ▸", "zfs destroy", "zfs snapshot ▸")
	dt.key(tcell.KeyEnter, 0)
	dt.key(tcell.KeyDown, 0)
	dt.key(tcell.KeyDown, 0)
	dt.key(tcell.KeyDown, 0)
	dt.waitForSelected("zfs destroy")
	dt.waitForRows("Scrub", "  scan done", "  zpool scrub", "zfs destroy", "zfs snapshot ▸")

	// a new record, read when shown again
	history = append(history, zfs.PoolHistoryEntry{Pool: "tank", Time: historyAt(6), Event: "zfs set", Text: "zfs set atime=off tank", User: "root", Host: "host"})
	dt.reopen("tank", "")
	<-progressed
	dt.waitForText("Reading the history again, this is the one from before… 50% · 2 records · 512 B")
	// the rows of before, at once: not the loading view
	assert.Equal(t, []string{"Scrub ▸", "zfs destroy", "zfs snapshot ▸"}, dt.shownEvents())

	// the selection is not kept across closing the dialog, but across a history read again
	dt.key(tcell.KeyEnter, 0)
	dt.key(tcell.KeyDown, 0)
	dt.key(tcell.KeyDown, 0)
	dt.key(tcell.KeyDown, 0)
	dt.waitForSelected("zfs destroy")
	close(release)
	dt.waitForRows("zfs set", "Scrub", "  scan done", "  zpool scrub", "zfs destroy", "zfs snapshot ▸")
	dt.waitForSelected("zfs destroy")
	dt.waitForText("4 of 4 groups (6 records)")
	assert.Equal(t, 2, reads)
}

// While the history is read the first time, the loading view tells how far.
func TestPoolHistoryDialog_Progress(t *testing.T) {
	release := make(chan struct{})
	cache := NewPoolHistoryCache(func(ctx context.Context, pool string, progress func(zfs.PoolHistoryProgress)) ([]zfs.PoolHistoryEntry, error) {
		progress(zfs.PoolHistoryProgress{Bytes: 3 << 20, Records: 12345})
		<-release
		return nil, nil
	})
	dt := newHistoryDialogTestWithCache(t, "tank", "", cache)
	t.Cleanup(func() { close(release) })

	dt.waitForText("Reading the history (zpool history)…")
	// below, so the line is not too long
	dt.waitForText("12,345 records · 3.0 MiB")
	text := dt.screenText()
	assert.Less(t, strings.Index(text, "Reading the history"), strings.Index(text, "12,345 records"))
	assert.NotContains(t, text, "Reading the history (zpool history)… 12,345")
}

// A dialog shown while the history is read does not read it a second time.
func TestPoolHistoryCache_OneReadAtATime(t *testing.T) {
	release := make(chan struct{})
	var reads atomic.Int32
	cache := NewPoolHistoryCache(func(ctx context.Context, pool string, progress func(zfs.PoolHistoryProgress)) ([]zfs.PoolHistoryEntry, error) {
		reads.Add(1)
		<-release
		return []zfs.PoolHistoryEntry{{Pool: pool, Event: "zpool create"}}, nil
	})
	states := make(chan PoolHistoryState, 10)
	first, stopFirst := cache.Watch("tank", func(state PoolHistoryState) { states <- state })
	second, stopSecond := cache.Watch("tank", func(PoolHistoryState) {})
	stopSecond()
	assert.True(t, first.Reading)
	assert.True(t, second.Reading)
	assert.Nil(t, second.History)

	close(release)
	state := <-states
	stopFirst()
	assert.False(t, state.Reading)
	require.NotNil(t, state.History)
	assert.Len(t, state.History.Entries, 1)
	assert.Equal(t, int32(1), reads.Load())

	// watched again: the history read before, and read again
	third, stopThird := cache.Watch("tank", func(PoolHistoryState) {})
	defer stopThird()
	assert.Same(t, state.History, third.History)
	assert.True(t, third.Reading)
}

// position returns the selected row and its line on the screen (below the header row).
func (dt *historyDialogTest) position() (row int, line int) {
	testutil.OnUiThread(dt.t, dt.app, func() {
		dt.app.ForceDraw()
		var offset int
		row, _ = dt.table().GetSelection()
		offset, _ = dt.table().GetOffset()
		line = row - offset
	})
	return row, line
}

func (dt *historyDialogTest) table() *poolHistoryTable {
	return dt.dialog.table
}

// spacedHistory returns n commands of the pool, two minutes apart (each a group of its own), oldest first.
func spacedHistory(pool string, n int) []zfs.PoolHistoryEntry {
	entries := make([]zfs.PoolHistoryEntry, n)
	for i := range entries {
		entries[i] = zfs.PoolHistoryEntry{Pool: pool, Time: historyAt(1).Add(time.Duration(2*i) * time.Minute),
			Event: "zfs set", Text: fmt.Sprintf("zfs set user:n=%d %s", i, pool), User: "root", Host: "host"}
	}
	return entries
}

// The events ZFS reports while the dialog is shown come in above. Scrolled down, the selected row stays where it is on
// the screen, so the list does not move under the reader; at the top, the new ones are shown.
func TestPoolHistoryDialog_RecordsComeIn(t *testing.T) {
	history := spacedHistory("tank", 100)
	cache := historyCacheOf(func() ([]zfs.PoolHistoryEntry, error) { return history, nil })
	cache.recordDelay = time.Millisecond
	dt := newHistoryDialogTestWithCache(t, "tank", "", cache)
	dt.waitForText("100 of 100 groups")
	newest := history[len(history)-1].Time
	record := func(minutes int) {
		cache.Record(zfs.PoolHistoryEntry{Pool: "tank", Time: newest.Add(time.Duration(minutes) * time.Minute), Internal: true,
			Event: "snapshot", Datasets: []string{fmt.Sprintf("tank@%d", minutes)}, Txg: uint64(minutes), Host: "host"})
	}

	// at the top
	row, _ := dt.position()
	require.Equal(t, 1, row)
	record(2)
	dt.waitForText("101 of 101 groups")
	assert.Equal(t, "snapshot", dt.shownEvents()[0])
	assert.Equal(t, "zfs set", dt.selected())
	row, line := dt.position()
	assert.Equal(t, 2, row, "the same record selected")
	assert.Equal(t, 2, line, "the new one shown above it")

	// scrolled down
	for range 50 {
		dt.key(tcell.KeyDown, 0)
	}
	testutil.WaitForEvents(t, dt.app, dt.screen)
	row, line = dt.position()
	require.Equal(t, 52, row)
	require.Less(t, line, 52, "scrolled")
	record(4)
	record(6)
	dt.waitForText("103 of 103 groups")
	newRow, newLine := dt.position()
	assert.Equal(t, 54, newRow, "the same record selected")
	assert.Equal(t, line, newLine, "on the same line")
}

// The events ZFS reports while the history is read may be in its output or not: they are added once.
func TestPoolHistoryCache_RecordsDuringARead(t *testing.T) {
	release := make(chan struct{})
	logged := zfs.PoolHistoryEntry{Pool: "tank", Time: historyAt(2), Internal: true, Event: "snapshot", Datasets: []string{"tank@a"}, Txg: 3}
	cache := NewPoolHistoryCache(func(ctx context.Context, pool string, progress func(zfs.PoolHistoryProgress)) ([]zfs.PoolHistoryEntry, error) {
		<-release
		return []zfs.PoolHistoryEntry{{Pool: pool, Time: historyAt(1), Event: "zpool create"}, logged}, nil
	})
	cache.recordDelay = time.Millisecond
	// of a pool not read: the next read has it
	cache.Record(zfs.PoolHistoryEntry{Pool: "other", Time: historyAt(1), Event: "snapshot"})
	assert.Empty(t, cache.pools)

	states := make(chan PoolHistoryState, 10)
	_, stop := cache.Watch("tank", func(state PoolHistoryState) { states <- state })
	defer stop()
	later := zfs.PoolHistoryEntry{Pool: "tank", Time: historyAt(2), Internal: true, Event: "snapshot", Datasets: []string{"tank@b"}, Txg: 4}
	// the time of a reported record has no fraction of a second, like the ones read
	cache.Record(logged)
	cache.Record(later)
	close(release)
	state := <-states
	require.NotNil(t, state.History)
	assert.Equal(t, []zfs.PoolHistoryEntry{{Pool: "tank", Time: historyAt(1), Event: "zpool create"}, logged, later}, state.History.Entries)

	// once read, added after a moment
	last := zfs.PoolHistoryEntry{Pool: "tank", Time: historyAt(3), Internal: true, Event: "destroy", Datasets: []string{"tank@a"}, Txg: 5}
	cache.Record(last)
	state = <-states
	assert.Equal(t, last, state.History.Entries[3])
	assert.Len(t, state.History.Entries, 4)
	assert.Same(t, &state.History.Entries[3], state.History.Groups[len(state.History.Groups)-1].Entries[0], "grouped again")
}
