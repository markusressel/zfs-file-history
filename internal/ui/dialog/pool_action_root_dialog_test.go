package dialog

import (
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"
	"zfs-file-history/internal/testutil"
	"zfs-file-history/internal/util"
	"zfs-file-history/internal/zfs"

	"github.com/rivo/tview"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newScrubNeedsRoot() *zfs.PoolActionNeedsRootError {
	return &zfs.PoolActionNeedsRootError{Action: zfs.PoolActionScrub, Pool: "tank", Cause: errors.New("cannot scrub tank: permission denied")}
}

func TestNewErrorDialog_PoolActionNeedsRoot(t *testing.T) {
	app, _ := testutil.NewApp()

	assert.Equal(t, "PoolActionRootDialog", NewErrorDialog(app, "Failed", newScrubNeedsRoot()).GetName())
	// also if wrapped
	assert.Equal(t, "PoolActionRootDialog", NewErrorDialog(app, "Failed", fmt.Errorf("failed: %w", newScrubNeedsRoot())).GetName())
}

func TestFormatPoolActionNeedsRoot(t *testing.T) {
	description := formatPoolActionNeedsRoot(newScrubNeedsRoot(), []string{"/usr/bin/zfs-file-history", "setup", "--pool-actions"}, true)

	assert.Equal(t, "To start a scrub on tank, zpool has to run as root: ZFS cannot delegate pool operations to users "+
		"like it does for datasets (zfs allow)."+
		"\n\nReason: cannot scrub tank: permission denied"+
		"\n\nYour user can be allowed to scrub, TRIM, upgrade and clear the errors of pools without a password "+
		"(nothing else, e.g. no destroying) with:\n"+
		"\n  sudo /usr/bin/zfs-file-history setup --pool-actions", description)
}

func TestSetupPoolActionsCommand(t *testing.T) {
	testutil.Replace(t, &executablePath, func() (string, error) { return "/opt/zfs-file-history", nil })
	assert.Equal(t, []string{"/opt/zfs-file-history", "setup", "--pool-actions"}, setupPoolActionsCommand())

	testutil.Replace(t, &executablePath, func() (string, error) { return "", errors.New("unknown") })
	assert.Equal(t, []string{"zfs-file-history", "setup", "--pool-actions"}, setupPoolActionsCommand())
}

type rootDialogTest struct {
	permissionsDialogTest
	// ran are the commands run with sudo
	ran [][]string
	// poolsChanged counts zfs.PoolsChanged
	poolsChanged atomic.Int32
}

// newRootDialogTest shows the dialog for newScrubNeedsRoot, with a retry function if withRetry is set. sudo and the
// clipboard are never used.
func newRootDialogTest(t *testing.T, withRetry bool, sudoErr error) *rootDialogTest {
	return newRootDialogTestFor(t, newScrubNeedsRoot(), withRetry, sudoErr)
}

// newRootDialogTestFor is newRootDialogTest for the given error.
func newRootDialogTestFor(t *testing.T, needsRoot *zfs.PoolActionNeedsRootError, withRetry bool, sudoErr error) *rootDialogTest {
	dt := &rootDialogTest{permissionsDialogTest: permissionsDialogTest{t: t}}
	// an emitter of this test, so its listener does not outlive it
	testutil.Replace(t, &zfs.PoolsChanged, util.NewEmitter[struct{}]())
	zfs.PoolsChanged.Subscribe(func(struct{}) { dt.poolsChanged.Add(1) })
	testutil.Replace(t, &executablePath, func() (string, error) { return "/usr/bin/zfs-file-history", nil })
	testutil.Replace(t, &grantPermissions, func(application *tview.Application, explanation string, commands [][]string) error {
		dt.recordUiThread()
		dt.mu.Lock()
		defer dt.mu.Unlock()
		dt.ran = append(dt.ran, commands...)
		return sudoErr
	})
	testutil.Replace(t, &copyToClipboard, func(text string) error {
		dt.mu.Lock()
		defer dt.mu.Unlock()
		dt.copied = append(dt.copied, text)
		return nil
	})

	dt.app, dt.screen = testutil.NewApp()
	dt.pages = tview.NewPages().AddPage("background", tview.NewBox(), true, true)
	testutil.StartApp(t, dt.app, dt.pages)

	var retry func()
	if withRetry {
		retry = func() {
			dt.mu.Lock()
			defer dt.mu.Unlock()
			dt.retried++
		}
	}
	testutil.OnUiThread(t, dt.app, func() {
		ShowDialogOnPages(dt.app, dt.pages, NewErrorDialogWithRetry(dt.app, "Operation Failed", needsRoot, retry), nil)
		// updates outside of input events are not drawn automatically
		dt.app.ForceDraw()
	})
	return dt
}

func (dt *rootDialogTest) commands() [][]string {
	dt.mu.Lock()
	defer dt.mu.Unlock()
	return append([][]string{}, dt.ran...)
}

// waitForFollowUp waits until the dialog with the given name replaced the root dialog.
func (dt *rootDialogTest) waitForFollowUp(name string) {
	require.Eventually(dt.t, func() bool {
		return dt.hasPage(name) && !dt.hasPage("PoolActionRootDialog")
	}, 3*time.Second, 10*time.Millisecond, "dialog %s", name)
}

func TestPoolActionRootDialog_RunOnce(t *testing.T) {
	dt := newRootDialogTest(t, true, nil)
	dt.waitForText("Run once with sudo")

	dt.choose('1')

	dt.waitForFollowUp("SuccessDialog")
	dt.waitForText("Started a scrub of tank.")
	assert.Equal(t, [][]string{{zfs.ZpoolPath(), "scrub", "tank"}}, dt.commands())
	assert.Equal(t, int32(1), dt.poolsChanged.Load(), "the pools are reloaded")
	_, retried := dt.counts()
	assert.Equal(t, 0, retried, "done, nothing to retry")
	assert.False(t, dt.onUiThread, "sudo runs from the background")
}

func TestPoolActionRootDialog_AllowAndRetry(t *testing.T) {
	dt := newRootDialogTest(t, true, nil)
	dt.waitForText("Always allow and retry")

	dt.choose('2')

	require.Eventually(t, func() bool {
		_, retried := dt.counts()
		return retried == 1 && !dt.hasPage("PoolActionRootDialog")
	}, 3*time.Second, 10*time.Millisecond, "the dialog closes and the action is retried")
	assert.Equal(t, [][]string{{"/usr/bin/zfs-file-history", "setup", "--pool-actions"}}, dt.commands())
	assert.False(t, dt.hasPage("SuccessDialog"), "the retried action shows its own result")
}

func TestPoolActionRootDialog_AllowWithoutRetry(t *testing.T) {
	dt := newRootDialogTest(t, false, nil)
	dt.waitForText("Always allow")
	assert.NotContains(t, dt.screenText(), "Retry")

	dt.choose('2')

	dt.waitForFollowUp("SuccessDialog")
	dt.waitForText("Please try again")
}

func TestPoolActionRootDialog_SudoFails(t *testing.T) {
	dt := newRootDialogTest(t, true, errors.New("sudo /usr/sbin/zpool scrub tank: exit status 1"))

	dt.choose('1')

	dt.waitForFollowUp("ErrorDialog")
	dt.waitForText("exit status 1")
	assert.Equal(t, int32(0), dt.poolsChanged.Load())
}

func TestPoolActionRootDialog_CopyAndRetry(t *testing.T) {
	dt := newRootDialogTest(t, true, nil)

	dt.choose('3')
	dt.waitForText("Copied, run it in a terminal")
	assert.True(t, dt.hasPage("PoolActionRootDialog"), "stays open to retry")
	dt.mu.Lock()
	assert.Equal(t, []string{"sudo /usr/bin/zfs-file-history setup --pool-actions"}, dt.copied)
	dt.mu.Unlock()

	dt.choose('4')
	require.Eventually(t, func() bool {
		_, retried := dt.counts()
		return retried == 1
	}, 3*time.Second, 10*time.Millisecond)
	assert.Empty(t, dt.commands(), "nothing run with sudo")
}

// newFeaturesNeedsRoot is the error of enabling two features without root, see zfs.EnablePoolFeatures.
func newFeaturesNeedsRoot() *zfs.PoolActionNeedsRootError {
	return &zfs.PoolActionNeedsRootError{
		Action: zfs.PoolAction{Name: "enable the features bookmarks, large_blocks", Done: "Enabled the features bookmarks, large_blocks",
			Args: zfs.PoolActionEnableFeature("bookmarks").Args},
		Then:  []zfs.PoolAction{zfs.PoolActionEnableFeature("large_blocks")},
		Pool:  "tank",
		Cause: errors.New("cannot set property for 'tank': permission denied"),
	}
}

// The setup does not allow enabling features: only running them once, or copying the commands, is offered.
func TestPoolActionRootDialog_NotAllowedBySetup(t *testing.T) {
	dt := newRootDialogTestFor(t, newFeaturesNeedsRoot(), true, nil)
	dt.waitForText("Run once with sudo")
	text := dt.screenText()
	assert.NotContains(t, text, "Always allow")
	assert.NotContains(t, text, "setup --pool-actions")

	dt.choose('2')
	dt.waitForText("Copied, run it in a terminal")
	dt.mu.Lock()
	assert.Equal(t, []string{"sudo " + zfs.ZpoolPath() + " set feature@bookmarks=enabled tank && sudo " +
		zfs.ZpoolPath() + " set feature@large_blocks=enabled tank"}, dt.copied)
	dt.mu.Unlock()

	dt.choose('1')
	dt.waitForFollowUp("SuccessDialog")
	dt.waitForText("Enabled the features bookmarks, large_blocks of tank.")
	assert.Equal(t, [][]string{
		{zfs.ZpoolPath(), "set", "feature@bookmarks=enabled", "tank"},
		{zfs.ZpoolPath(), "set", "feature@large_blocks=enabled", "tank"},
	}, dt.commands(), "both with one password")
}

func TestFormatPoolActionNeedsRoot_NotAllowedBySetup(t *testing.T) {
	description := formatPoolActionNeedsRoot(newFeaturesNeedsRoot(), []string{"zfs-file-history", "setup", "--pool-actions"}, false)

	assert.Contains(t, description, "To enable the features bookmarks, large_blocks on tank, zpool has to run as root")
	assert.Contains(t, description, "it cannot be undone, so sudo asks every time")
	assert.NotContains(t, description, "setup")
}
