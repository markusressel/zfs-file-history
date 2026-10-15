package pool_browser

import (
	"errors"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"zfs-file-history/internal/testutil"
	"zfs-file-history/internal/ui/dialog"
	"zfs-file-history/internal/ui/shortcut_helper"
	"zfs-file-history/internal/ui/table"
	"zfs-file-history/internal/ui/theme"
	uiutil "zfs-file-history/internal/ui/util"
	"zfs-file-history/internal/util"
	"zfs-file-history/internal/zfs"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func names(entries []*zfs.PoolListEntry) []string {
	var result []string
	for _, entry := range entries {
		result = append(result, entry.Name)
	}
	return result
}

func TestSortPoolEntries(t *testing.T) {
	newEntries := func() []*zfs.PoolListEntry {
		return []*zfs.PoolListEntry{
			{Name: "tank", Health: "DEGRADED", Size: 300, Allocated: 100, Free: 200, Capacity: 33, Fragmentation: 5, DedupRatio: "1.00"},
			{Name: "Backup", Health: "ONLINE", Size: 100, Allocated: 90, Free: 10, Capacity: 90, Fragmentation: -1, DedupRatio: "1.50"},
			{Name: "rpool", Health: "ONLINE", Size: 200, Allocated: 20, Free: 180, Capacity: 10, Fragmentation: 40, DedupRatio: "1.00"},
		}
	}

	tests := []struct {
		column   *table.Column
		inverted bool
		expected []string
	}{
		{columnName, false, []string{"Backup", "rpool", "tank"}},
		{columnName, true, []string{"tank", "rpool", "Backup"}},
		// equal values fall back to the name
		{columnHealth, false, []string{"tank", "Backup", "rpool"}},
		{columnSize, false, []string{"Backup", "rpool", "tank"}},
		{columnSize, true, []string{"tank", "rpool", "Backup"}},
		{columnAllocated, false, []string{"rpool", "Backup", "tank"}},
		{columnFree, false, []string{"Backup", "rpool", "tank"}},
		{columnCapacity, false, []string{"rpool", "tank", "Backup"}},
		// not known first
		{columnFragmentation, false, []string{"Backup", "tank", "rpool"}},
		{columnDedup, false, []string{"rpool", "tank", "Backup"}},
		{columnDedup, true, []string{"Backup", "tank", "rpool"}},
	}
	for _, test := range tests {
		t.Run(test.column.Title, func(t *testing.T) {
			assert.Equal(t, test.expected, names(sortPoolEntries(newEntries(), test.column, test.inverted)))
		})
	}
}

func TestToTableCells(t *testing.T) {
	entry := &zfs.PoolListEntry{Name: "tank", Health: "DEGRADED", Size: 1024, Allocated: 512, Free: 512, Capacity: 50, Fragmentation: -1, DedupRatio: "1.25"}

	cells := toTableCells(0, tableColumns, entry)

	require.Len(t, cells, len(tableColumns))
	texts := map[*table.Column]string{}
	for i, column := range tableColumns {
		texts[column] = cells[i].Text
	}
	assert.Equal(t, "tank", texts[columnName])
	assert.Equal(t, "DEGRADED", texts[columnHealth])
	assert.Equal(t, uiutil.StableLengthHumanizedBytes(1024), texts[columnSize])
	assert.Equal(t, uiutil.StableLengthHumanizedBytes(512), texts[columnAllocated])
	assert.Equal(t, uiutil.StableLengthHumanizedBytes(512), texts[columnFree])
	assert.Equal(t, "50%", texts[columnCapacity])
	assert.Equal(t, "-", texts[columnFragmentation])
	assert.Equal(t, "1.25x", texts[columnDedup])

	assert.Equal(t, theme.Colors.Pool.Degraded, foreground(cells[1]))
	assert.Equal(t, tview.AlignRight, cells[2].Align)

	// fuller pools are warmer
	empty := toTableCells(0, []*table.Column{columnCapacity}, &zfs.PoolListEntry{Capacity: 0})[0]
	full := toTableCells(0, []*table.Column{columnCapacity}, &zfs.PoolListEntry{Capacity: 100})[0]
	assert.Equal(t, theme.Colors.Magnitude[0].Color, foreground(empty))
	assert.Equal(t, theme.Colors.Magnitude[len(theme.Colors.Magnitude)-1].Color, foreground(full))
}

func foreground(cell *tview.TableCell) tcell.Color {
	color, _, _ := cell.Style.Decompose()
	return color
}

func TestFormatPercent(t *testing.T) {
	assert.Equal(t, "-", formatPercent(-1))
	assert.Equal(t, "0%", formatPercent(0))
	assert.Equal(t, "39%", formatPercent(39))
}

func TestFormatStatus(t *testing.T) {
	entries := []*zfs.PoolListEntry{
		{Name: "rpool", Health: zfs.PoolStateOnline},
		{Name: "tank", Health: zfs.PoolStateDegraded},
		{Name: "backup", Health: zfs.PoolStateFaulted},
	}

	assert.Equal(t, "Loading pools...", testutil.StripTags(formatStatus(nil, 0, false)))
	assert.Equal(t, "0 pools", testutil.StripTags(formatStatus([]*zfs.PoolListEntry{}, 0, false)))
	assert.Equal(t, "1 pool", testutil.StripTags(formatStatus(entries[:1], 1, false)))
	assert.Equal(t, "3 pools · 2 not online", testutil.StripTags(formatStatus(entries, 3, false)))
	assert.Equal(t, "1 of 3 pools · 2 not online", testutil.StripTags(formatStatus(entries, 1, true)))
	// a filter matching all
	assert.Equal(t, "3 of 3 pools · 2 not online", testutil.StripTags(formatStatus(entries, 3, true)))
}

func TestFindEntryToSelect(t *testing.T) {
	entries := []*zfs.PoolListEntry{{Name: "rpool"}, {Name: "tank"}}
	assert.Nil(t, findEntryToSelect(nil, "tank"))
	assert.Same(t, entries[1], findEntryToSelect(entries, "tank"))
	assert.Same(t, entries[0], findEntryToSelect(entries, "gone"))
	assert.Same(t, entries[0], findEntryToSelect(entries, ""))
}

func setListPools(t *testing.T, f func() ([]*zfs.PoolListEntry, error)) {
	testutil.Replace(t, &listPools, f)
}

// newBrowserApp creates a browser as the root of a running application.
// The root is set before the event loop starts, like in CreateUi, to avoid racing with the first draw.
func newBrowserApp(t *testing.T) (*tview.Application, *PoolBrowserComponent) {
	app, _ := testutil.NewApp()
	browser := NewPoolBrowser(app)
	testutil.StartApp(t, app, browser.GetLayout())
	return app, browser
}

// recordedSelections are the names of the pools of the SelectedPoolChangedEvents ("" for none).
type recordedSelections struct {
	mu    sync.Mutex
	names []string
}

func (r *recordedSelections) record(browser *PoolBrowserComponent) *recordedSelections {
	browser.Events.Subscribe(func(event Event) {
		if e, ok := event.(SelectedPoolChangedEvent); ok {
			r.mu.Lock()
			defer r.mu.Unlock()
			name := ""
			if e.Pool != nil {
				name = e.Pool.Name
			}
			r.names = append(r.names, name)
		}
	})
	return r
}

func (r *recordedSelections) get() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string{}, r.names...)
}

