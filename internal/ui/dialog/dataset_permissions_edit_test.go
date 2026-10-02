package dialog

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
	"zfs-file-history/internal/ui/shortcut_helper"
	"zfs-file-history/internal/zfs"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// pressDialogKey sends a key to the dialog, like the application would, and returns the event that is passed on.
func pressDialogKey(d *DatasetPermissionsDialog, key tcell.Key, r rune) *tcell.EventKey {
	return d.layout.GetInputCapture()(tcell.NewEventKey(key, r, tcell.ModNone))
}

func TestDatasetPermissionsDialog_Toggle(t *testing.T) {
	d := NewDatasetPermissionsDialog(tview.NewApplication(), newTestDatasetPermissions(), nil)
	toggle := func(row int) {
		d.table.Select(row, 0)
		assert.Nil(t, pressDialogKey(d, tcell.KeyRune, ' '))
	}

	toggle(2) // destroy: missing, granted
	toggle(1) // snapshot: delegated to alice on this dataset, revoked
	toggle(3) // mount: from the parent (via group staff), cannot be revoked here
	toggle(4) // hold: via the permission set @backup, cannot be revoked here

	rows := tableRows(d)
	assert.Equal(t, "snapshot|s|→✗|user alice (this dataset)", rows[1])
	assert.Equal(t, "destroy|d|→✓", rows[2])
	assert.Equal(t, "mount|m|✓|group staff (from pool) +1", rows[3])
	assert.Equal(t, "hold|h|✓|user alice via @backup (this dataset)", rows[4])
	assert.Equal(t, "What user alice may do on this dataset and its children: (2 changes)", d.summary.GetText(true))
	assert.Equal(t, zfs.DelegationChange{
		Dataset: "pool/data",
		Grantee: zfs.Grantee{Type: zfs.WhoUser, Name: "alice"},
		Scope:   zfs.ScopeLocalAndDescendent,
		Allow:   []zfs.Permission{zfs.PermissionDestroy},
		Unallow: []zfs.Permission{zfs.PermissionSnapshot},
	}, d.pendingChange())

	// toggling again removes the change
	toggle(1)
	assert.Equal(t, "snapshot|s|✓|user alice (this dataset)", tableRows(d)[1])
	assert.Len(t, d.pending, 1)
}

func TestDatasetPermissionsDialog_RevokeHintsAtOtherGrants(t *testing.T) {
	permissions := newTestDatasetPermissions()
	everyone := zfs.Delegation{Dataset: "pool", Scope: zfs.ScopeLocalAndDescendent, WhoType: zfs.WhoEveryone, Permissions: []string{"snapshot"}}
	permissions.Grants[zfs.PermissionSnapshot] = append(permissions.Grants[zfs.PermissionSnapshot], zfs.Grant{Delegation: everyone})
	d := NewDatasetPermissionsDialog(tview.NewApplication(), permissions, nil)

	d.table.Select(1, 0)
	pressDialogKey(d, tcell.KeyRune, ' ')

	assert.Contains(t, d.details.GetText(true), "Will be revoked when applying (a), Space to undo. Still granted by everyone (from pool) afterwards.")
}

func TestDatasetPermissionsDialog_RootCannotEdit(t *testing.T) {
	permissions := newTestDatasetPermissions()
	permissions.IsRoot = true
	d := NewDatasetPermissionsDialog(tview.NewApplication(), permissions, nil)

	d.table.Select(2, 0)
	pressDialogKey(d, tcell.KeyRune, ' ')

	assert.Empty(t, d.pending, "root needs no delegations")
	assert.Equal(t, "destroy: Destroy snapshots and datasets (also needs mount)", d.details.GetText(true))
}

func TestDatasetPermissionsDialog_EscDiscardsChangesFirst(t *testing.T) {
	d := NewDatasetPermissionsDialog(tview.NewApplication(), newTestDatasetPermissions(), nil)
	d.table.Select(2, 0)
	pressDialogKey(d, tcell.KeyRune, ' ')

	assert.Nil(t, pressDialogKey(d, tcell.KeyEscape, 0))
	assert.Empty(t, d.pending)

	pressDialogKey(d, tcell.KeyEscape, 0)
	select {
	case action := <-d.GetActionChannel():
		assert.Equal(t, DialogCloseActionId, action)
	case <-time.After(time.Second):
		t.Fatal("not closed")
	}
}

