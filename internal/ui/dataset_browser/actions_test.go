package dataset_browser

import (
	"fmt"
	"strings"
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

// actionTest runs a browser with a clone "pool/clone" (mounted), a dataset "pool/data" and the root dataset of the
// pool, and records the calls of the actions instead of changing anything.
type actionTest struct {
	t       *testing.T
	app     *tview.Application
	browser *DatasetBrowserComponent
	screen  tcell.SimulationScreen

	mu sync.Mutex
	// calls are the calls of the actions, e.g. "destroy pool/clone recursive=false"
	calls []string
	// destroyPreview is the dry run of destroying a dataset
	destroyPreview *zfs.DestroyPreview
}

func newActionTest(t *testing.T) *actionTest {
	at := &actionTest{
		t:              t,
		destroyPreview: &zfs.DestroyPreview{Destroyed: []string{"pool/clone"}, Reclaim: 1024},
	}
	setListDatasets(t, func() ([]*zfs.DatasetListEntry, error) {
		return []*zfs.DatasetListEntry{
			{Name: "pool", MountPath: "/pool"},
			{Name: "pool/clone", MountPath: "/clone", Origin: "pool/data@daily"},
			{Name: "pool/data", MountPath: "/data"},
		}, nil
	})
	testutil.Replace(t, &createSnapshot, func(dataset string, name string, recursive bool) error {
		at.record("snapshot %s recursive=%t", dataset, recursive)
		return nil
	})
	testutil.Replace(t, &previewPromote, func(name string) (*zfs.PromotePreview, error) {
		at.record("preview promote %s", name)
		return &zfs.PromotePreview{Clone: name, Origin: "pool/data@daily", Moved: []string{"pool/data@daily"}}, nil
	})
	testutil.Replace(t, &promoteClone, func(name string) error {
		at.record("promote %s", name)
		return nil
	})
	testutil.Replace(t, &previewDestroyDataset, func(name string) (*zfs.DestroyPreview, error) {
		at.record("preview destroy %s", name)
		return at.destroyPreview, nil
	})
	testutil.Replace(t, &destroyDataset, func(name string, recursive bool, confirmed *zfs.DestroyPreview) error {
		at.record("destroy %s recursive=%t", name, recursive)
		assert.Same(t, at.destroyPreview, confirmed, "destroys what was confirmed")
		return nil
	})

	at.app, at.browser, at.screen = newBrowserApp(t)
	testutil.OnUiThread(t, at.app, func() { at.browser.Refresh(false) })
	require.Eventually(t, func() bool {
		count := 0
		testutil.OnUiThread(t, at.app, func() { count = len(at.browser.tableContainer.GetEntries()) })
		return count == 3
	}, 2*time.Second, 10*time.Millisecond)
	return at
}

func (at *actionTest) record(format string, args ...any) {
	at.mu.Lock()
	defer at.mu.Unlock()
	at.calls = append(at.calls, fmt.Sprintf(format, args...))
}

func (at *actionTest) recorded() []string {
	at.mu.Lock()
	defer at.mu.Unlock()
	return append([]string(nil), at.calls...)
}

func (at *actionTest) selectDataset(name string) {
	testutil.OnUiThread(at.t, at.app, func() {
		at.browser.tableContainer.Select(findByName(at.browser.tableContainer.GetEntries(), name))
	})
}

func (at *actionTest) press(key tcell.Key, r rune) {
	testutil.Press(at.t, at.app, at.screen, key, r)
}

func (at *actionTest) typeText(text string) {
	testutil.Type(at.t, at.app, at.screen, text)
}

// screenText returns the text on the screen, a line per row.
func (at *actionTest) screenText() string {
	return testutil.ScreenText(at.t, at.app, at.screen)
}

func (at *actionTest) hasDialog(name string) bool {
	return testutil.HasPage(at.t, at.app, at.browser.layout, name)
}

func (at *actionTest) waitForDialog(name string) {
	require.Eventually(at.t, func() bool { return at.hasDialog(name) }, 2*time.Second, 10*time.Millisecond, "dialog %s", name)
}

func (at *actionTest) waitForNoDialog(name string) {
	require.Eventually(at.t, func() bool { return !at.hasDialog(name) }, 2*time.Second, 10*time.Millisecond, "dialog %s", name)
}

// chooseAction opens the actions of the selected dataset and chooses the option with the given number.
func (at *actionTest) chooseAction(number rune) {
	at.press(tcell.KeyEnter, 0)
	at.waitForDialog(string(dialog.DatasetActionDialogPage))
	at.press(tcell.KeyRune, number)
	at.press(tcell.KeyEnter, 0)
}

// settle waits until the event loop processed the injected keys and the background work finished.
func (at *actionTest) settle() {
	testutil.OnUiThread(at.t, at.app, func() {})
	time.Sleep(50 * time.Millisecond)
	testutil.OnUiThread(at.t, at.app, func() {})
}

// the options of a clone (mounted): 1 open, 2 snapshot, 3 snapshot (recursive), 4 permissions, 5 properties,
// 6 promote, 7 destroy; of other datasets the same without promote
const (
	optionSnapshotRecursive = '3'
	optionPromote           = '6'
	optionDestroy           = '7'
	optionDestroyNotAClone  = '6'
)

func TestDatasetBrowser_DestroyClone(t *testing.T) {
	at := newActionTest(t)
	at.selectDataset("pool/clone")

	at.chooseAction(optionDestroy)
	at.waitForDialog("DestroyDatasetByNameDialog")
	assert.Equal(t, []string{"preview destroy pool/clone"}, at.recorded())

	// Enter alone, or a wrong name, destroys nothing
	at.press(tcell.KeyEnter, 0)
	at.typeText("pool/clon")
	at.press(tcell.KeyEnter, 0)
	at.settle()
	assert.True(t, at.hasDialog("DestroyDatasetByNameDialog"))
	assert.Equal(t, []string{"preview destroy pool/clone"}, at.recorded())

	at.typeText("e")
	at.press(tcell.KeyEnter, 0)
	at.waitForDialog("SuccessDialog")
	assert.Equal(t, []string{"preview destroy pool/clone", "destroy pool/clone recursive=false"}, at.recorded())
}

func TestDatasetBrowser_DestroyCanBeCancelled(t *testing.T) {
	at := newActionTest(t)
	at.selectDataset("pool/clone")

	at.chooseAction(optionDestroy)
	at.waitForDialog("DestroyDatasetByNameDialog")
	at.typeText("pool/clone")
	at.press(tcell.KeyEscape, 0)
	at.waitForNoDialog("DestroyDatasetByNameDialog")
	at.settle()

	assert.Equal(t, []string{"preview destroy pool/clone"}, at.recorded())
}

func TestDatasetBrowser_DestroyWithSnapshotsNeedsTheCheckbox(t *testing.T) {
	at := newActionTest(t)
	at.destroyPreview = &zfs.DestroyPreview{Destroyed: []string{"pool/clone@x", "pool/clone/child", "pool/clone"}, Reclaim: 1024}
	at.selectDataset("pool/clone")

	// without the checkbox: nothing is destroyed
	at.chooseAction(optionDestroy)
	at.waitForDialog(string(dialog.DestroyDatasetDialogPage))
	require.Eventually(t, func() bool {
		return strings.Contains(at.screenText(), "Also destroy its 1 child dataset and 1 snapshot")
	}, 2*time.Second, 10*time.Millisecond)
	at.press(tcell.KeyRune, '1') // continue
	at.press(tcell.KeyEnter, 0)
	at.waitForDialog("ErrorDialog")
	assert.False(t, at.hasDialog("DestroyDatasetByNameDialog"))
	at.press(tcell.KeyEscape, 0)
	at.waitForNoDialog("ErrorDialog")

	// with it, the name is asked for
	at.chooseAction(optionDestroy)
	at.waitForDialog(string(dialog.DestroyDatasetDialogPage))
	at.press(tcell.KeyUp, 0) // to the checklist
	at.press(tcell.KeyRune, ' ')
	at.press(tcell.KeyRune, '1') // continue
	at.press(tcell.KeyEnter, 0)
	at.waitForDialog("DestroyDatasetByNameDialog")
	assert.Len(t, at.recorded(), 2, "only the dry runs")

	at.typeText("pool/clone")
	at.press(tcell.KeyEnter, 0)
	at.waitForDialog("SuccessDialog")
	assert.Equal(t, "destroy pool/clone recursive=true", at.recorded()[2])
}

// Regression: only clones could be destroyed. All datasets can, except the root dataset of a pool, which is only
// destroyed with the pool; only clones can be promoted.
func TestDatasetBrowser_DatasetsCanBeDestroyed(t *testing.T) {
	at := newActionTest(t)

	commandNames := func(dataset string) string {
		at.selectDataset(dataset)
		var names []string
		testutil.OnUiThread(t, at.app, func() {
			for _, command := range at.browser.GetCommands() {
				names = append(names, command.Name)
			}
		})
		return strings.Join(names, "|")
	}

	assert.Contains(t, commandNames("pool/clone"), "Destroy dataset")
	assert.Contains(t, commandNames("pool/clone"), "Promote clone")
	assert.Contains(t, commandNames("pool/data"), "Destroy dataset")
	assert.NotContains(t, commandNames("pool/data"), "Promote")
	assert.NotContains(t, commandNames("pool"), "Destroy")
}

// A dataset that is not a clone is destroyed the same way: after a dry run, which the confirmation shows, and its
// full name typed.
func TestDatasetBrowser_DestroyDataset(t *testing.T) {
	at := newActionTest(t)
	at.destroyPreview = &zfs.DestroyPreview{Destroyed: []string{"pool/data"}, Reclaim: 1024}
	at.selectDataset("pool/data")

	at.chooseAction(optionDestroyNotAClone)
	at.waitForDialog("DestroyDatasetByNameDialog")
	text := at.screenText()
	assert.Contains(t, text, "Destroy the dataset pool/data with all its files")
	assert.Contains(t, text, "cannot be undone")
	assert.Contains(t, text, "It is unmounted from /data first")

	at.typeText("pool/data")
	at.press(tcell.KeyEnter, 0)
	at.waitForDialog("SuccessDialog")
	assert.Equal(t, []string{"preview destroy pool/data", "destroy pool/data recursive=false"}, at.recorded())
}

// Regression: the confirmation listed what would be destroyed in the order of the dry run, so the child datasets
// were hidden behind "… and 12 more" snapshots.
func TestDatasetBrowser_DestroyListsTheDatasetsFirst(t *testing.T) {
	at := newActionTest(t)
	destroyed := []string{"pool/data/child"}
	for i := range 12 {
		destroyed = append(destroyed, fmt.Sprintf("pool/data@snap%d", i))
	}
	at.destroyPreview = &zfs.DestroyPreview{Destroyed: append(destroyed, "pool/data"), Reclaim: 1024}
	at.selectDataset("pool/data")

	at.chooseAction(optionDestroyNotAClone)
	at.waitForDialog(string(dialog.DestroyDatasetDialogPage))
	require.Eventually(t, func() bool {
		return strings.Contains(at.screenText(), "Also destroy its 1 child dataset and 12 snapshots")
	}, 2*time.Second, 10*time.Millisecond)
	testutil.OnUiThread(t, at.app, func() {
		description := formatDestroyDescription(&zfs.DatasetListEntry{Name: "pool/data"}, at.destroyPreview)
		assert.Contains(t, description, "Datasets that will be destroyed (2)")
		assert.Contains(t, description, "pool/data/child")
		assert.Contains(t, description, "Snapshots that will be destroyed (12)")
	})
}

func TestDatasetBrowser_PromoteClone(t *testing.T) {
	at := newActionTest(t)
	at.selectDataset("pool/clone")

	at.chooseAction(optionPromote)
	at.waitForDialog(string(dialog.PromoteCloneDialogPage))
	assert.Equal(t, []string{"preview promote pool/clone"}, at.recorded())

	at.press(tcell.KeyRune, '1') // promote
	at.press(tcell.KeyEnter, 0)
	at.waitForNoDialog(string(dialog.PromoteCloneDialogPage))
	require.Eventually(t, func() bool { return len(at.recorded()) == 2 }, 2*time.Second, 10*time.Millisecond)
	assert.Equal(t, "promote pool/clone", at.recorded()[1])
}

func TestDatasetBrowser_CreateSnapshotRecursively(t *testing.T) {
	at := newActionTest(t)
	at.selectDataset("pool/clone")

	at.chooseAction(optionSnapshotRecursive)

	require.Eventually(t, func() bool { return len(at.recorded()) == 1 }, 2*time.Second, 10*time.Millisecond)
	assert.Equal(t, "snapshot pool/clone recursive=true", at.recorded()[0])
}

func TestDatasetBrowser_OpenInFileBrowser(t *testing.T) {
	at := newActionTest(t)
	var opened []string
	at.browser.Events.Subscribe(func(event Event) {
		if e, ok := event.(OpenInFileBrowserEvent); ok {
			opened = append(opened, e.Path)
		}
	})
	at.selectDataset("pool/data")

	at.chooseAction('1')

	require.Eventually(t, func() bool {
		var result []string
		testutil.OnUiThread(t, at.app, func() { result = append(result, opened...) })
		return len(result) == 1 && result[0] == "/data"
	}, 2*time.Second, 10*time.Millisecond)
	assert.Empty(t, at.recorded())
}