func TestPoolBrowser_RefreshLoadsAndSelects(t *testing.T) {
	var mu sync.Mutex
	pools := []*zfs.PoolListEntry{{Name: "tank", Health: "ONLINE"}, {Name: "rpool", Health: "ONLINE"}}
	setListPools(t, func() ([]*zfs.PoolListEntry, error) {
		mu.Lock()
		defer mu.Unlock()
		return pools, nil
	})
	app, browser := newBrowserApp(t)
	selections := (&recordedSelections{}).record(browser)

	assert.False(t, browser.IsLoading())
	testutil.OnUiThread(t, app, func() { browser.Refresh() })
	assert.Eventually(t, func() bool { return len(selections.get()) == 1 }, 2*time.Second, 10*time.Millisecond)
	// sorted by name: the first one
	assert.Equal(t, []string{"rpool"}, selections.get())

	testutil.OnUiThread(t, app, func() {
		assert.False(t, browser.IsLoading())
		assert.Equal(t, "2 pools", testutil.StripTags(browser.tableContainer.GetFooter()))
		browser.tableContainer.Select(browser.tableContainer.GetEntries()[1])
		assert.Equal(t, "tank", browser.GetSelectedPool().Name)
	})

	// a reload keeps the selected pool
	mu.Lock()
	pools = []*zfs.PoolListEntry{{Name: "a", Health: "ONLINE"}, {Name: "rpool", Health: "ONLINE"}, {Name: "tank", Health: "DEGRADED"}}
	mu.Unlock()
	testutil.OnUiThread(t, app, func() { browser.Refresh() })
	assert.Eventually(t, func() bool { return len(selections.get()) == 3 }, 2*time.Second, 10*time.Millisecond)
	assert.Equal(t, []string{"rpool", "tank", "tank"}, selections.get())
	testutil.OnUiThread(t, app, func() {
		assert.Equal(t, "3 pools · 1 not online", testutil.StripTags(browser.tableContainer.GetFooter()))
	})

	// no pools anymore: nothing selected
	mu.Lock()
	pools = nil
	mu.Unlock()
	testutil.OnUiThread(t, app, func() { browser.Refresh() })
	assert.Eventually(t, func() bool { return len(selections.get()) == 4 }, 2*time.Second, 10*time.Millisecond)
	assert.Equal(t, "", selections.get()[3])
	testutil.OnUiThread(t, app, func() {
		assert.Nil(t, browser.GetSelectedPool())
		assert.Equal(t, "0 pools", testutil.StripTags(browser.tableContainer.GetFooter()))
	})
}

