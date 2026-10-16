package snapshot_browser

import (
	"sync"
	"testing"
	"time"
	"zfs-file-history/internal/data"
	"zfs-file-history/internal/testutil"
	"zfs-file-history/internal/ui/dialog"
	"zfs-file-history/internal/zfs"

	"github.com/gdamore/tcell/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// blockPreviews makes the dry run of destroying snapshots and the preview of rolling back wait until the returned
// function is called, and returns it.
func (dt *destroyTest) blockPreviews() (release func()) {
	released := make(chan struct{})
	preview := previewDestroySnapshots
	testutil.Replace(dt.t, &previewDestroySnapshots, func(snapshots []*zfs.Snapshot, recursive bool, dependantClones bool) (*zfs.DestroyPreview, error) {
		<-released
		return preview(snapshots, recursive, dependantClones)
	})
	testutil.Replace(dt.t, &previewRollback, func(snapshot string) (*zfs.RollbackPreview, error) {
		<-released
		return &zfs.RollbackPreview{Snapshot: snapshot}, nil
	})
	release = sync.OnceFunc(func() { close(released) })
	dt.t.Cleanup(release)
	return release
}

// Regression: destroying and rolling back ran their previews in the action dialog, with a spinner next to the
// option, which looked like they ran without a confirmation. The dialog of the confirmation opens right away and
// says what is checked; the confirmation follows once that is done.
func TestSnapshotBrowser_PreviewsRunInTheConfirmation(t *testing.T) {
	tests := []struct {
		name string
		// multiSelection selects both snapshots first; option is the option chosen in the dialog
		multiSelection bool
		dialog         string
		option         rune
		checking       string
		confirmation   string
	}{
		// menu: 1 create, 2 clone, 3 hold, 4 destroy, 5 destroy (recursive), 6 roll back
		{name: "destroy", dialog: "SnapshotActionDialog", option: '4',
			checking: "Checking what destroying pool/data@daily-1 would destroy", confirmation: "DestroySnapshotsDialog"},
		{name: "destroy recursively", dialog: "SnapshotActionDialog", option: '5',
			checking: "Checking what destroying pool/data@daily-1 and", confirmation: "DestroySnapshotsDialog"},
		{name: "roll back", dialog: "SnapshotActionDialog", option: '6',
			checking: "Checking what rolling back to pool/data@daily-1 would lose", confirmation: "RollbackDialog"},
		// multi menu: 1 hold all, 2 destroy all
		{name: "destroy the selected ones", multiSelection: true, dialog: "MultiSnapshotActionDialog", option: '2',
			checking: "Checking what destroying 2 snapshots would destroy", confirmation: "DestroySnapshotsDialog"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			dt := newDestroyTest(t)
			release := dt.blockPreviews()
			if test.multiSelection {
				dt.selectAll()
			}

			dt.press(tcell.KeyEnter, 0)
			dt.waitForDialog(test.dialog)
			dt.press(tcell.KeyRune, test.option)
			dt.press(tcell.KeyEnter, 0)

			dt.waitForDialog(string(dialog.PreviewDialogPage))
			require.Eventually(t, func() bool { return !dt.hasDialog(test.dialog) }, 2*time.Second, 10*time.Millisecond)
			testutil.WaitForText(t, dt.app, dt.screen, test.checking, 2*time.Second)

			release()
			dt.waitForDialog(test.confirmation)
			require.Eventually(t, func() bool { return !dt.hasDialog(string(dialog.PreviewDialogPage)) }, 2*time.Second, 10*time.Millisecond)
		})
	}
}

// The delete key shows what is checked as well: it showed nothing until the dry run was done.
func TestSnapshotBrowser_DeleteKeyShowsTheCheck(t *testing.T) {
	dt := newDestroyTest(t)
	release := dt.blockPreviews()

	testutil.OnUiThread(t, dt.app, func() { dt.browser.openDeleteDialog(dt.browser.GetSelection()) })

	dt.waitForDialog(string(dialog.PreviewDialogPage))
	release()
	dt.waitForDialog("DestroySnapshotsDialog")
}

// Cancelled while it is checked, the confirmation is not shown once that is done.
func TestSnapshotBrowser_PreviewCanBeCancelled(t *testing.T) {
	dt := newDestroyTest(t)
	release := dt.blockPreviews()

	dt.press(tcell.KeyEnter, 0)
	dt.waitForDialog("SnapshotActionDialog")
	dt.press(tcell.KeyRune, '4') // destroy
	dt.press(tcell.KeyEnter, 0)
	dt.waitForDialog(string(dialog.PreviewDialogPage))
	dt.press(tcell.KeyEscape, 0)
	require.Eventually(t, func() bool { return !dt.hasDialog(string(dialog.PreviewDialogPage)) }, 2*time.Second, 10*time.Millisecond)
	release()

	require.Eventually(t, func() bool {
		previews, _ := dt.calls()
		return len(previews) == 1
	}, 2*time.Second, 10*time.Millisecond)
	require.Never(t, func() bool { return dt.hasDialog("DestroySnapshotsDialog") || dt.hasDialog("ErrorDialog") },
		200*time.Millisecond, 10*time.Millisecond)
	_, destroys := dt.calls()
	assert.Empty(t, destroys)
}

func TestFormatDestroyCheck(t *testing.T) {
	entries := []*data.SnapshotBrowserEntry{
		{Snapshot: &zfs.Snapshot{Name: "daily-1", FullName: "pool/data@daily-1"}},
		{Snapshot: &zfs.Snapshot{Name: "daily-2", FullName: "pool/data@daily-2"}},
	}

	assert.Equal(t, "Checking what destroying pool/data@daily-1 would destroy: their holds, and a dry run. Nothing is "+
		"destroyed yet.", formatDestroyCheck(&destroyRequest{entries: entries[:1]}))
	assert.Equal(t, "Checking what destroying pool/data@daily-1 and the snapshots with the same name of the child "+
		"datasets would destroy: their holds, and a dry run. Nothing is destroyed yet.",
		formatDestroyCheck(&destroyRequest{entries: entries[:1], recursive: true}))
	assert.Equal(t, "Checking what destroying 2 snapshots would destroy: their holds, and a dry run. Nothing is "+
		"destroyed yet.", formatDestroyCheck(&destroyRequest{entries: entries}))
}
