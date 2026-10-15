package dialog

import (
	"errors"
	"strings"
	"testing"
	"time"
	"zfs-file-history/internal/testutil"
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

// testHistory is the history of two pools, oldest first, like zfs.PoolHistory returns it.
var testHistory = []zfs.PoolHistoryEntry{
	{Pool: "tank", Time: historyAt(1), Event: "zfs snapshot", Datasets: []string{"tank/data@daily"}, Text: "zfs snapshot -r tank/data@daily", User: "root", Host: "host"},
	{Pool: "tank", Time: historyAt(1), Internal: true, Event: "snapshot", Datasets: []string{"tank/data/home@daily"}, Txg: 7, Host: "host"},
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

// shownEvents returns the events of the records shown, from the top.
func (dt *historyDialogTest) shownEvents() []string {
	var events []string
	testutil.OnUiThread(dt.t, dt.app, func() {
		for _, entry := range dt.dialog.content.entries {
			events = append(events, entry.Event)
		}
	})
	return events
}

func (dt *historyDialogTest) key(key tcell.Key, r rune) {
	dt.screen.InjectKey(key, r, tcell.ModNone)
}

func TestPoolHistoryDialog_Pool(t *testing.T) {
	dt := newHistoryDialogTest(t, "tank", "", func() ([]zfs.PoolHistoryEntry, error) { return testHistory, nil })

	dt.waitForText("History of tank")
	dt.waitForText("3 of 5 records")
	// the commands of the pool, newest first; the events logged by ZFS only on request
	assert.Equal(t, []string{"zpool scrub", "zfs destroy", "zfs snapshot"}, dt.shownEvents())
	// the arguments of a command besides its event, the whole record of the selected one below the list
	dt.waitForText("2024-01-02 04:00:00")
	dt.waitForText("2024-01-02 04:00:00, Command, root on host: zpool scrub tank")

	dt.key(tcell.KeyRune, 'i')
	dt.waitForText("5 of 5 records")
	assert.Equal(t, []string{"scan done", "zpool scrub", "zfs destroy", "snapshot", "zfs snapshot"}, dt.shownEvents())
	// the selected record stays selected
	dt.waitForText("Command, root on host: zpool scrub tank")

	dt.key(tcell.KeyDown, 0)
	dt.key(tcell.KeyDown, 0)
	dt.waitForText("Logged by ZFS (txg 7), host: snapshot tank/data/home@daily")
}

func TestPoolHistoryDialog_Filter(t *testing.T) {
	dt := newHistoryDialogTest(t, "tank", "", func() ([]zfs.PoolHistoryEntry, error) { return testHistory, nil })
	dt.waitForText("3 of 5 records")

	// in the event, datasets, details or user, ignoring case
	dt.key(tcell.KeyRune, '/')
	for _, r := range "ALICE" {
		dt.key(tcell.KeyRune, r)
	}
	dt.waitForText("1 of 5 records")
	assert.Equal(t, []string{"zfs destroy"}, dt.shownEvents())

	// Esc clears the filter, the next one closes the dialog
	dt.key(tcell.KeyEscape, 0)
	dt.waitForText("3 of 5 records")
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
	dt.waitForText("1 of 1 records")
	assert.Equal(t, []string{"snapshot"}, dt.shownEvents())
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
		many = append(many, zfs.PoolHistoryEntry{Pool: "tank", Time: historyAt(0).Add(time.Duration(i) * time.Minute), Event: "zfs snapshot"})
	}
	dt := newHistoryDialogTest(t, "tank", "", func() ([]zfs.PoolHistoryEntry, error) {
		<-loaded
		return many, nil
	})
	dt.waitForText("Reading the history")
	close(loaded)
	dt.waitForText("200 of 200 records")

	dt.waitForText("2024-01-02 03:19:00")
	offset := func() (row int) {
		testutil.OnUiThread(t, dt.app, func() { row, _ = dt.dialog.table.GetOffset() })
		return row
	}
	assert.Equal(t, 0, offset())

	// the same with a filter that matches nothing, then cleared
	dt.key(tcell.KeyRune, '/')
	dt.key(tcell.KeyRune, 'x')
	dt.waitForText("0 of 200 records")
	dt.key(tcell.KeyEscape, 0)
	dt.waitForText("200 of 200 records")
	dt.waitForText("2024-01-02 03:19:00")
	assert.Equal(t, 0, offset())
}