func TestDatasetPermissionsDialog_ShortcutsFit(t *testing.T) {
	shortcuts := shortcut_helper.NewShortcutMap(tview.NewApplication())
	shortcuts.SetEntries(datasetPermissionsShortcuts(false))
	// the content width of the dialog, see CalculateDialogSize: the dialog frame has a border
	width := datasetPermissionsContentWidth + 6 - 2
	assert.LessOrEqual(t, shortcuts.CalculateHeightForWidth(width), datasetPermissionsShortcutLines)
}

// permissionsEditTest shows the permissions dialog in a running application. zfs and sudo are never run.
type permissionsEditTest struct {
	t      *testing.T
	app    *tview.Application
	screen tcell.SimulationScreen
	pages  *tview.Pages
	dialog *DatasetPermissionsDialog

	mu        sync.Mutex
	applied   []zfs.DelegationChange
	applyErr  error
	sudo      [][]string
	sudoErr   error
	reloads   int
	onChanged int
}

func newPermissionsEditTest(t *testing.T, permissions *zfs.DatasetPermissions) *permissionsEditTest {
	et := &permissionsEditTest{t: t}
	originalApply, originalLoad, originalGrant := applyDelegationChange, loadDatasetPermissions, grantPermissions
	t.Cleanup(func() {
		applyDelegationChange, loadDatasetPermissions, grantPermissions = originalApply, originalLoad, originalGrant
	})
	applyDelegationChange = func(change zfs.DelegationChange) error {
		et.mu.Lock()
		defer et.mu.Unlock()
		et.applied = append(et.applied, change)
		return et.applyErr
	}
	grantPermissions = func(application *tview.Application, explanation string, commands [][]string) error {
		et.mu.Lock()
		defer et.mu.Unlock()
		et.sudo = append(et.sudo, commands...)
		return et.sudoErr
	}
	loadDatasetPermissions = func(dataset string) (*zfs.DatasetPermissions, error) {
		et.mu.Lock()
		defer et.mu.Unlock()
		et.reloads++
		// the change was applied: alice has destroy delegated now
		reloaded := newTestDatasetPermissions()
		destroy := zfs.Delegation{Dataset: "pool/data", Scope: zfs.ScopeLocalAndDescendent, WhoType: zfs.WhoUser, Who: "alice", Permissions: []string{"destroy"}}
		reloaded.Delegations.Entries = append(reloaded.Delegations.Entries, destroy)
		reloaded.Grants[zfs.PermissionDestroy] = []zfs.Grant{{Delegation: destroy}}
		return reloaded, nil
	}

	et.app = tview.NewApplication()
	et.screen = tcell.NewSimulationScreen("UTF-8")
	et.app.SetScreen(et.screen)
	et.pages = tview.NewPages().AddPage("background", tview.NewBox(), true, true)
	et.app.SetRoot(et.pages, true)
	go func() { _ = et.app.Run() }()
	t.Cleanup(et.app.Stop)

	onUiThread(t, et.app, func() {
		et.dialog = NewDatasetPermissionsDialog(et.app, permissions, func() {
			et.mu.Lock()
			defer et.mu.Unlock()
			et.onChanged++
		})
		ShowDialogOnPages(et.app, et.pages, et.dialog, nil)
		et.app.ForceDraw()
	})
	return et
}

func (et *permissionsEditTest) press(key tcell.Key, r rune) {
	et.screen.InjectKey(key, r, tcell.ModNone)
}

func (et *permissionsEditTest) typeText(text string) {
	for _, r := range text {
		et.press(tcell.KeyRune, r)
	}
}

func (et *permissionsEditTest) hasPage(name string) bool {
	shown := false
	onUiThread(et.t, et.app, func() { shown = et.pages.HasPage(name) })
	return shown
}

func (et *permissionsEditTest) waitFor(message string, condition func() bool) {
	require.Eventually(et.t, condition, 3*time.Second, 10*time.Millisecond, message)
}