func TestPoolBrowser_SelectOnLoad(t *testing.T) {
	setListPools(t, func() ([]*zfs.PoolListEntry, error) {
		return []*zfs.PoolListEntry{{Name: "rpool"}, {Name: "tank"}}, nil
	})
	app, browser := newBrowserApp(t)
	selections := (&recordedSelections{}).record(browser)

	testutil.OnUiThread(t, app, func() {
		browser.SelectOnLoad("tank")
		browser.Refresh()
	})
	assert.Eventually(t, func() bool { return len(selections.get()) == 1 }, 2*time.Second, 10*time.Millisecond)
	assert.Equal(t, []string{"tank"}, selections.get())

	// only once: afterwards, the selection is kept
	testutil.OnUiThread(t, app, func() {
		browser.tableContainer.Select(browser.tableContainer.GetEntries()[0])
		browser.Refresh()
	})
	assert.Eventually(t, func() bool { return len(selections.get()) == 3 }, 2*time.Second, 10*time.Millisecond)
	assert.Equal(t, []string{"tank", "rpool", "rpool"}, selections.get())
}

func TestPoolBrowser_SelectPool(t *testing.T) {
	setListPools(t, func() ([]*zfs.PoolListEntry, error) {
		return []*zfs.PoolListEntry{{Name: "rpool"}, {Name: "tank"}}, nil
	})
	app, browser := newBrowserApp(t)
	selections := (&recordedSelections{}).record(browser)

	// not loaded yet: selected once loaded
	testutil.OnUiThread(t, app, func() {
		browser.SelectPool("tank")
		browser.Refresh()
	})
	assert.Eventually(t, func() bool { return len(selections.get()) == 1 }, 2*time.Second, 10*time.Millisecond)
	assert.Equal(t, []string{"tank"}, selections.get())

	// loaded: selected right away
	testutil.OnUiThread(t, app, func() {
		browser.SelectPool("rpool")
		assert.Equal(t, "rpool", browser.GetSelectedPool().Name)
	})
	assert.Equal(t, []string{"tank", "rpool"}, selections.get())
}

