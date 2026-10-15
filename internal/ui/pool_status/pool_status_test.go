package pool_status

import (
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
	"zfs-file-history/internal/testutil"
	"zfs-file-history/internal/ui/theme"
	"zfs-file-history/internal/ui/txwidgets"
	"zfs-file-history/internal/zfs"

	"github.com/rivo/tview"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFormatStatus(t *testing.T) {
	status := &zfs.PoolStatus{
		Name:   "tank",
		State:  zfs.PoolStateDegraded,
		Status: "One or more devices is currently being resilvered.  The pool will\ncontinue to function, possibly in a degraded state.",
		Action: "Wait for the resilver to complete.",
		Scan:   "resilver in progress since Tue Oct 13 09:12:44 2026\n254G resilvered, 31.77% done, 02:10:31 to go",
		// 10 of the 30 characters of the bar
		ScanProgress: 33.4,
		Other:        []zfs.PoolStatusField{{Name: "remove", Value: "Removal of vdev 1 copied 12.3G"}},
		Errors:       "1 data errors, use '-v' for a list",
	}

	lines := strings.Split(testutil.StripTags(formatStatus(status)), "\n")

	assert.Equal(t, []string{
		"  State:  DEGRADED",
		" Status:  One or more devices is currently being resilvered.  The pool will",
		"          continue to function, possibly in a degraded state.",
		" Action:  Wait for the resilver to complete.",
		"   Scan:  resilver in progress since Tue Oct 13 09:12:44 2026",
		"          254G resilvered, 31.77% done, 02:10:31 to go",
		"          " + strings.Repeat("█", 10) + strings.Repeat("░", 20) + " 33.4%",
		" Remove:  Removal of vdev 1 copied 12.3G",
		" Errors:  1 data errors, use '-v' for a list",
		"",
	}, lines)
}

func TestFormatStatus_Healthy(t *testing.T) {
	status := &zfs.PoolStatus{
		Name:         "rpool",
		State:        zfs.PoolStateOnline,
		Scan:         "scrub repaired 0B in 00:06:46 with 0 errors on Thu Oct  1 12:50:53 2026",
		ScanProgress: -1,
		See:          "https://openzfs.github.io/openzfs-docs/msg/ZFS-8000-9P",
		Errors:       "No known data errors",
	}

	text := formatStatus(status)

	// the fields that are not set are left out, no progress bar without a running scan
	assert.Equal(t, strings.Join([]string{
		"  State:  ONLINE",
		"    See:  https://openzfs.github.io/openzfs-docs/msg/ZFS-8000-9P",
		"   Scan:  scrub repaired 0B in 00:06:46 with 0 errors on Thu Oct  1 12:50:53 2026",
		" Errors:  No known data errors",
		"",
	}, "\n"), testutil.StripTags(text))
	assert.Contains(t, text, txwidgets.ColorTag(theme.Colors.Pool.Online)+"ONLINE", "colored by the state")
}

func TestErrorsColor(t *testing.T) {
	assert.Equal(t, theme.Colors.Pool.Online, errorsColor("No known data errors"))
	assert.Equal(t, theme.Colors.Pool.Faulted, errorsColor("1 data errors, use '-v' for a list"))
}

func TestCapitalize(t *testing.T) {
	assert.Equal(t, "", capitalize(""))
	assert.Equal(t, "Remove", capitalize("remove"))
	assert.Equal(t, "Checkpoint", capitalize("Checkpoint"))
}

// fakeLoader loads the status of each pool by its name, counting the loads.
type fakeLoader struct {
	mu    sync.Mutex
	loads []string
	fail  map[string]bool
}

func (f *fakeLoader) load(name string) (*zfs.PoolStatus, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.loads = append(f.loads, name)
	if f.fail[name] {
		return nil, errors.New("zpool status failed")
	}
	status := &zfs.PoolStatus{
		Name:         name,
		State:        zfs.PoolStateOnline,
		ScanProgress: -1,
		Errors:       "No known data errors",
		Devices:      []*zfs.PoolDevice{{Name: name, State: zfs.PoolStateOnline}},
	}
	return status, nil
}

func (f *fakeLoader) getLoads() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string{}, f.loads...)
}

// recordedStatuses are the names of the pools of the StatusLoadedEvents ("" for none).
type recordedStatuses struct {
	mu    sync.Mutex
	names []string
}

func (r *recordedStatuses) get() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string{}, r.names...)
}

func setupTest(t *testing.T) (*tview.Application, *PoolStatusComponent, *fakeLoader, *recordedStatuses) {
	fake := &fakeLoader{fail: map[string]bool{}}
	testutil.Replace(t, &loadPoolStatus, fake.load)

	app, _ := testutil.NewApp()
	poolStatus := NewPoolStatus(app)
	statuses := &recordedStatuses{}
	poolStatus.Events.Subscribe(func(event Event) {
		if e, ok := event.(StatusLoadedEvent); ok {
			statuses.mu.Lock()
			defer statuses.mu.Unlock()
			name := ""
			if e.Status != nil {
				name = e.Status.Name
			}
			statuses.names = append(statuses.names, name)
		}
	})
	// set the root before the event loop starts, like CreateUi does
	testutil.StartApp(t, app, poolStatus.GetLayout())
	return app, poolStatus, fake, statuses
}

