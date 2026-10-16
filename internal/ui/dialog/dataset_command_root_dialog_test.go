package dialog

import (
	"errors"
	"fmt"
	"testing"
	"time"
	"zfs-file-history/internal/testutil"
	"zfs-file-history/internal/zfs"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newUnmountNeedsRoot() *zfs.DatasetCommandNeedsRootError {
	return &zfs.DatasetCommandNeedsRootError{
		Action:   "unmount tank/data",
		Done:     "Unmounted tank/data",
		Commands: [][]string{{"/usr/bin/zfs", "unmount", "tank/data/child"}, {"/usr/bin/zfs", "unmount", "tank/data"}},
		Cause:    errors.New("cannot unmount '/data/child': permission denied"),
	}
}

func TestNewErrorDialog_DatasetCommandNeedsRoot(t *testing.T) {
	app, _ := testutil.NewApp()

	assert.Equal(t, "DatasetCommandRootDialog", NewErrorDialog(app, "Failed", newUnmountNeedsRoot()).GetName())
	// also if wrapped
	assert.Equal(t, "DatasetCommandRootDialog", NewErrorDialog(app, "Failed", fmt.Errorf("failed: %w", newUnmountNeedsRoot())).GetName())
}

func TestFormatDatasetCommandNeedsRoot(t *testing.T) {
	assert.Equal(t, "To unmount tank/data, zfs has to run as root: Linux only lets root mount and unmount file systems, "+
		"whatever was delegated with zfs allow."+
		"\n\nReason: cannot unmount '/data/child': permission denied"+
		"\n\nThe setup cannot allow it without a password: a rule for zfs mount would let your user mount any dataset, "+
		"also over the directories of the system. It runs:\n"+
		"\n  sudo /usr/bin/zfs unmount tank/data/child"+
		"\n  sudo /usr/bin/zfs unmount tank/data", formatDatasetCommandNeedsRoot(newUnmountNeedsRoot()))
}

func TestDatasetCommandRootDialog_RunOnce(t *testing.T) {
	dt := newRootDialogTestFor(t, newUnmountNeedsRoot(), true, nil)
	dt.waitForText("Run once with sudo")

	dt.choose('1')

	dt.waitForFollowUp("SuccessDialog")
	dt.waitForText("Unmounted tank/data.")
	assert.Equal(t, newUnmountNeedsRoot().Commands, dt.commands(), "all commands left, with sudo")
	assert.Equal(t, int32(1), dt.datasetsLoaded.Load(), "the datasets are reloaded")
	_, retried := dt.counts()
	assert.Equal(t, 0, retried, "done, nothing to retry")
	assert.False(t, dt.onUiThread, "sudo runs from the background")
}

func TestDatasetCommandRootDialog_SudoFails(t *testing.T) {
	dt := newRootDialogTestFor(t, newUnmountNeedsRoot(), true, errors.New("sudo /usr/bin/zfs unmount tank/data: exit status 1"))

	dt.choose('1')

	require.Eventually(t, func() bool {
		return dt.hasPage("ErrorDialog") && !dt.hasPage("DatasetCommandRootDialog")
	}, 3*time.Second, 10*time.Millisecond)
	dt.waitForText("exit status 1")
	assert.Equal(t, int32(1), dt.datasetsLoaded.Load(), "some of them may have run")
}

func TestDatasetCommandRootDialog_CopyAndRetry(t *testing.T) {
	dt := newRootDialogTestFor(t, newUnmountNeedsRoot(), true, nil)

	dt.choose('2')
	dt.waitForText("Copied, run them in a terminal")
	assert.True(t, dt.hasPage("DatasetCommandRootDialog"), "stays open to retry")
	dt.mu.Lock()
	assert.Equal(t, []string{"sudo /usr/bin/zfs unmount tank/data/child && sudo /usr/bin/zfs unmount tank/data"}, dt.copied)
	dt.mu.Unlock()

	dt.choose('3')
	require.Eventually(t, func() bool {
		_, retried := dt.counts()
		return retried == 1
	}, 3*time.Second, 10*time.Millisecond)
	assert.Empty(t, dt.commands(), "nothing run with sudo")
}
