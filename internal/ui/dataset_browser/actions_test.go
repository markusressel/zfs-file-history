package dataset_browser

import (
	"errors"
	"fmt"
	"slices"
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

// actionTest runs a browser with a clone "pool/clone" (mounted), a dataset "pool/data", the root dataset of the
// pool, an unmounted dataset "pool/later", an encrypted one with its key loaded "pool/secret" (mounted) and one
// without "pool/locked", and records the calls of the actions instead of changing anything.
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
	// mountPlans are the plans of the mount actions, by action and dataset
	mountPlans map[zfs.MountAction]map[string]*zfs.MountPlan
	// mountErr is returned by runMountPlan, terminalErr by running a plan in the terminal
	mountErr    error
	terminalErr error
}

func newActionTest(t *testing.T) *actionTest {
	at := &actionTest{
		t:              t,
		destroyPreview: &zfs.DestroyPreview{Destroyed: []string{"pool/clone"}, Reclaim: 1024},
	}
	datasets := []*zfs.DatasetListEntry{
		{Name: "pool", MountPath: "/pool"},
		{Name: "pool/clone", MountPath: "/clone", Origin: "pool/data@daily"},
		{Name: "pool/data", MountPath: "/data"},
		{Name: "pool/later", CanMount: "noauto", Mountpoint: "/later"},
		{Name: "pool/secret", CanMount: "on", Mountpoint: "/secret", MountPath: "/secret",
			KeyStatus: "available", EncryptionRoot: "pool/secret", KeyLocation: "prompt"},
		{Name: "pool/locked", CanMount: "on", Mountpoint: "/locked",
			KeyStatus: "unavailable", EncryptionRoot: "pool/locked", KeyLocation: "prompt"},
	}
	setListDatasets(t, func() ([]*zfs.DatasetListEntry, error) { return datasets, nil })
	at.mountPlans = map[zfs.MountAction]map[string]*zfs.MountPlan{
		zfs.MountActionMount: {
			"pool/later": {Action: zfs.MountActionMount, Dataset: "pool/later", MountPath: "/later",
				Commands: [][]string{{"mount", "pool/later"}}},
			"pool/locked": {Action: zfs.MountActionMount, Dataset: "pool/locked", MountPath: "/locked", EncryptionRoot: "pool/locked",
				Commands: [][]string{{"mount", "-l", "pool/locked"}}, Prompts: true, NeedsRoot: true},
		},
		zfs.MountActionUnmount: {
			"pool/data": {Action: zfs.MountActionUnmount, Dataset: "pool/data", MountPath: "/data",
				Unmounted: []zfs.Mount{{Name: "pool/data/child", Mountpoint: "/data/child"}, {Name: "pool/data", Mountpoint: "/data"}},
				Commands:  [][]string{{"unmount", "pool/data/child"}, {"unmount", "pool/data"}}},
		},
		zfs.MountActionLoadKey: {
			"pool/locked": {Action: zfs.MountActionLoadKey, Dataset: "pool/locked", EncryptionRoot: "pool/locked",
				Commands: [][]string{{"load-key", "pool/locked"}}, Prompts: true},
		},
		zfs.MountActionUnloadKey: {
			"pool/secret": {Action: zfs.MountActionUnloadKey, Dataset: "pool/secret", EncryptionRoot: "pool/secret",
				Unmounted: []zfs.Mount{{Name: "pool/secret", Mountpoint: "/secret"}},
				Commands:  [][]string{{"unmount", "pool/secret"}, {"unload-key", "pool/secret"}}},
		},
	}
	testutil.Replace(t, &planMountAction, func(action zfs.MountAction, dataset string) (*zfs.MountPlan, error) {
		at.record("plan %d %s", action, dataset)
		plan, ok := at.mountPlans[action][dataset]
		if !ok {
			return nil, fmt.Errorf("no plan for %d %s", action, dataset)
		}
		return plan, nil
	})
	testutil.Replace(t, &runMountPlan, func(plan *zfs.MountPlan) error {
		at.record("run %s", plan.Name())
		assert.Same(t, at.mountPlans[plan.Action][plan.Dataset], plan, "runs what was confirmed")
		return at.mountErr
	})
	terminal := func(sudo bool) func(*tview.Application, string, [][]string) error {
		return func(application *tview.Application, explanation string, commands [][]string) error {
			at.record("terminal sudo=%t %v: %s", sudo, commands, explanation)
			return at.terminalErr
		}
	}
	testutil.Replace(t, &runInTerminal, terminal(false))
	testutil.Replace(t, &runWithSudoInTerminal, terminal(true))
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
	testutil.OnUiThread(t, at.app, func() {
		if at.browser.IsHidingUnmounted() {
			at.browser.ToggleHideUnmounted()
		}
		at.browser.Refresh(false)
	})
	require.Eventually(t, func() bool {
		count := 0
		testutil.OnUiThread(t, at.app, func() { count = len(at.browser.tableContainer.GetEntries()) })
		return count == len(datasets)
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
// 6 unmount, 7 promote, 8 destroy; of other mounted datasets the same without promote, with 7 unload key if
// encrypted. Of datasets that are not mounted: 1 snapshot, 2 snapshot (recursive), 3 permissions, 4 properties,
// 5 mount (or load key and mount), 6 load key if it is not loaded.
const (
	optionSnapshotRecursive = '3'
	optionUnmount           = '6'
	optionPromote           = '7'
	optionDestroy           = '8'
	optionDestroyNotAClone  = '7'
	optionUnloadKey         = '7'
	optionMount             = '5'
	optionLoadKey           = '6'
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

	commandNames := func(dataset string) string { return strings.Join(at.commandNames(dataset), "|") }

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

// commandNames returns the names of the commands of the browser with the dataset selected.
func (at *actionTest) commandNames(dataset string) []string {
	at.selectDataset(dataset)
	var names []string
	testutil.OnUiThread(at.t, at.app, func() {
		for _, command := range at.browser.GetCommands() {
			names = append(names, command.Name)
		}
	})
	return names
}

// waitForRecorded waits until the calls are the given ones.
func (at *actionTest) waitForRecorded(calls ...string) {
	require.Eventually(at.t, func() bool { return slices.Equal(calls, at.recorded()) }, 2*time.Second, 10*time.Millisecond,
		"calls %v", calls)
}

// Only the mount actions that zfs would not refuse right away are offered.
func TestDatasetBrowser_MountOptions(t *testing.T) {
	at := newActionTest(t)

	mountCommands := func(dataset string) []string {
		return slices.DeleteFunc(at.commandNames(dataset), func(name string) bool {
			return !slices.Contains([]string{"Mount", "Unmount", "Load key and mount", "Load key", "Unload key"}, name)
		})
	}
	assert.Equal(t, []string{"Unmount"}, mountCommands("pool/data"))
	assert.Equal(t, []string{"Mount"}, mountCommands("pool/later"))
	assert.Equal(t, []string{"Unmount", "Unload key"}, mountCommands("pool/secret"))
	assert.Equal(t, []string{"Load key and mount", "Load key"}, mountCommands("pool/locked"))
}

func TestDatasetBrowser_Mount(t *testing.T) {
	at := newActionTest(t)
	at.selectDataset("pool/later")

	at.chooseAction(optionMount)

	at.waitForRecorded("plan 0 pool/later", "run mount pool/later")
}

func TestDatasetBrowser_MountNeedsRoot(t *testing.T) {
	at := newActionTest(t)
	at.mountErr = &zfs.DatasetCommandNeedsRootError{Action: "mount pool/later", Done: "Mounted pool/later at /later",
		Commands: [][]string{{"/usr/bin/zfs", "mount", "pool/later"}}, Cause: errors.New("cannot mount 'pool/later': Insufficient privileges")}
	at.selectDataset("pool/later")

	at.chooseAction(optionMount)

	at.waitForDialog(string(dialog.DatasetCommandRootDialogPage))
	assert.Contains(t, at.screenText(), "sudo /usr/bin/zfs mount pool/later")
}

// zfs asks for the passphrase in the terminal, so the application never sees it; with sudo if it needs root.
func TestDatasetBrowser_LoadKeyAndMountInTheTerminal(t *testing.T) {
	at := newActionTest(t)
	at.selectDataset("pool/locked")

	at.chooseAction(optionMount)

	at.waitForRecorded("plan 0 pool/locked", "terminal sudo=true [["+zfs.ZfsPath()+" mount -l pool/locked]]: loading the key "+
		"of pool/locked and mounting pool/locked: zfs asks for its passphrase. It runs as root, sudo asks for your password first.")
}

func TestDatasetBrowser_LoadKeyInTheTerminal(t *testing.T) {
	at := newActionTest(t)
	at.selectDataset("pool/locked")

	at.chooseAction(optionLoadKey)

	at.waitForRecorded("plan 2 pool/locked", "terminal sudo=false [["+zfs.ZfsPath()+" load-key pool/locked]]: loading the key "+
		"of pool/locked: zfs asks for its passphrase.")
	assert.False(t, at.hasDialog("ErrorDialog"))
}

// A failure in the terminal (e.g. a wrong passphrase) is shown.
func TestDatasetBrowser_LoadKeyFails(t *testing.T) {
	at := newActionTest(t)
	at.terminalErr = errors.New("/usr/bin/zfs load-key pool/locked: exit status 255")
	at.selectDataset("pool/locked")

	at.chooseAction(optionLoadKey)

	at.waitForDialog("ErrorDialog")
	assert.Contains(t, at.screenText(), "exit status 255")
}

func TestDatasetBrowser_Unmount(t *testing.T) {
	at := newActionTest(t)
	at.selectDataset("pool/data")

	// asks first, with what is unmounted
	at.chooseAction(optionUnmount)
	at.waitForDialog(string(dialog.UnmountDialogPage))
	text := at.screenText()
	assert.Contains(t, text, "Unmount pool/data from /data.")
	assert.Contains(t, text, "pool/data/child (/data/child)")
	assert.Equal(t, []string{"plan 1 pool/data"}, at.recorded())

	at.press(tcell.KeyRune, '1') // unmount
	at.press(tcell.KeyEnter, 0)
	at.waitForRecorded("plan 1 pool/data", "run unmount pool/data")
}

func TestDatasetBrowser_UnmountCanBeCancelled(t *testing.T) {
	at := newActionTest(t)
	at.selectDataset("pool/data")

	at.chooseAction(optionUnmount)
	at.waitForDialog(string(dialog.UnmountDialogPage))
	at.press(tcell.KeyEscape, 0)
	at.waitForNoDialog(string(dialog.UnmountDialogPage))
	testutil.WaitForUpdates(t, at.app)

	assert.Equal(t, []string{"plan 1 pool/data"}, at.recorded())
}

// A failed unmount (e.g. a dataset in use) is shown.
func TestDatasetBrowser_UnmountFails(t *testing.T) {
	at := newActionTest(t)
	at.mountErr = errors.New("cannot unmount '/data': pool or dataset is busy")
	at.selectDataset("pool/data")

	at.chooseAction(optionUnmount)
	at.waitForDialog(string(dialog.UnmountDialogPage))
	at.press(tcell.KeyRune, '1') // unmount
	at.press(tcell.KeyEnter, 0)
	at.waitForDialog("ErrorDialog")
	assert.Contains(t, at.screenText(), "pool or dataset is busy")
	assert.Equal(t, []string{"plan 1 pool/data", "run unmount pool/data"}, at.recorded())
}

// A retry after running the commands of the root dialog yourself plans again, and asks again before unmounting.
func TestDatasetBrowser_UnmountRetryAsksAgain(t *testing.T) {
	at := newActionTest(t)
	at.mountErr = &zfs.DatasetCommandNeedsRootError{Action: "unmount pool/data", Done: "Unmounted pool/data",
		Commands: [][]string{{"/usr/bin/zfs", "unmount", "pool/data"}}, Cause: errors.New("permission denied")}
	at.selectDataset("pool/data")

	at.chooseAction(optionUnmount)
	at.waitForDialog(string(dialog.UnmountDialogPage))
	at.press(tcell.KeyRune, '1') // unmount
	at.press(tcell.KeyEnter, 0)
	at.waitForDialog(string(dialog.DatasetCommandRootDialogPage))
	at.press(tcell.KeyRune, '3') // retry
	at.press(tcell.KeyEnter, 0)

	at.waitForDialog(string(dialog.UnmountDialogPage))
	assert.Equal(t, []string{"plan 1 pool/data", "run unmount pool/data", "plan 1 pool/data"}, at.recorded())
}

func TestDatasetBrowser_UnloadKey(t *testing.T) {
	at := newActionTest(t)
	at.selectDataset("pool/secret")

	at.chooseAction(optionUnloadKey)
	at.waitForDialog(string(dialog.UnmountDialogPage))
	text := at.screenText()
	assert.Contains(t, text, "Unload Key")
	assert.Contains(t, text, "Unload the key of pool/secret.")
	assert.Contains(t, text, "pool/secret (/secret)")

	at.press(tcell.KeyRune, '1') // unload key
	at.press(tcell.KeyEnter, 0)
	at.waitForRecorded("plan 3 pool/secret", "run unload the key of pool/secret")
}

// blockDestroyPreview makes the dry run of destroying a dataset wait until the returned function is called.
func (at *actionTest) blockDestroyPreview() (release func()) {
	released := make(chan struct{})
	testutil.Replace(at.t, &previewDestroyDataset, func(name string) (*zfs.DestroyPreview, error) {
		<-released
		at.record("preview destroy %s", name)
		return at.destroyPreview, nil
	})
	return sync.OnceFunc(func() { close(released) })
}

// Regression: choosing "Destroy dataset" ran the dry run in the action dialog, with a spinner next to the option,
// which looked like the dataset was destroyed without a confirmation. The dialog of the confirmation opens right away
// and says what is checked; the confirmation follows once the dry run is done.
func TestDatasetBrowser_DestroyChecksInTheConfirmation(t *testing.T) {
	at := newActionTest(t)
	release := at.blockDestroyPreview()
	t.Cleanup(release)
	at.selectDataset("pool/clone")

	at.chooseAction(optionDestroy)

	at.waitForDialog(string(dialog.PreviewDialogPage))
	at.waitForNoDialog(string(dialog.DatasetActionDialogPage))
	assert.Contains(t, at.screenText(), "Checking what destroying pool/clone would destroy")
	assert.Empty(t, at.recorded())

	release()
	at.waitForDialog("DestroyDatasetByNameDialog")
	at.waitForNoDialog(string(dialog.PreviewDialogPage))
	assert.Equal(t, []string{"preview destroy pool/clone"}, at.recorded())
}

// Cancelled while it is checked, the confirmation is not shown once the dry run is done.
func TestDatasetBrowser_DestroyCheckCanBeCancelled(t *testing.T) {
	at := newActionTest(t)
	release := at.blockDestroyPreview()
	t.Cleanup(release)
	at.selectDataset("pool/clone")

	at.chooseAction(optionDestroy)
	at.waitForDialog(string(dialog.PreviewDialogPage))
	at.press(tcell.KeyEscape, 0)
	at.waitForNoDialog(string(dialog.PreviewDialogPage))
	release()

	require.Eventually(t, func() bool { return len(at.recorded()) == 1 }, 2*time.Second, 10*time.Millisecond)
	require.Never(t, func() bool { return at.hasDialog("DestroyDatasetByNameDialog") || at.hasDialog("ErrorDialog") },
		200*time.Millisecond, 10*time.Millisecond)
}