// displayed returns the title and text currently displayed, read on the UI thread.
func displayed(t *testing.T, app *tview.Application, poolStatus *PoolStatusComponent) (title string, text string) {
	testutil.OnUiThread(t, app, func() {
		title = poolStatus.textView.GetTitle()
		text = poolStatus.textView.GetText(true)
	})
	return title, text
}

func TestPoolStatus_SetPool(t *testing.T) {
	app, poolStatus, fake, statuses := setupTest(t)

	testutil.OnUiThread(t, app, func() { poolStatus.SetPool("tank") })
	require.Eventually(t, func() bool { return len(statuses.get()) == 1 }, 2*time.Second, 10*time.Millisecond)
	assert.Equal(t, []string{"tank"}, statuses.get())
	title, text := displayed(t, app, poolStatus)
	assert.Equal(t, " Pool: tank ", title)
	assert.Contains(t, text, "State:  ONLINE")
	testutil.OnUiThread(t, app, func() {
		assert.Equal(t, "tank", poolStatus.GetPool())
		assert.False(t, poolStatus.IsLoading())
	})

	// the same pool again does not reload, a refresh does
	testutil.OnUiThread(t, app, func() { poolStatus.SetPool("tank") })
	testutil.OnUiThread(t, app, func() { poolStatus.Refresh() })
	require.Eventually(t, func() bool { return len(statuses.get()) == 2 }, 2*time.Second, 10*time.Millisecond)
	assert.Equal(t, []string{"tank", "tank"}, fake.getLoads())

	// none: cleared
	testutil.OnUiThread(t, app, func() { poolStatus.SetPool("") })
	require.Eventually(t, func() bool { return len(statuses.get()) == 3 }, 2*time.Second, 10*time.Millisecond)
	assert.Equal(t, "", statuses.get()[2])
	title, text = displayed(t, app, poolStatus)
	assert.Equal(t, " Pool ", title)
	assert.Empty(t, text)
	assert.Len(t, fake.getLoads(), 2, "nothing to load")
}

func TestPoolStatus_LoadError(t *testing.T) {
	app, poolStatus, fake, statuses := setupTest(t)
	fake.fail["broken"] = true

	testutil.OnUiThread(t, app, func() { poolStatus.SetPool("broken") })
	require.Eventually(t, func() bool { return len(statuses.get()) == 1 }, 2*time.Second, 10*time.Millisecond)
	assert.Equal(t, []string{""}, statuses.get(), "no status")
	title, text := displayed(t, app, poolStatus)
	assert.Equal(t, " Pool: broken ", title)
	assert.Contains(t, text, "Could not load the status: zpool status failed")
}

func TestPoolStatus_ShowStatus(t *testing.T) {
	app, poolStatus, fake, statuses := setupTest(t)
	testutil.OnUiThread(t, app, func() { poolStatus.SetPool("tank") })
	require.Eventually(t, func() bool { return len(statuses.get()) == 1 }, 2*time.Second, 10*time.Millisecond)

	// e.g. the progress of a scrub, loaded by the activity monitor: shown without loading it again
	scrubbing := &zfs.PoolStatus{Name: "tank", State: zfs.PoolStateOnline, ScanProgress: 42,
		Scan: "scrub in progress since Tue Oct 13 10:00:01 2026\n0B repaired, 42.00% done"}
	testutil.OnUiThread(t, app, func() { poolStatus.ShowStatus(scrubbing) })
	require.Eventually(t, func() bool { return len(statuses.get()) == 2 }, 2*time.Second, 10*time.Millisecond)
	_, text := displayed(t, app, poolStatus)
	assert.Contains(t, text, "42.0%")
	assert.Equal(t, []string{"tank"}, fake.getLoads())

	// the statuses of other pools are ignored, and nothing is loaded periodically
	testutil.OnUiThread(t, app, func() {
		poolStatus.ShowStatus(&zfs.PoolStatus{Name: "rpool", ScanProgress: -1})
		poolStatus.ShowStatus(nil)
	})
	time.Sleep(100 * time.Millisecond)
	assert.Equal(t, []string{"tank", "tank"}, statuses.get())
	assert.Equal(t, []string{"tank"}, fake.getLoads())
}

func TestPoolStatus_Focus(t *testing.T) {
	app, poolStatus, _, _ := setupTest(t)
	testutil.OnUiThread(t, app, func() {
		app.SetFocus(tview.NewBox())
		assert.False(t, poolStatus.HasFocus())
		poolStatus.Focus()
		assert.True(t, poolStatus.HasFocus())
	})
}

// the size of the status, for the layout of the pool page
func TestPoolStatus_ContentSize(t *testing.T) {
	app, poolStatus, _, statuses := setupTest(t)
	testutil.OnUiThread(t, app, func() {
		width, height := poolStatus.ContentSize()
		assert.Zero(t, width+height, "nothing loaded yet")
	})

	testutil.OnUiThread(t, app, func() { poolStatus.SetPool("tank") })
	require.Eventually(t, func() bool { return len(statuses.get()) == 1 }, 2*time.Second, 10*time.Millisecond)
	testutil.OnUiThread(t, app, func() {
		width, height := poolStatus.ContentSize()
		// " Errors:  No known data errors" and "State", plus the border
		assert.Equal(t, len(" Errors:  No known data errors")+2, width)
		assert.Equal(t, 2+2, height)
	})
}