func TestPoolBrowser_RefreshErrorIsReported(t *testing.T) {
	setListPools(t, func() ([]*zfs.PoolListEntry, error) {
		return nil, errors.New("zpool list failed")
	})
	app, browser := newBrowserApp(t)

	statusEvents := make(chan PoolBrowserStatusEvent, 10)
	browser.Events.Subscribe(func(event Event) {
		if e, ok := event.(PoolBrowserStatusEvent); ok {
			statusEvents <- e
		}
	})

	testutil.OnUiThread(t, app, func() { browser.Refresh() })
	select {
	case e := <-statusEvents:
		assert.Contains(t, e.Message.Message, "zpool list failed")
	case <-time.After(2 * time.Second):
		t.Fatal("expected an error status event")
	}
	testutil.OnUiThread(t, app, func() {
		assert.Equal(t, "Loading pools...", testutil.StripTags(browser.tableContainer.GetFooter()))
	})
}

func TestPoolBrowser_Filter(t *testing.T) {
	setListPools(t, func() ([]*zfs.PoolListEntry, error) {
		return []*zfs.PoolListEntry{{Name: "rpool", Health: "ONLINE"}, {Name: "tank", Health: "ONLINE"}}, nil
	})
	app, browser := newBrowserApp(t)

	testutil.OnUiThread(t, app, func() { browser.Refresh() })
	assert.Eventually(t, func() bool {
		var selected *zfs.PoolListEntry
		testutil.OnUiThread(t, app, func() { selected = browser.GetSelectedPool() })
		return selected != nil
	}, 2*time.Second, 10*time.Millisecond)

	testutil.OnUiThread(t, app, func() {
		browser.tableContainer.SetFilterText("ta*")
		assert.Equal(t, []string{"tank"}, names(browser.tableContainer.GetEntries()))
		assert.Equal(t, "1 of 2 pools", testutil.StripTags(browser.tableContainer.GetFooter()))
	})
}

func TestPoolBrowser_Focus(t *testing.T) {
	setListPools(t, func() ([]*zfs.PoolListEntry, error) { return nil, nil })
	app, browser := newBrowserApp(t)

	testutil.OnUiThread(t, app, func() {
		app.SetFocus(tview.NewBox())
		assert.False(t, browser.HasFocus())
		browser.Focus()
		assert.True(t, browser.HasFocus())
	})
}

func TestPoolBrowser_Shortcuts(t *testing.T) {
	app, _ := testutil.NewApp()
	browser := NewPoolBrowser(app)

	var keys []string
	for _, shortcut := range browser.GetShortcutMap() {
		keys = append(keys, shortcut.KeyCombo[0])
		assert.NotNil(t, shortcut.Run, shortcut.Name)
	}
	assert.Equal(t, []string{"F2", "/"}, keys)
	assert.NotEmpty(t, browser.GetCommands(), "sorting by each column")

	// the columns dialog is shown on the dialog pages
	pages := tview.NewPages()
	browser.SetDialogPages(pages)
	browser.GetShortcutMap()[0].Run()
	assert.Equal(t, 1, pages.GetPageCount())
	var _ shortcut_helper.CommandProvider = browser
}

