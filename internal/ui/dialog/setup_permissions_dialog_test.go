package dialog

import (
	"errors"
	"sync/atomic"
	"testing"
	"time"
	"zfs-file-history/internal/testutil"
	"zfs-file-history/internal/util"
	"zfs-file-history/internal/zfs"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSetupCommand(t *testing.T) {
	original := executablePath
	t.Cleanup(func() { executablePath = original })

	executablePath = func() (string, error) { return "/opt/zfs-file-history", nil }
	assert.Equal(t, []string{"/opt/zfs-file-history", "setup", "--events", "--pool-actions"},
		setupCommand([]*zfs.SudoersRule{zfs.SudoersRuleEvents, zfs.SudoersRulePoolActions}))

	executablePath = func() (string, error) { return "", errors.New("unknown") }
	assert.Equal(t, []string{"zfs-file-history", "setup", "--data-errors"}, setupCommand([]*zfs.SudoersRule{zfs.SudoersRuleDataErrors}))
}

func TestFormatSetupPermissions(t *testing.T) {
	description := formatSetupPermissions([]*zfs.SudoersRule{zfs.SudoersRuleDataErrors})

	assert.Contains(t, description, "only root may run")
	assert.Contains(t, description, "zfs-file-history): zpool status -p -v.")
	assert.NotContains(t, description, "Live updates")

	assert.Equal(t, "Your user may already run all zpool commands zfs-file-history needs root for.", formatSetupPermissions(nil))
}

type setupDialogTest struct {
	rootDialogTest
	// neverAsk counts the calls of onNeverAsk
	neverAsk atomic.Int32
	// sudoersChanged counts zfs.SudoersChanged
	sudoersChanged atomic.Int32
}

// newSetupDialogTest shows the dialog with the given rules allowed already. sudo and the clipboard are never used.
func newSetupDialogTest(t *testing.T, allowed map[string]bool, automatic bool, sudoErr error) *setupDialogTest {
	dt := &setupDialogTest{rootDialogTest: rootDialogTest{permissionsDialogTest: permissionsDialogTest{t: t}}}
	originalGrant, originalCopy, originalExecutable := grantPermissions, copyToClipboard, executablePath
	originalSudoersChanged := zfs.SudoersChanged
	t.Cleanup(func() {
		grantPermissions, copyToClipboard, executablePath = originalGrant, originalCopy, originalExecutable
		zfs.SudoersChanged = originalSudoersChanged
	})
	// an emitter of this test, so its listener does not outlive it
	zfs.SudoersChanged = util.NewEmitter[struct{}]()
	zfs.SudoersChanged.Subscribe(func(struct{}) { dt.sudoersChanged.Add(1) })
	executablePath = func() (string, error) { return "/usr/bin/zfs-file-history", nil }
	grantPermissions = func(application *tview.Application, explanation string, commands [][]string) error {
		dt.recordUiThread()
		dt.mu.Lock()
		defer dt.mu.Unlock()
		dt.ran = append(dt.ran, commands...)
		return sudoErr
	}
	copyToClipboard = func(text string) error {
		dt.mu.Lock()
		defer dt.mu.Unlock()
		dt.copied = append(dt.copied, text)
		return nil
	}

	dt.app = tview.NewApplication()
	dt.screen = tcell.NewSimulationScreen("UTF-8")
	dt.app.SetScreen(dt.screen)
	dt.pages = tview.NewPages().AddPage("background", tview.NewBox(), true, true)
	dt.app.SetRoot(dt.pages, true)
	go func() { _ = dt.app.Run() }()
	t.Cleanup(dt.app.Stop)

	var onNeverAsk func()
	if automatic {
		onNeverAsk = func() { dt.neverAsk.Add(1) }
	}
	testutil.OnUiThread(t, dt.app, func() {
		ShowDialogOnPages(dt.app, dt.pages, NewSetupPermissionsDialog(dt.app, allowed, onNeverAsk), nil)
		// updates outside of input events are not drawn automatically
		dt.app.ForceDraw()
	})
	return dt
}

// waitForClosed waits until the setup dialog is closed, and the dialog with the given name shown ("": none).
func (dt *setupDialogTest) waitForClosed(name string) {
	require.Eventually(dt.t, func() bool {
		return (name == "" || dt.hasPage(name)) && !dt.hasPage(string(SetupPermissionsDialogPage))
	}, 3*time.Second, 10*time.Millisecond, "dialog %s", name)
}

// key sends a key to the dialog.
func (dt *setupDialogTest) key(key tcell.Key, r rune) {
	dt.screen.InjectKey(key, r, tcell.ModNone)
}

func TestSetupPermissionsDialog_AllowChecked(t *testing.T) {
	dt := newSetupDialogTest(t, map[string]bool{}, true, nil)
	// the rules that only read are checked
	dt.waitForText("☑ Live updates: snapshots")
	dt.waitForText("☑ Damaged files")
	dt.waitForText("☐ Pool actions")

	// from the first option up into the checklist: pool actions on, live updates off
	dt.key(tcell.KeyUp, 0)
	dt.key(tcell.KeyRune, ' ')
	dt.waitForText("☑ Pool actions")
	dt.key(tcell.KeyUp, 0)
	dt.key(tcell.KeyUp, 0)
	dt.key(tcell.KeyEnter, 0)
	dt.waitForText("☐ Live updates: snapshots")
	// down out of the checklist to the first option
	dt.key(tcell.KeyDown, 0)
	dt.key(tcell.KeyDown, 0)
	dt.key(tcell.KeyDown, 0)
	dt.key(tcell.KeyEnter, 0)

	dt.waitForClosed("SuccessDialog")
	dt.waitForText("Allowed: Damaged files, Pool actions.")
	assert.Equal(t, [][]string{{"/usr/bin/zfs-file-history", "setup", "--data-errors", "--pool-actions"}}, dt.commands())
	assert.Equal(t, int32(1), dt.sudoersChanged.Load(), "e.g. the event watcher starts again")
	assert.False(t, dt.onUiThread, "sudo runs from the background")
}

func TestSetupPermissionsDialog_AllowAll(t *testing.T) {
	dt := newSetupDialogTest(t, map[string]bool{"events": true}, false, nil)
	dt.waitForText("Allow all")

	dt.choose('2')

	dt.waitForClosed("SuccessDialog")
	dt.waitForText("Allowed: Damaged files, Pool actions.")
	assert.Equal(t, [][]string{{"/usr/bin/zfs-file-history", "setup", "--data-errors", "--pool-actions"}}, dt.commands())
}

func TestSetupPermissionsDialog_OnlyNotAllowedOnes(t *testing.T) {
	dt := newSetupDialogTest(t, map[string]bool{"events": true, "data-errors": true}, false, nil)
	dt.waitForText("☐ Pool actions")
	assert.NotContains(t, dt.screenText(), "Live updates")
	assert.NotContains(t, dt.screenText(), "Don't ask again", "only offered when shown by itself")

	// nothing checked: nothing to run, stays open
	dt.choose('1')
	time.Sleep(50 * time.Millisecond)
	assert.True(t, dt.hasPage(string(SetupPermissionsDialogPage)))
	assert.Empty(t, dt.commands())
}

func TestSetupPermissionsDialog_AllAllowed(t *testing.T) {
	dt := newSetupDialogTest(t, map[string]bool{"events": true, "data-errors": true, "pool-actions": true}, true, nil)
	dt.waitForText("Your user may already run all zpool commands")
	assert.NotContains(t, dt.screenText(), "Allow checked")
}

func TestSetupPermissionsDialog_SudoFails(t *testing.T) {
	dt := newSetupDialogTest(t, map[string]bool{}, false, errors.New("sudo /usr/bin/zfs-file-history setup: exit status 1"))
	dt.waitForText("Allow checked")

	dt.choose('1')

	dt.waitForClosed("ErrorDialog")
	dt.waitForText("exit status 1")
	assert.Equal(t, int32(0), dt.sudoersChanged.Load())
}

func TestSetupPermissionsDialog_Copy(t *testing.T) {
	dt := newSetupDialogTest(t, map[string]bool{}, false, nil)
	dt.waitForText("Copy command")

	dt.choose('3')

	dt.waitForText("Copied, run it in a terminal")
	dt.mu.Lock()
	assert.Equal(t, []string{"sudo /usr/bin/zfs-file-history setup --events --data-errors"}, dt.copied)
	dt.mu.Unlock()
	assert.Empty(t, dt.commands())
}

func TestSetupPermissionsDialog_NeverAsk(t *testing.T) {
	dt := newSetupDialogTest(t, map[string]bool{}, true, nil)
	dt.waitForText("Don't ask again")
	assert.Contains(t, dt.screenText(), "Not now")

	dt.choose('4')

	dt.waitForClosed("")
	assert.Equal(t, int32(1), dt.neverAsk.Load())
	assert.Empty(t, dt.commands())
}