func (et *permissionsEditTest) screenText() string {
	var text strings.Builder
	onUiThread(et.t, et.app, func() {
		et.app.ForceDraw()
		cells, width, height := et.screen.GetContents()
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

func (et *permissionsEditTest) summaryText() string {
	var text string
	onUiThread(et.t, et.app, func() { text = et.dialog.summary.GetText(true) })
	return text
}

// toggleDestroy selects the destroy row and toggles it.
func (et *permissionsEditTest) toggleDestroy() {
	onUiThread(et.t, et.app, func() { et.dialog.table.Select(2, 0) })
	et.press(tcell.KeyRune, ' ')
	et.waitFor("toggled", func() bool { return strings.Contains(et.summaryText(), "1 change") })
}

func TestDatasetPermissionsDialog_Apply(t *testing.T) {
	et := newPermissionsEditTest(t, newTestDatasetPermissions())
	et.toggleDestroy()

	et.press(tcell.KeyRune, 'a')
	et.waitFor("confirmation", func() bool { return et.hasPage("ApplyDelegationsDialog") })
	et.waitFor("commands shown", func() bool {
		text := et.screenText()
		return strings.Contains(text, "Grant destroy to user alice on pool/data") && strings.Contains(text, "zfs allow -u alice destroy pool/data")
	})
	et.press(tcell.KeyRune, '1')
	et.press(tcell.KeyEnter, 0)

	et.waitFor("applied", func() bool {
		et.mu.Lock()
		defer et.mu.Unlock()
		return et.onChanged == 1 && !et.hasPage("ApplyDelegationsDialog")
	})
	et.mu.Lock()
	assert.Len(t, et.applied, 1)
	assert.Equal(t, []zfs.Permission{zfs.PermissionDestroy}, et.applied[0].Allow)
	assert.Empty(t, et.sudo, "applied as the current user")
	assert.Equal(t, 1, et.reloads)
	et.mu.Unlock()

	// the dialog shows the reloaded delegations, without pending changes
	var destroyRow string
	onUiThread(t, et.app, func() { destroyRow = tableRows(et.dialog)[2] })
	assert.Equal(t, "destroy|d|✓|user alice (this dataset)", destroyRow)
	assert.Equal(t, "What user alice may do on this dataset and its children:", et.summaryText())
}

func TestDatasetPermissionsDialog_ApplyWithSudo(t *testing.T) {
	et := newPermissionsEditTest(t, newTestDatasetPermissions())
	et.applyErr = fmt.Errorf("%w: cannot set permissions", zfs.ErrPermissionDenied)
	et.toggleDestroy()

	et.press(tcell.KeyRune, 'a')
	et.waitFor("confirmation", func() bool { return et.hasPage("ApplyDelegationsDialog") })
	et.waitFor("sudo mentioned", func() bool { return strings.Contains(et.screenText(), "run with sudo") })
	et.press(tcell.KeyRune, '1')
	et.press(tcell.KeyEnter, 0)

	et.waitFor("applied", func() bool {
		et.mu.Lock()
		defer et.mu.Unlock()
		return et.onChanged == 1
	})
	et.mu.Lock()
	defer et.mu.Unlock()
	assert.Equal(t, [][]string{{"zfs", "allow", "-u", "alice", "destroy", "pool/data"}}, et.sudo)
}

func TestDatasetPermissionsDialog_ApplyFails(t *testing.T) {
	et := newPermissionsEditTest(t, newTestDatasetPermissions())
	et.applyErr = fmt.Errorf("%w: cannot set permissions", zfs.ErrPermissionDenied)
	et.sudoErr = errors.New("sudo: 3 incorrect password attempts")
	et.toggleDestroy()

	et.press(tcell.KeyRune, 'a')
	et.waitFor("confirmation", func() bool { return et.hasPage("ApplyDelegationsDialog") })
	et.press(tcell.KeyRune, '1')
	et.press(tcell.KeyEnter, 0)

	et.waitFor("error", func() bool { return et.hasPage("ErrorDialog") })
	et.waitFor("error text", func() bool { return strings.Contains(et.screenText(), "incorrect password") })
	et.mu.Lock()
	assert.Zero(t, et.onChanged)
	assert.Zero(t, et.reloads)
	et.mu.Unlock()
	assert.True(t, strings.Contains(et.summaryText(), "1 change"), "the changes are kept, to try again")
}

func TestDatasetPermissionsDialog_ApplyWithoutChanges(t *testing.T) {
	et := newPermissionsEditTest(t, newTestDatasetPermissions())
	et.press(tcell.KeyRune, 'a')
	time.Sleep(50 * time.Millisecond)
	assert.False(t, et.hasPage("ApplyDelegationsDialog"))
}