func TestPoolBrowser_Keys(t *testing.T) {
	setListPools(t, func() ([]*zfs.PoolListEntry, error) { return []*zfs.PoolListEntry{{Name: "rpool"}}, nil })
	app, browser := newBrowserApp(t)
	pages := tview.NewPages()
	testutil.OnUiThread(t, app, func() {
		browser.SetDialogPages(pages)
		browser.Focus()
		browser.Refresh()
	})
	require.Eventually(t, func() bool {
		var selected *zfs.PoolListEntry
		testutil.OnUiThread(t, app, func() { selected = browser.GetSelectedPool() })
		return selected != nil
	}, 2*time.Second, 10*time.Millisecond)

	// Enter on a pool opens its actions
	app.QueueEvent(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone))
	assert.Eventually(t, func() bool {
		front := ""
		testutil.OnUiThread(t, app, func() { front, _ = pages.GetFrontPage() })
		return front == string(dialog.PoolActionDialogPage)
	}, 2*time.Second, 10*time.Millisecond)
	testutil.OnUiThread(t, app, func() { assert.Equal(t, "rpool", browser.GetSelectedPool().Name) })
}

func TestPoolBrowser_ColumnsKey(t *testing.T) {
	setListPools(t, func() ([]*zfs.PoolListEntry, error) { return []*zfs.PoolListEntry{{Name: "rpool"}}, nil })
	app, browser := newBrowserApp(t)
	pages := tview.NewPages()
	testutil.OnUiThread(t, app, func() {
		browser.SetDialogPages(pages)
		browser.Focus()
	})

	// F2 opens the columns dialog
	app.QueueEvent(tcell.NewEventKey(tcell.KeyF2, 0, tcell.ModNone))
	assert.Eventually(t, func() bool {
		count := 0
		testutil.OnUiThread(t, app, func() { count = pages.GetPageCount() })
		return count == 1
	}, 2*time.Second, 10*time.Millisecond)
}

// setRunPoolAction replaces running the pool actions, recording them as "<zpool args> <pool>", failing with err.
func setRunPoolAction(t *testing.T, err error) func() []string {
	var mu sync.Mutex
	var actions []string
	testutil.Replace(t, &runPoolAction, func(action zfs.PoolAction, pool string) error {
		mu.Lock()
		defer mu.Unlock()
		actions = append(actions, strings.Join(append(append([]string{}, action.Args...), pool), " "))
		return err
	})
	return func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string{}, actions...)
	}
}

// commandNamed returns the command of the browser with the given name, read on the UI thread.
func commandNamed(t *testing.T, app *tview.Application, browser *PoolBrowserComponent, name string) shortcut_helper.ShortcutEntry {
	var found *shortcut_helper.ShortcutEntry
	testutil.OnUiThread(t, app, func() {
		for _, command := range browser.GetCommands() {
			if command.Name == name {
				found = &command
			}
		}
	})
	require.NotNil(t, found, "no command %q", name)
	return *found
}

// loadedBrowserApp returns a running browser with the pools loaded and the first one selected, showing its dialogs
// on the returned pages.
func loadedBrowserApp(t *testing.T, pools ...string) (*tview.Application, *PoolBrowserComponent, *tview.Pages) {
	setListPools(t, func() ([]*zfs.PoolListEntry, error) {
		var entries []*zfs.PoolListEntry
		for _, pool := range pools {
			entries = append(entries, &zfs.PoolListEntry{Name: pool})
		}
		return entries, nil
	})
	app, browser := newBrowserApp(t)
	pages := tview.NewPages()
	testutil.OnUiThread(t, app, func() {
		browser.SetDialogPages(pages)
		browser.Refresh()
	})
	require.Eventually(t, func() bool {
		var selected *zfs.PoolListEntry
		testutil.OnUiThread(t, app, func() { selected = browser.GetSelectedPool() })
		return selected != nil
	}, 2*time.Second, 10*time.Millisecond)
	return app, browser, pages
}

