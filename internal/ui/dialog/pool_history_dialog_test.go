package dialog

import (
	"errors"
	"slices"
	"strings"
	"testing"
	"time"
	"zfs-file-history/internal/testutil"
	"zfs-file-history/internal/ui/scrollbar"
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
}

// newHistoryDialogTest shows the history of the pool or dataset, read by load.
func newHistoryDialogTest(t *testing.T, pool string, dataset string, load func() ([]zfs.PoolHistoryEntry, error)) *historyDialogTest {
	dt := &historyDialogTest{t: t, app: tview.NewApplication(), screen: tcell.NewSimulationScreen("UTF-8")}
	dt.app.SetScreen(dt.screen)
	dt.screen.SetSize(140, 40)
	dt.pages = tview.NewPages().AddPage("background", tview.NewBox(), true, true)
	dt.app.SetRoot(dt.pages, true)
	go func() { _ = dt.app.Run() }()
	t.Cleanup(dt.app.Stop)
	testutil.OnUiThread(t, dt.app, func() {
		dt.dialog = NewPoolHistoryDialog(dt.app, pool, dataset, load)
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
