package pool_browser

import (
	"sync"
	"testing"
	"time"
	"zfs-file-history/internal/testutil"
	"zfs-file-history/internal/ui/dialog"
	"zfs-file-history/internal/zfs"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// destroyTest runs a browser with the pool "tank" and records the calls of destroying it instead of destroying it.
type destroyTest struct {
	t       *testing.T
	app     *tview.Application
	screen  tcell.SimulationScreen
	browser *PoolBrowserComponent
	preview *zfs.PoolDestroyPreview

	mu    sync.Mutex
	calls []string
}

func newDestroyTest(t *testing.T) *destroyTest {
	dt := &destroyTest{t: t, preview: &zfs.PoolDestroyPreview{Pool: "tank", Datasets: []string{"tank", "tank/data"}, Allocated: 4096}}
	setListPools(t, func() ([]*zfs.PoolListEntry, error) { return []*zfs.PoolListEntry{{Name: "tank"}}, nil })
	testutil.Replace(t, &previewDestroyPool, func(pool string) (*zfs.PoolDestroyPreview, error) {
		dt.record("preview " + pool)
		return dt.preview, nil
	})
	testutil.Replace(t, &destroyPool, func(confirmed *zfs.PoolDestroyPreview) error {
		dt.record("destroy " + confirmed.Pool)
		assert.Same(t, dt.preview, confirmed, "destroys what was confirmed")
		return nil
	})
	dt.app, dt.screen = testutil.NewApp()
	dt.browser = NewPoolBrowser(dt.app)
	testutil.StartApp(t, dt.app, dt.browser.GetLayout())
	testutil.OnUiThread(t, dt.app, dt.browser.Refresh)
	require.Eventually(t, func() bool {
		var selected *zfs.PoolListEntry
		testutil.OnUiThread(t, dt.app, func() { selected = dt.browser.GetSelectedPool() })
		return selected != nil
	}, 2*time.Second, 10*time.Millisecond)
	return dt
}

func (dt *destroyTest) record(call string) {
	dt.mu.Lock()
	defer dt.mu.Unlock()
	dt.calls = append(dt.calls, call)
}

func (dt *destroyTest) recorded() []string {
	dt.mu.Lock()
	defer dt.mu.Unlock()
	return append([]string(nil), dt.calls...)
}

func (dt *destroyTest) press(key tcell.Key, r rune) {
	testutil.Press(dt.t, dt.app, dt.screen, key, r)
}

func (dt *destroyTest) waitForDialog(name string) {
	testutil.WaitForPage(dt.t, dt.app, dt.browser.layout, name, 2*time.Second)
}

// startDestroy chooses "Destroy pool" (e.g. from the command menu) and waits for the first confirmation.
func (dt *destroyTest) startDestroy() {
	command := commandNamed(dt.t, dt.app, dt.browser, "Destroy pool")
	testutil.OnUiThread(dt.t, dt.app, command.Run)
	dt.waitForDialog(string(dialog.DestroyPoolDialogPage))
}

// Destroying a pool cannot be undone: it shows what is lost, a box must be checked, and the name of the pool typed.
func TestPoolBrowser_DestroyPool(t *testing.T) {
	dt := newDestroyTest(t)

	dt.startDestroy()
	text := testutil.ScreenText(t, dt.app, dt.screen)
	assert.Contains(t, text, "Destroy the pool tank with all its datasets")
	assert.Contains(t, text, "tank/data")
	assert.Contains(t, text, "Destroy its 2 datasets with all snapshots and files")

	// without the box: nothing is destroyed
	dt.press(tcell.KeyRune, '1') // continue
	dt.press(tcell.KeyEnter, 0)
	dt.waitForDialog("ErrorDialog")
	assert.False(t, testutil.HasPage(t, dt.app, dt.browser.layout, "DestroyPoolByNameDialog"))
	dt.press(tcell.KeyEscape, 0)
	testutil.WaitForNoPage(t, dt.app, dt.browser.layout, "ErrorDialog", 2*time.Second)

	// with it, the name is asked for
	dt.startDestroy()
	dt.press(tcell.KeyUp, 0) // to the checklist
	dt.press(tcell.KeyRune, ' ')
	dt.press(tcell.KeyRune, '1') // continue
	dt.press(tcell.KeyEnter, 0)
	dt.waitForDialog("DestroyPoolByNameDialog")
	assert.Equal(t, []string{"preview tank", "preview tank"}, dt.recorded(), "only the previews")

	// Enter alone, or another name, does not destroy it
	dt.press(tcell.KeyEnter, 0)
	testutil.Type(t, dt.app, dt.screen, "tan")
	dt.press(tcell.KeyEnter, 0)
	testutil.WaitForUpdates(t, dt.app)
	assert.True(t, testutil.HasPage(t, dt.app, dt.browser.layout, "DestroyPoolByNameDialog"))
	assert.Len(t, dt.recorded(), 2)

	testutil.Type(t, dt.app, dt.screen, "k")
	dt.press(tcell.KeyEnter, 0)
	dt.waitForDialog("SuccessDialog")
	assert.Equal(t, "destroy tank", dt.recorded()[2])
}

func TestPoolBrowser_DestroyPoolCanBeCancelled(t *testing.T) {
	dt := newDestroyTest(t)

	dt.startDestroy()
	dt.press(tcell.KeyUp, 0)
	dt.press(tcell.KeyRune, ' ')
	dt.press(tcell.KeyRune, '1')
	dt.press(tcell.KeyEnter, 0)
	dt.waitForDialog("DestroyPoolByNameDialog")
	testutil.Type(t, dt.app, dt.screen, "tank")
	dt.press(tcell.KeyEscape, 0)
	testutil.WaitForNoPage(t, dt.app, dt.browser.layout, "DestroyPoolByNameDialog", 2*time.Second)
	testutil.WaitForUpdates(t, dt.app)

	assert.Equal(t, []string{"preview tank"}, dt.recorded())
}

// A pool the preview refuses (e.g. the running system is mounted from it) shows why, and nothing is confirmed.
func TestPoolBrowser_DestroyPoolRefused(t *testing.T) {
	dt := newDestroyTest(t)
	testutil.Replace(t, &previewDestroyPool, func(pool string) (*zfs.PoolDestroyPreview, error) {
		return nil, assert.AnError
	})

	command := commandNamed(t, dt.app, dt.browser, "Destroy pool")
	testutil.OnUiThread(t, dt.app, command.Run)

	dt.waitForDialog("ErrorDialog")
	assert.False(t, testutil.HasPage(t, dt.app, dt.browser.layout, string(dialog.DestroyPoolDialogPage)))
	assert.Empty(t, dt.recorded())
}

// blockPreview makes listing what destroying the pool destroys wait until the returned function is called.
func (dt *destroyTest) blockPreview() (release func()) {
	released := make(chan struct{})
	testutil.Replace(dt.t, &previewDestroyPool, func(pool string) (*zfs.PoolDestroyPreview, error) {
		<-released
		dt.record("preview " + pool)
		return dt.preview, nil
	})
	return sync.OnceFunc(func() { close(released) })
}

// Regression: choosing "Destroy pool" listed what it destroys in the action dialog, with a spinner next to the option,
// which looked like the pool was destroyed without a confirmation. The dialog of the confirmation opens right away
// and says what is checked; the confirmation follows once that is done.
func TestPoolBrowser_DestroyPoolChecksInTheConfirmation(t *testing.T) {
	dt := newDestroyTest(t)
	release := dt.blockPreview()
	t.Cleanup(release)

	command := commandNamed(t, dt.app, dt.browser, "Destroy pool")
	testutil.OnUiThread(t, dt.app, command.Run)

	dt.waitForDialog(string(dialog.PreviewDialogPage))
	testutil.WaitForNoPage(t, dt.app, dt.browser.layout, string(dialog.PoolActionDialogPage), 2*time.Second)
	assert.Contains(t, testutil.ScreenText(t, dt.app, dt.screen), "Checking what destroying the pool tank would destroy")
	assert.Empty(t, dt.recorded())

	release()
	dt.waitForDialog(string(dialog.DestroyPoolDialogPage))
	testutil.WaitForNoPage(t, dt.app, dt.browser.layout, string(dialog.PreviewDialogPage), 2*time.Second)
	assert.Equal(t, []string{"preview tank"}, dt.recorded())
}

// Cancelled while it is checked, the confirmation is not shown once that is done.
func TestPoolBrowser_DestroyPoolCheckCanBeCancelled(t *testing.T) {
	dt := newDestroyTest(t)
	release := dt.blockPreview()
	t.Cleanup(release)

	command := commandNamed(t, dt.app, dt.browser, "Destroy pool")
	testutil.OnUiThread(t, dt.app, command.Run)
	dt.waitForDialog(string(dialog.PreviewDialogPage))
	dt.press(tcell.KeyEscape, 0)
	testutil.WaitForNoPage(t, dt.app, dt.browser.layout, string(dialog.PreviewDialogPage), 2*time.Second)
	release()

	require.Eventually(t, func() bool { return len(dt.recorded()) == 1 }, 2*time.Second, 10*time.Millisecond)
	require.Never(t, func() bool {
		return testutil.HasPage(t, dt.app, dt.browser.layout, string(dialog.DestroyPoolDialogPage)) ||
			testutil.HasPage(t, dt.app, dt.browser.layout, "ErrorDialog")
	}, 200*time.Millisecond, 10*time.Millisecond)
}
