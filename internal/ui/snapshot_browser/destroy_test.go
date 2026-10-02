package snapshot_browser

import (
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
	"zfs-file-history/internal/data"
	"zfs-file-history/internal/zfs"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type destroyCall struct {
	snapshots       []string
	recursive       bool
	dependantClones bool
	onUiThread      bool
}

type destroyTest struct {
	t       *testing.T
	app     *tview.Application
	screen  tcell.SimulationScreen
	browser *SnapshotBrowserComponent
	entries []*data.SnapshotBrowserEntry

	mu           sync.Mutex
	previews     []destroyCall
	destroys     []destroyCall
	previewError error

	// holds are the existing holds returned by listHolds
	holds        []zfs.Hold
	holdCalls    []holdCall
	releaseCalls []holdCall
	holdsListed  []holdCall
}

type holdCall struct {
	snapshots  []string
	recursive  bool
	onUiThread bool
}

func fullSnapshotNames(snapshots []*zfs.Snapshot) []string {
	var result []string
	for _, snapshot := range snapshots {
		result = append(result, snapshot.FullName)
	}
	return result
}

// newDestroyTest shows a snapshot browser with two snapshots. ZFS is never called: dry runs and destroys are recorded.
func newDestroyTest(t *testing.T) *destroyTest {
	dt := &destroyTest{t: t}

	originalPreview, originalDestroy := previewDestroySnapshots, destroySnapshots
	t.Cleanup(func() { previewDestroySnapshots, destroySnapshots = originalPreview, originalDestroy })
	originalList, originalHold, originalRelease := listHolds, holdSnapshots, releaseSnapshots
	t.Cleanup(func() { listHolds, holdSnapshots, releaseSnapshots = originalList, originalHold, originalRelease })
	listHolds = func(names []string, recursive bool) ([]zfs.Hold, error) {
		dt.mu.Lock()
		defer dt.mu.Unlock()
		dt.holdsListed = append(dt.holdsListed, holdCall{names, recursive, isOnUiThread(dt.app)})
		var result []zfs.Hold
		for _, h := range dt.holds {
			for _, name := range names {
				if h.Snapshot == name || (recursive && strings.HasSuffix(h.Snapshot, name[strings.Index(name, "@"):])) {
					result = append(result, h)
					break
				}
			}
		}
		return result, nil
	}
	holdSnapshots = func(names []string) error {
		dt.mu.Lock()
		defer dt.mu.Unlock()
		dt.holdCalls = append(dt.holdCalls, holdCall{snapshots: names, onUiThread: isOnUiThread(dt.app)})
		return nil
	}
	releaseSnapshots = func(names []string) error {
		dt.mu.Lock()
		defer dt.mu.Unlock()
		dt.releaseCalls = append(dt.releaseCalls, holdCall{snapshots: names, onUiThread: isOnUiThread(dt.app)})
		return nil
	}
	previewDestroySnapshots = func(snapshots []*zfs.Snapshot, recursive bool, dependantClones bool) (*zfs.DestroyPreview, error) {
		dt.mu.Lock()
		dt.previews = append(dt.previews, destroyCall{fullSnapshotNames(snapshots), recursive, dependantClones, isOnUiThread(dt.app)})
		err := dt.previewError
		dt.mu.Unlock()
		if err != nil {
			return nil, err
		}
		destroyed := fullSnapshotNames(snapshots)
		if recursive {
			destroyed = append(destroyed, "pool/data/child@daily-1")
		}
		return &zfs.DestroyPreview{Destroyed: destroyed, Reclaim: 3 * 1024 * 1024 * 1024}, nil
	}
	destroySnapshots = func(snapshots []*zfs.Snapshot, recursive bool, dependantClones bool) error {
		dt.mu.Lock()
		defer dt.mu.Unlock()
		dt.destroys = append(dt.destroys, destroyCall{fullSnapshotNames(snapshots), recursive, dependantClones, isOnUiThread(dt.app)})
		return nil
	}

	dt.app = tview.NewApplication()
	dt.screen = tcell.NewSimulationScreen("UTF-8")
	dt.app.SetScreen(dt.screen)
	dt.browser = NewSnapshotBrowser(dt.app)
	dt.app.SetRoot(dt.browser.GetLayout(), true)
	go func() { _ = dt.app.Run() }()
	t.Cleanup(dt.app.Stop)

	dt.entries = []*data.SnapshotBrowserEntry{
		{Snapshot: &zfs.Snapshot{Name: "daily-1", FullName: "pool/data@daily-1", Path: "/pool/data/.zfs/snapshot/daily-1"}},
		{Snapshot: &zfs.Snapshot{Name: "daily-2", FullName: "pool/data@daily-2", Path: "/pool/data/.zfs/snapshot/daily-2"}},
	}
	daily1 := dt.entries[0]
	onUiThread(t, dt.app, func() {
		// SetData sorts the given slice in place
		dt.browser.tableContainer.SetData(dt.entries)
		dt.browser.tableContainer.Select(daily1)
	})
	return dt
}

func (dt *destroyTest) press(key tcell.Key, r rune) {
	dt.screen.InjectKey(key, r, tcell.ModNone)
}

func (dt *destroyTest) hasDialog(name string) bool {
	has := false
	onUiThread(dt.t, dt.app, func() { has = dt.browser.container.Pages.HasPage(name) })
	return has
}

func (dt *destroyTest) waitForDialog(name string) {
	require.Eventually(dt.t, func() bool { return dt.hasDialog(name) }, 5*time.Second, 10*time.Millisecond, "dialog %s", name)
}

func (dt *destroyTest) calls() (previews []destroyCall, destroys []destroyCall) {
	dt.mu.Lock()
	defer dt.mu.Unlock()
	return append([]destroyCall{}, dt.previews...), append([]destroyCall{}, dt.destroys...)
}

func (dt *destroyTest) screenText() string {
	var text strings.Builder
	onUiThread(dt.t, dt.app, func() {
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

// selectAll selects both snapshots with the space key.
func (dt *destroyTest) selectAll() {
	onUiThread(dt.t, dt.app, func() { dt.browser.tableContainer.SelectFirstIfExists() })
	dt.press(tcell.KeyRune, ' ')
	dt.press(tcell.KeyDown, 0)
	dt.press(tcell.KeyRune, ' ')
	require.Eventually(dt.t, func() bool {
		count := 0
		onUiThread(dt.t, dt.app, func() { count = len(dt.browser.tableContainer.GetMultiSelection()) })
		return count == 2
	}, 2*time.Second, 10*time.Millisecond)
}

// confirm selects the first option of the confirmation ("Destroy").
func (dt *destroyTest) confirm() {
	dt.press(tcell.KeyRune, '1')
	dt.press(tcell.KeyEnter, 0)
}

func TestFormatDestroyDescription(t *testing.T) {
	description := formatDestroyDescription(&zfs.DestroyPreview{
		Destroyed: []string{"pool/data@a", "pool/data@b"},
		Reclaim:   1536 * 1024 * 1024,
	})
	assert.Equal(t, "This frees 1.5 GiB and cannot be undone.\n\nWill be destroyed (2):\n  pool/data@a\n  pool/data@b", description)

	var many []string
	for i := 0; i < 15; i++ {
		many = append(many, "pool/data@x")
	}
	description = formatDestroyDescription(&zfs.DestroyPreview{Destroyed: many})
	assert.Contains(t, description, "Will be destroyed (15):")
	assert.Equal(t, maxListedDestroyed, strings.Count(description, "pool/data@x"))
	assert.Contains(t, description, "… and 5 more")
}

func TestSnapshotBrowser_DeleteKeyShowsDryRunAndDestroysOnConfirmation(t *testing.T) {
	dt := newDestroyTest(t)

	// Delete: dry run in the background, then the confirmation with its result
	dt.press(tcell.KeyDelete, 0)
	dt.waitForDialog("DestroySnapshotsDialog")
	previews, destroys := dt.calls()
	require.Len(t, previews, 1)
	assert.Equal(t, destroyCall{snapshots: []string{"pool/data@daily-1"}}, previews[0], "dry run without flags, off the UI thread")
	assert.Empty(t, destroys, "nothing is destroyed before the confirmation")
	require.Eventually(t, func() bool {
		text := dt.screenText()
		return strings.Contains(text, "This frees 3.0 GiB") && strings.Contains(text, "pool/data@daily-1")
	}, 2*time.Second, 10*time.Millisecond)

	// cancel: nothing is destroyed
	dt.press(tcell.KeyEscape, 0)
	require.Eventually(t, func() bool { return !dt.hasDialog("DestroySnapshotsDialog") }, 2*time.Second, 10*time.Millisecond)
	time.Sleep(50 * time.Millisecond)
	_, destroys = dt.calls()
	assert.Empty(t, destroys)

	// confirm: exactly the previewed snapshot is destroyed, off the UI thread
	dt.press(tcell.KeyDelete, 0)
	dt.waitForDialog("DestroySnapshotsDialog")
	dt.confirm()
	dt.waitForDialog("SuccessDialog")
	_, destroys = dt.calls()
	require.Len(t, destroys, 1)
	assert.Equal(t, destroyCall{snapshots: []string{"pool/data@daily-1"}}, destroys[0])
}

func TestSnapshotBrowser_RecursiveDestroyFromTheMenu(t *testing.T) {
	dt := newDestroyTest(t)

	// menu: 1 create, 2 clone, 3 hold, 4 destroy, 5 destroy (recursive)
	dt.press(tcell.KeyEnter, 0)
	dt.waitForDialog("SnapshotActionDialog")
	dt.press(tcell.KeyRune, '5')
	dt.press(tcell.KeyEnter, 0)
	dt.waitForDialog("DestroySnapshotsDialog")

	previews, destroys := dt.calls()
	require.Len(t, previews, 1)
	assert.Equal(t, destroyCall{snapshots: []string{"pool/data@daily-1"}, recursive: true, dependantClones: true}, previews[0])
	assert.Empty(t, destroys)
	// everything that would be destroyed is listed, e.g. snapshots of child datasets
	require.Eventually(t, func() bool { return strings.Contains(dt.screenText(), "pool/data/child@daily-1") }, 2*time.Second, 10*time.Millisecond)

	dt.confirm()
	dt.waitForDialog("SuccessDialog")
	_, destroys = dt.calls()
	require.Len(t, destroys, 1)
	assert.Equal(t, destroyCall{snapshots: []string{"pool/data@daily-1"}, recursive: true, dependantClones: true}, destroys[0])
}

func TestSnapshotBrowser_FailedDryRunDestroysNothing(t *testing.T) {
	dt := newDestroyTest(t)
	dt.previewError = errors.New("cannot destroy: snapshot has dependent clones")

	dt.press(tcell.KeyEnter, 0)
	dt.waitForDialog("SnapshotActionDialog")
	dt.press(tcell.KeyRune, '4') // destroy
	dt.press(tcell.KeyEnter, 0)

	dt.waitForDialog("ErrorDialog")
	assert.False(t, dt.hasDialog("DestroySnapshotsDialog"))
	require.Eventually(t, func() bool { return strings.Contains(dt.screenText(), "dependent clones") }, 2*time.Second, 10*time.Millisecond)
	_, destroys := dt.calls()
	assert.Empty(t, destroys)
}

func TestSnapshotBrowser_DestroyMultiSelection(t *testing.T) {
	dt := newDestroyTest(t)

	dt.selectAll()

	// multi menu: 1 hold all, 2 destroy all
	dt.press(tcell.KeyEnter, 0)
	dt.waitForDialog("MultiSnapshotActionDialog")
	dt.press(tcell.KeyRune, '2')
	dt.press(tcell.KeyEnter, 0)
	dt.waitForDialog("DestroySnapshotsDialog")

	previews, destroys := dt.calls()
	require.Len(t, previews, 1)
	assert.ElementsMatch(t, []string{"pool/data@daily-1", "pool/data@daily-2"}, previews[0].snapshots, "one dry run for all")
	assert.Empty(t, destroys)
	require.Eventually(t, func() bool { return strings.Contains(dt.screenText(), "Will be destroyed (2)") }, 2*time.Second, 10*time.Millisecond)

	dt.confirm()
	dt.waitForDialog("SuccessDialog")
	_, destroys = dt.calls()
	require.Len(t, destroys, 1)
	assert.ElementsMatch(t, previews[0].snapshots, destroys[0].snapshots, "exactly the previewed snapshots are destroyed")
	require.Eventually(t, func() bool {
		count := -1
		onUiThread(t, dt.app, func() { count = len(dt.browser.tableContainer.GetMultiSelection()) })
		return count == 0
	}, 2*time.Second, 10*time.Millisecond, "the multi selection is cleared")
}
