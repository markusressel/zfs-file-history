package dialog

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
	"zfs-file-history/internal/testutil"
	"zfs-file-history/internal/zfs"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newMissingRelease() *zfs.MissingPermissionsError {
	return &zfs.MissingPermissionsError{
		Action: "release holds",
		User:   "alice",
		Missing: []zfs.PermissionGap{
			{Dataset: "pool/data", Permissions: []zfs.Permission{zfs.PermissionRelease}},
			{Dataset: "pool/other", Permissions: []zfs.Permission{zfs.PermissionRelease, zfs.PermissionMount}},
		},
	}
}

func TestNewErrorDialog_MissingPermissions(t *testing.T) {
	app := tview.NewApplication()

	assert.Equal(t, "ErrorDialog", NewErrorDialog(app, "Failed", errors.New("dataset is busy")).GetName())
	assert.Equal(t, "MissingPermissionsDialog", NewErrorDialog(app, "Failed", newMissingRelease()).GetName())
	// also if wrapped
	wrapped := fmt.Errorf("cannot release: %w", newMissingRelease())
	assert.Equal(t, "MissingPermissionsDialog", NewErrorDialog(app, "Failed", wrapped).GetName())
}

func TestFormatMissingPermissions(t *testing.T) {
	missing := newMissingRelease()
	description := formatMissingPermissions(missing, sudoCommands(missing.GrantCommands()))
	assert.Equal(t, "To release holds, user alice needs these ZFS permissions:\n"+
		"\n  release on pool/data"+
		"\n  release, mount on pool/other"+
		"\n\nThey can be delegated by root with:\n"+
		"\n  sudo zfs allow -u alice release pool/data"+
		"\n  sudo zfs allow -u alice release,mount pool/other", description)

	missing.Cause = errors.New("cannot release hold: permission denied")
	description = formatMissingPermissions(missing, nil)
	assert.Contains(t, description, "\n\nReason: cannot release hold: permission denied\n\n")
}

type permissionsDialogTest struct {
	t      *testing.T
	app    *tview.Application
	screen tcell.SimulationScreen
	pages  *tview.Pages

	mu      sync.Mutex
	granted [][]string
	copied  []string
	// verifyResults are returned by the calls of verifyPermissions, in order (nil if exhausted)
	verifyResults []error
	verified      int
	retried       int
	// onUiThread records whether grantPermissions / verifyPermissions were called on the UI thread
	onUiThread bool
}

// newPermissionsDialogTest shows the dialog for newMissingRelease, with a retry function if withRetry is set.
// sudo, zfs and the clipboard are never used.
func newPermissionsDialogTest(t *testing.T, withRetry bool, grantErr error, copyErr error, verifyResults ...error) *permissionsDialogTest {
	dt := &permissionsDialogTest{t: t, verifyResults: verifyResults}
	originalGrant, originalCopy, originalVerify := grantPermissions, copyToClipboard, verifyPermissions
	t.Cleanup(func() {
		grantPermissions, copyToClipboard, verifyPermissions = originalGrant, originalCopy, originalVerify
	})
	grantPermissions = func(application *tview.Application, explanation string, commands [][]string) error {
		dt.recordUiThread()
		dt.mu.Lock()
		defer dt.mu.Unlock()
		dt.granted = append(dt.granted, commands...)
		return grantErr
	}
	copyToClipboard = func(text string) error {
		dt.mu.Lock()
		defer dt.mu.Unlock()
		dt.copied = append(dt.copied, text)
		return copyErr
	}
	verifyPermissions = func(missing *zfs.MissingPermissionsError) error {
		dt.recordUiThread()
		dt.mu.Lock()
		defer dt.mu.Unlock()
		dt.verified++
		if dt.verified <= len(dt.verifyResults) {
			return dt.verifyResults[dt.verified-1]
		}
		return nil
	}

	dt.app = tview.NewApplication()
	dt.screen = tcell.NewSimulationScreen("UTF-8")
	dt.app.SetScreen(dt.screen)
	dt.pages = tview.NewPages().AddPage("background", tview.NewBox(), true, true)
	dt.app.SetRoot(dt.pages, true)
	go func() { _ = dt.app.Run() }()
	t.Cleanup(dt.app.Stop)

	var retry func()
	if withRetry {
		retry = func() {
			dt.mu.Lock()
			defer dt.mu.Unlock()
			dt.retried++
		}
	}
	testutil.OnUiThread(t, dt.app, func() {
		ShowDialogOnPages(dt.app, dt.pages, NewErrorDialogWithRetry(dt.app, "Release Failed", newMissingRelease(), retry), nil)
		// updates outside of input events are not drawn automatically
		dt.app.ForceDraw()
	})
	return dt
}

// recordUiThread notes if the caller blocks the UI thread: then a queued update cannot run.
func (dt *permissionsDialogTest) recordUiThread() {
	ran := make(chan struct{}, 1)
	go dt.app.QueueUpdate(func() { ran <- struct{}{} })
	select {
	case <-ran:
	case <-time.After(time.Second):
		dt.mu.Lock()
		dt.onUiThread = true
		dt.mu.Unlock()
	}
}

func (dt *permissionsDialogTest) choose(option rune) {
	dt.screen.InjectKey(tcell.KeyRune, option, tcell.ModNone)
	dt.screen.InjectKey(tcell.KeyEnter, 0, tcell.ModNone)
}

func (dt *permissionsDialogTest) hasPage(name string) bool {
	shown := false
	testutil.OnUiThread(dt.t, dt.app, func() { shown = dt.pages.HasPage(name) })
	return shown
}

