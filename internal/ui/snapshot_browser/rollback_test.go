package snapshot_browser

import (
	"strings"
	"testing"
	"time"
	"zfs-file-history/internal/ui/dialog"
	"zfs-file-history/internal/ui/theme"
	"zfs-file-history/internal/ui/txwidgets"
	"zfs-file-history/internal/zfs"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stubRollback replaces the rollback with recording, and returns the recorded calls.
func stubRollback(t *testing.T, dt *destroyTest) func() []string {
	originalPreview, originalRollback := previewRollback, rollback
	t.Cleanup(func() { previewRollback, rollback = originalPreview, originalRollback })
	var calls []string
	previewRollback = func(snapshot string) (*zfs.RollbackPreview, error) {
		dt.mu.Lock()
		defer dt.mu.Unlock()
		calls = append(calls, "preview "+snapshot)
		return &zfs.RollbackPreview{Snapshot: snapshot, Destroyed: []string{"pool/data@daily-2"}}, nil
	}
	rollback = func(confirmed *zfs.RollbackPreview) error {
		dt.mu.Lock()
		defer dt.mu.Unlock()
		calls = append(calls, "rollback "+confirmed.Snapshot+" "+strings.Join(confirmed.Destroyed, ","))
		return nil
	}
	return func() []string {
		dt.mu.Lock()
		defer dt.mu.Unlock()
		return append([]string(nil), calls...)
	}
}

func TestSnapshotBrowser_RollbackAsksForTheName(t *testing.T) {
	dt := newDestroyTest(t)
	calls := stubRollback(t, dt)
	rolledBack := make(chan RolledBackEvent, 1)
	dt.browser.Events.Subscribe(func(event Event) {
		if e, ok := event.(RolledBackEvent); ok {
			rolledBack <- e
		}
	})

	// menu: 1 create, 2 clone, 3 hold, 4 destroy, 5 destroy (recursive), 6 roll back
	dt.press(tcell.KeyEnter, 0)
	dt.waitForDialog("SnapshotActionDialog")
	dt.press(tcell.KeyRune, '6')
	dt.press(tcell.KeyEnter, 0)
	dt.waitForDialog("RollbackDialog")
	assert.Equal(t, []string{"preview pool/data@daily-1"}, calls())
	// the newer snapshots are listed
	require.Eventually(t, func() bool { return strings.Contains(dt.screenText(), "pool/data@daily-2") }, 2*time.Second, 10*time.Millisecond)

	// the name of the dataset is not enough
	for _, r := range "pool/data" {
		dt.press(tcell.KeyRune, r)
	}
	dt.press(tcell.KeyEnter, 0)
	require.Eventually(t, func() bool { return strings.Contains(dt.screenText(), "the name does not match") }, 2*time.Second, 10*time.Millisecond)
	assert.Len(t, calls(), 1)

	for _, r := range "@daily-1" {
		dt.press(tcell.KeyRune, r)
	}
	dt.press(tcell.KeyEnter, 0)
	dt.waitForDialog("SuccessDialog")
	assert.Equal(t, []string{"preview pool/data@daily-1", "rollback pool/data@daily-1 pool/data@daily-2"}, calls())
	select {
	case e := <-rolledBack:
		assert.Equal(t, "pool/data@daily-1", e.Snapshot)
	case <-time.After(2 * time.Second):
		t.Fatal("no RolledBackEvent")
	}
}

func TestSnapshotBrowser_RollbackCanBeCancelled(t *testing.T) {
	dt := newDestroyTest(t)
	calls := stubRollback(t, dt)

	dt.press(tcell.KeyEnter, 0)
	dt.waitForDialog("SnapshotActionDialog")
	dt.press(tcell.KeyRune, '6')
	dt.press(tcell.KeyEnter, 0)
	dt.waitForDialog("RollbackDialog")
	dt.press(tcell.KeyEscape, 0)
	require.Eventually(t, func() bool { return !dt.hasDialog("RollbackDialog") }, 2*time.Second, 10*time.Millisecond)

	assert.Equal(t, []string{"preview pool/data@daily-1"}, calls())
}

func TestFormatRollbackDescription(t *testing.T) {
	description := formatRollbackDescription(&zfs.RollbackPreview{
		Snapshot:  "pool/data@[old]",
		Destroyed: []string{"pool/data@[new]"},
	})

	// what is lost stands out
	assert.Contains(t, description, txwidgets.Span(theme.Colors.Dialog.Danger, "are lost"))
	assert.Contains(t, description, txwidgets.Span(theme.Colors.Dialog.Danger, "are destroyed"))
	// names are shown as they are, not taken for style tags
	shown := tview.NewTextView().SetDynamicColors(true).SetText(description).GetText(true)
	assert.Contains(t, shown, "Roll back pool/data to snapshot [old].")
	assert.Contains(t, description, dialog.Highlight("pool/data"))
	assert.Contains(t, description, dialog.Highlight("[old]"))
	assert.Contains(t, shown, "pool/data@[new]")
	assert.Contains(t, shown, "clone the snapshot instead")
}