func TestPoolBrowser_Actions(t *testing.T) {
	actions := setRunPoolAction(t, nil)
	app, browser, pages := loadedBrowserApp(t, "tank")
	var messages atomic.Int32
	browser.Events.Subscribe(func(event Event) {
		if e, ok := event.(PoolBrowserStatusEvent); ok && e.Message.Message == "Started a scrub of tank" {
			messages.Add(1)
		}
	})
	var reloads atomic.Int32
	// an emitter of this test, so its listener does not outlive it
	testutil.Replace(t, &zfs.PoolsChanged, util.NewEmitter[struct{}]())
	zfs.PoolsChanged.Subscribe(func(struct{}) { reloads.Add(1) })

	// e.g. from the command menu
	command := commandNamed(t, app, browser, "Start scrub")
	testutil.OnUiThread(t, app, command.Run)

	require.Eventually(t, func() bool { return messages.Load() == 1 }, 2*time.Second, 10*time.Millisecond)
	assert.Equal(t, []string{"scrub tank"}, actions())
	assert.Equal(t, int32(1), reloads.Load(), "the pools are reloaded")
	// the dialog is closed
	require.Eventually(t, func() bool {
		count := -1
		testutil.OnUiThread(t, app, func() { count = pages.GetPageCount() })
		return count == 0
	}, 2*time.Second, 10*time.Millisecond)
}

func TestPoolBrowser_ActionsDependOnTheStatus(t *testing.T) {
	setRunPoolAction(t, nil)
	app, browser, _ := loadedBrowserApp(t, "tank")
	scrubbing := &zfs.PoolStatus{Name: "tank", Scan: "scrub in progress since Tue Oct 13 10:00:01 2026", ScanProgress: 10}

	testutil.OnUiThread(t, app, func() { browser.SetStatus(scrubbing) })
	commandNamed(t, app, browser, "Pause scrub")

	// the status of another pool (e.g. still loading the one of the selected pool) is not used
	testutil.OnUiThread(t, app, func() {
		other := *scrubbing
		other.Name = "other"
		browser.SetStatus(&other)
	})
	commandNamed(t, app, browser, "Start scrub")
}

func TestPoolBrowser_ActionFails(t *testing.T) {
	actions := setRunPoolAction(t, errors.New("cannot scrub tank: permission denied"))
	app, browser, pages := loadedBrowserApp(t, "tank")

	command := commandNamed(t, app, browser, "Start TRIM")
	testutil.OnUiThread(t, app, command.Run)

	// the error is shown, with a retry
	require.Eventually(t, func() bool {
		front := ""
		testutil.OnUiThread(t, app, func() { front, _ = pages.GetFrontPage() })
		return front != "" && front != string(dialog.PoolActionDialogPage)
	}, 2*time.Second, 10*time.Millisecond)
	assert.Equal(t, []string{"trim tank"}, actions())
}

func TestPoolBrowser_ActionNeedsRoot(t *testing.T) {
	setRunPoolAction(t, &zfs.PoolActionNeedsRootError{Action: zfs.PoolActionScrub, Pool: "tank", Cause: errors.New("permission denied")})
	app, browser, pages := loadedBrowserApp(t, "tank")

	command := commandNamed(t, app, browser, "Start scrub")
	testutil.OnUiThread(t, app, command.Run)

	// offers to run it with sudo, or to allow it
	require.Eventually(t, func() bool {
		front := ""
		testutil.OnUiThread(t, app, func() { front, _ = pages.GetFrontPage() })
		return front == string(dialog.PoolActionRootDialogPage)
	}, 2*time.Second, 10*time.Millisecond)
}