// waitForDialog waits until the dialog with the given name replaced the permissions dialog.
func (dt *permissionsDialogTest) waitForDialog(name string) {
	require.Eventually(dt.t, func() bool {
		return dt.hasPage(name) && !dt.hasPage("MissingPermissionsDialog")
	}, 3*time.Second, 10*time.Millisecond, "dialog %s", name)
}

func (dt *permissionsDialogTest) waitForText(text string) {
	require.Eventually(dt.t, func() bool { return strings.Contains(dt.screenText(), text) }, 3*time.Second, 10*time.Millisecond, "text %q", text)
}

func (dt *permissionsDialogTest) counts() (verified int, retried int) {
	dt.mu.Lock()
	defer dt.mu.Unlock()
	return dt.verified, dt.retried
}

func (dt *permissionsDialogTest) screenText() string {
	var text strings.Builder
	testutil.OnUiThread(dt.t, dt.app, func() {
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

func TestMissingPermissionsDialog_GrantVerifiesAndRetries(t *testing.T) {
	dt := newPermissionsDialogTest(t, true, nil, nil)
	dt.waitForText("Grant and retry")

	dt.choose('1')

	require.Eventually(t, func() bool {
		_, retried := dt.counts()
		return retried == 1 && !dt.hasPage("MissingPermissionsDialog")
	}, 3*time.Second, 10*time.Millisecond, "the dialog closes and the operation is retried")
	verified, _ := dt.counts()
	assert.Equal(t, 1, verified, "the permissions are verified before the retry")
	assert.False(t, dt.hasPage("SuccessDialog"), "the retried operation shows its own result")

	dt.mu.Lock()
	defer dt.mu.Unlock()
	assert.Equal(t, [][]string{
		{"zfs", "allow", "-u", "alice", "release", "pool/data"},
		{"zfs", "allow", "-u", "alice", "release,mount", "pool/other"},
	}, dt.granted)
	assert.False(t, dt.onUiThread, "sudo and zfs run from the background")
}

func TestMissingPermissionsDialog_GrantWithoutRetry(t *testing.T) {
	dt := newPermissionsDialogTest(t, false, nil, nil)
	dt.waitForText("Grant now")
	assert.NotContains(t, dt.screenText(), "Retry")

	dt.choose('1')

	dt.waitForDialog("SuccessDialog")
	dt.waitForText("Please try again")
}

func TestMissingPermissionsDialog_StillMissingAfterGrant(t *testing.T) {
	stillMissing := &zfs.MissingPermissionsError{
		Action:  "release holds",
		User:    "alice",
		Missing: []zfs.PermissionGap{{Dataset: "pool/other", Permissions: []zfs.Permission{zfs.PermissionMount}}},
		Cause:   errors.New("the permissions are still missing"),
	}
	dt := newPermissionsDialogTest(t, true, nil, nil, stillMissing)

	dt.choose('1')

	// the dialog is shown again, for what is still missing, and nothing is retried
	dt.waitForText("Reason: the permissions are still missing")
	dt.waitForText("sudo zfs allow -u alice mount pool/other")
	assert.NotContains(t, dt.screenText(), "release on pool/data")
	_, retried := dt.counts()
	assert.Zero(t, retried)

	// granting again works and retries
	dt.choose('1')
	require.Eventually(t, func() bool {
		_, retried := dt.counts()
		return retried == 1
	}, 3*time.Second, 10*time.Millisecond)
}

func TestMissingPermissionsDialog_GrantFails(t *testing.T) {
	dt := newPermissionsDialogTest(t, true, errors.New("sudo: a password is required"), nil)

	dt.choose('1')

	dt.waitForDialog("ErrorDialog")
	dt.waitForText("a password is required")
	verified, retried := dt.counts()
	assert.Zero(t, verified)
	assert.Zero(t, retried)
}

func TestMissingPermissionsDialog_CopyThenRetry(t *testing.T) {
	dt := newPermissionsDialogTest(t, true, nil, nil, &zfs.MissingPermissionsError{
		Action:  "release holds",
		User:    "alice",
		Missing: newMissingRelease().Missing,
		Cause:   errors.New("the permissions are still missing"),
	})

	dt.choose('2')

	// the dialog stays open to retry after running the command
	dt.waitForText("Copied, run it in a terminal")
	assert.True(t, dt.hasPage("MissingPermissionsDialog"))
	dt.mu.Lock()
	assert.Equal(t, []string{"sudo zfs allow -u alice release pool/data\nsudo zfs allow -u alice release,mount pool/other"}, dt.copied)
	assert.Empty(t, dt.granted)
	dt.mu.Unlock()

	// retry before the command was run: still missing
	dt.choose('3')
	dt.waitForText("Reason: the permissions are still missing")
	_, retried := dt.counts()
	assert.Zero(t, retried)

	// retry after the command was run
	dt.choose('3')
	require.Eventually(t, func() bool {
		_, retried := dt.counts()
		return retried == 1
	}, 3*time.Second, 10*time.Millisecond)
}

func TestMissingPermissionsDialog_CopyFailsShowsCommand(t *testing.T) {
	dt := newPermissionsDialogTest(t, true, nil, errors.New("no clipboard utility found"))

	dt.choose('2')

	dt.waitForDialog("ErrorDialog")
	dt.waitForText("no clipboard utility found")
	dt.waitForText("sudo zfs allow -u alice release pool/data")
}