// Upgrading the pool cannot be undone: it only runs once the name of the pool was typed.
func TestPoolBrowser_UpgradeIsConfirmedByName(t *testing.T) {
	actions := setRunPoolAction(t, nil)
	setListPools(t, func() ([]*zfs.PoolListEntry, error) { return []*zfs.PoolListEntry{{Name: "tank"}}, nil })
	var upgraded atomic.Int32
	app, screen := testutil.NewApp()
	browser := NewPoolBrowser(app)
	browser.Events.Subscribe(func(event Event) {
		if e, ok := event.(PoolBrowserStatusEvent); ok && e.Message.Message == "Enabled all features of tank" {
			upgraded.Add(1)
		}
	})
	testutil.StartApp(t, app, browser.GetLayout())
	testutil.OnUiThread(t, app, browser.Refresh)
	require.Eventually(t, func() bool {
		var selected *zfs.PoolListEntry
		testutil.OnUiThread(t, app, func() { selected = browser.GetSelectedPool() })
		return selected != nil
	}, 2*time.Second, 10*time.Millisecond)
	testutil.OnUiThread(t, app, func() {
		browser.SetStatus(&zfs.PoolStatus{Name: "tank", ScanProgress: -1, Action: "Enable all features using 'zpool upgrade'."})
	})

	// e.g. from the command menu
	command := commandNamed(t, app, browser, "Upgrade pool")
	testutil.OnUiThread(t, app, command.Run)
	testutil.WaitForPage(t, app, browser.layout, string(dialog.PoolUpgradeDialogPage), 2*time.Second)
	assert.Empty(t, actions(), "not before the name was typed")

	// Enter alone, or another name, does not upgrade
	testutil.Press(t, app, screen, tcell.KeyEnter, 0)
	testutil.Type(t, app, screen, "tan")
	testutil.Press(t, app, screen, tcell.KeyEnter, 0)
	testutil.WaitForUpdates(t, app)
	assert.True(t, testutil.HasPage(t, app, browser.layout, string(dialog.PoolUpgradeDialogPage)))
	assert.Empty(t, actions())

	testutil.Type(t, app, screen, "k")
	testutil.Press(t, app, screen, tcell.KeyEnter, 0)
	require.Eventually(t, func() bool { return upgraded.Load() == 1 }, 2*time.Second, 10*time.Millisecond)
	assert.Equal(t, []string{"upgrade tank"}, actions())
	testutil.WaitForNoPage(t, app, browser.layout, string(dialog.PoolUpgradeDialogPage), 2*time.Second)
}

// Enabling features cannot be undone: they are listed and chosen first, and only enabled once the name of the pool
// was typed.
func TestPoolBrowser_EnableFeatures(t *testing.T) {
	setListPools(t, func() ([]*zfs.PoolListEntry, error) { return []*zfs.PoolListEntry{{Name: "tank"}}, nil })
	testutil.Replace(t, &listPoolFeatures, func(pool string) (*zfs.PoolFeatures, error) {
		return &zfs.PoolFeatures{Pool: pool, Compatibility: "off", Features: []zfs.PoolFeature{
			{Name: "bookmarks", Description: "\"zfs bookmark\" command", State: zfs.PoolFeatureDisabled},
			{Name: "async_destroy", Description: "Destroy filesystems asynchronously.", State: zfs.PoolFeatureEnabled, Unavailable: "already enabled"},
		}}, nil
	})
	var mu sync.Mutex
	var enabled [][]string
	testutil.Replace(t, &enablePoolFeatures, func(pool string, features []string) error {
		mu.Lock()
		defer mu.Unlock()
		enabled = append(enabled, append([]string{pool}, features...))
		return nil
	})
	getEnabled := func() [][]string {
		mu.Lock()
		defer mu.Unlock()
		return append([][]string{}, enabled...)
	}
	var done atomic.Int32
	app, screen := testutil.NewApp()
	browser := NewPoolBrowser(app)
	browser.Events.Subscribe(func(event Event) {
		if e, ok := event.(PoolBrowserStatusEvent); ok && e.Message.Message == "Enabled the features bookmarks of tank" {
			done.Add(1)
		}
	})
	testutil.StartApp(t, app, browser.GetLayout())
	testutil.OnUiThread(t, app, browser.Refresh)
	require.Eventually(t, func() bool {
		var selected *zfs.PoolListEntry
		testutil.OnUiThread(t, app, func() { selected = browser.GetSelectedPool() })
		return selected != nil
	}, 2*time.Second, 10*time.Millisecond)

	command := commandNamed(t, app, browser, "Enable features")
	testutil.OnUiThread(t, app, command.Run)
	testutil.WaitForPage(t, app, browser.layout, string(dialog.PoolFeaturesDialogPage), 2*time.Second)
	testutil.WaitForText(t, app, screen, "Already enabled", 2*time.Second)

	testutil.Press(t, app, screen, tcell.KeyRune, ' ')
	testutil.Press(t, app, screen, tcell.KeyEnter, 0)
	testutil.WaitForPage(t, app, browser.layout, string(dialog.EnablePoolFeaturesDialogPage), 2*time.Second)
	assert.Empty(t, getEnabled(), "not before the name was typed")

	testutil.Type(t, app, screen, "tan")
	testutil.Press(t, app, screen, tcell.KeyEnter, 0)
	testutil.WaitForUpdates(t, app)
	assert.Empty(t, getEnabled(), "not with another name")

	testutil.Type(t, app, screen, "k")
	testutil.Press(t, app, screen, tcell.KeyEnter, 0)
	require.Eventually(t, func() bool { return done.Load() == 1 }, 2*time.Second, 10*time.Millisecond)
	assert.Equal(t, [][]string{{"tank", "bookmarks"}}, getEnabled())
	testutil.WaitForNoPage(t, app, browser.layout, string(dialog.EnablePoolFeaturesDialogPage), 2*time.Second)
}

func TestPoolBrowser_EnableFeatures_ListingFails(t *testing.T) {
	testutil.Replace(t, &listPoolFeatures, func(pool string) (*zfs.PoolFeatures, error) {
		return nil, errors.New("cannot list the features of tank: no pool")
	})
	app, browser, pages := loadedBrowserApp(t, "tank")

	command := commandNamed(t, app, browser, "Enable features")
	testutil.OnUiThread(t, app, command.Run)

	testutil.WaitForPage(t, app, pages, "ErrorDialog", 2*time.Second)
	assert.False(t, testutil.HasPage(t, app, pages, string(dialog.PoolFeaturesDialogPage)))
}

// Not offered unless zpool status suggests it.
func TestPoolBrowser_UpgradeOnlyIfSuggested(t *testing.T) {
	app, browser, _ := loadedBrowserApp(t, "tank")
	testutil.OnUiThread(t, app, func() { browser.SetStatus(&zfs.PoolStatus{Name: "tank", ScanProgress: -1}) })
	var names []string
	testutil.OnUiThread(t, app, func() {
		for _, command := range browser.GetCommands() {
			names = append(names, command.Name)
		}
	})
	assert.NotContains(t, names, "Upgrade pool")
}

func TestPoolBrowser_ActionShortcut(t *testing.T) {
	app, browser, _ := loadedBrowserApp(t, "tank")

	testutil.OnUiThread(t, app, func() {
		shortcuts := browser.GetShortcutMap()
		last := shortcuts[len(shortcuts)-1]
		assert.Equal(t, shortcut_helper.KeyEnter, last.KeyCombo[0])
		assert.NotNil(t, last.Run)
	})
}

// h shows the history of the selected pool, only offered while it can be read (see SetOnShowHistory)
func TestPoolBrowser_History(t *testing.T) {
	app, browser, _ := loadedBrowserApp(t, "tank")
	shown := make(chan string, 1)
	hasHistory := func() (found bool) {
		testutil.OnUiThread(t, app, func() {
			found = slices.ContainsFunc(browser.GetShortcutMap(), func(entry shortcut_helper.ShortcutEntry) bool { return entry.Name == "History" })
		})
		return found
	}

	assert.False(t, hasHistory())
	app.QueueEvent(tcell.NewEventKey(tcell.KeyRune, 'h', tcell.ModNone))

	testutil.OnUiThread(t, app, func() {
		browser.SetOnShowHistory(func(pool string) { shown <- pool })
		browser.Focus()
	})
	assert.True(t, hasHistory())
	app.QueueEvent(tcell.NewEventKey(tcell.KeyRune, 'h', tcell.ModNone))
	select {
	case pool := <-shown:
		assert.Equal(t, "tank", pool)
	case <-time.After(2 * time.Second):
		t.Fatal("the history was not shown")
	}
}
