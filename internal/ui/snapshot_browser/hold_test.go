package snapshot_browser

import (
	"errors"
	"strings"
	"testing"
	"time"
	"zfs-file-history/internal/zfs"

	"github.com/gdamore/tcell/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stubHolds replaces the zfs hold functions: existing are the holds listHolds returns,
// changes records the snapshots passed to holdSnapshots / releaseSnapshots.
func stubHolds(t *testing.T, existing []zfs.Hold, listErr error, changeErr error) (held *[]string, released *[]string) {
	originalList, originalHold, originalRelease := listHolds, holdSnapshots, releaseSnapshots
	t.Cleanup(func() { listHolds, holdSnapshots, releaseSnapshots = originalList, originalHold, originalRelease })

	held, released = &[]string{}, &[]string{}
	listHolds = func(names []string, recursive bool) ([]zfs.Hold, error) {
		var result []zfs.Hold
		for _, h := range existing {
			for _, name := range names {
				if h.Snapshot == name {
					result = append(result, h)
				}
			}
		}
		return result, listErr
	}
	holdSnapshots = func(names []string) error {
		*held = append(*held, names...)
		return changeErr
	}
	releaseSnapshots = func(names []string) error {
		*released = append(*released, names...)
		return changeErr
	}
	return held, released
}

func TestHoldOrRelease_HoldSkipsSnapshotsAlreadyHeld(t *testing.T) {
	held, released := stubHolds(t, []zfs.Hold{
		{Snapshot: "pool/data@a", Tag: zfs.HoldTag},
		{Snapshot: "pool/data@b", Tag: "syncoid_host"},
	}, nil, nil)

	result, err := holdOrRelease([]string{"pool/data@a", "pool/data@b", "pool/data@c"}, true)

	require.NoError(t, err)
	assert.Equal(t, []string{"pool/data@b", "pool/data@c"}, *held, "a hold of another tag does not count")
	assert.Empty(t, *released)
	assert.Equal(t, []string{"pool/data@b", "pool/data@c"}, result.changed)
	assert.Equal(t, []string{"pool/data@a"}, result.skipped)
}

func TestHoldOrRelease_ReleasesOnlyOwnHolds(t *testing.T) {
	held, released := stubHolds(t, []zfs.Hold{
		{Snapshot: "pool/data@a", Tag: zfs.HoldTag},
		{Snapshot: "pool/data@a", Tag: "syncoid_host"},
		{Snapshot: "pool/data@b", Tag: "syncoid_host"},
	}, nil, nil)

	result, err := holdOrRelease([]string{"pool/data@a", "pool/data@b"}, false)

	require.NoError(t, err)
	assert.Empty(t, *held)
	assert.Equal(t, []string{"pool/data@a"}, *released, "holds of other tags are never released")
	assert.Equal(t, []string{"pool/data@b"}, result.skipped)
	assert.Equal(t, []zfs.Hold{{Snapshot: "pool/data@a", Tag: "syncoid_host"}}, result.otherHolds)
}

func TestHoldOrRelease_ReleaseWithoutOwnHolds(t *testing.T) {
	_, released := stubHolds(t, []zfs.Hold{{Snapshot: "pool/data@a", Tag: "syncoid_host"}}, nil, nil)

	_, err := holdOrRelease([]string{"pool/data@a"}, false)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "Not held by "+zfs.HoldTag)
	assert.Contains(t, err.Error(), "pool/data@a (syncoid_host)")
	assert.Empty(t, *released)
}

func TestHoldOrRelease_Errors(t *testing.T) {
	held, _ := stubHolds(t, nil, errors.New("permission denied"), nil)
	_, err := holdOrRelease([]string{"pool/data@a"}, true)
	assert.EqualError(t, err, "permission denied")
	assert.Empty(t, *held, "nothing is held if the holds cannot be listed")

	stubHolds(t, nil, nil, errors.New("cannot hold"))
	_, err = holdOrRelease([]string{"pool/data@a"}, true)
	assert.EqualError(t, err, "cannot hold")
}

func TestFormatHoldMessage(t *testing.T) {
	message := formatHoldMessage(&holdResult{changed: []string{"pool/data@a"}, skipped: []string{"pool/data@b"}}, true)
	assert.Equal(t, "Held 1 snapshot with tag 'zfs-file-history', destroying it fails until the hold is released:\n"+
		"  pool/data@a\n\nAlready held:\n  pool/data@b", message)

	message = formatHoldMessage(&holdResult{skipped: []string{"pool/data@b"}}, true)
	assert.Equal(t, "Already held:\n  pool/data@b", message)

	message = formatHoldMessage(&holdResult{
		changed:    []string{"pool/data@a"},
		skipped:    []string{"pool/data@b"},
		otherHolds: []zfs.Hold{{Snapshot: "pool/data@a", Tag: "syncoid_host"}},
	}, false)
	assert.Equal(t, "Released the hold 'zfs-file-history' of 1 snapshot:\n  pool/data@a\n\n"+
		"Not held by zfs-file-history:\n  pool/data@b\n\n"+
		"Still held by other tags:\n  pool/data@a (syncoid_host)", message)

	var many []string
	for i := 0; i < 12; i++ {
		many = append(many, "pool/data@x")
	}
	message = formatHoldMessage(&holdResult{changed: many}, true)
	assert.Equal(t, maxListedHolds, strings.Count(message, "pool/data@x"))
	assert.Contains(t, message, "… and 2 more")
}

func TestSnapshotBrowser_HoldFromTheMenu(t *testing.T) {
	dt := newDestroyTest(t)

	// menu: 1 create, 2 clone, 3 hold (no release, as the snapshot has no holds)
	dt.press(tcell.KeyEnter, 0)
	dt.waitForDialog("SnapshotActionDialog")
	assert.NotContains(t, dt.screenText(), "Release")
	dt.press(tcell.KeyRune, '3')
	dt.press(tcell.KeyEnter, 0)
	dt.waitForDialog("SuccessDialog")

	dt.mu.Lock()
	defer dt.mu.Unlock()
	require.Len(t, dt.holdCalls, 1)
	assert.Equal(t, holdCall{snapshots: []string{"pool/data@daily-1"}}, dt.holdCalls[0], "held off the UI thread")
	assert.Empty(t, dt.releaseCalls)
}

func TestSnapshotBrowser_ReleaseFromTheMenu(t *testing.T) {
	dt := newDestroyTest(t)
	onUiThread(t, dt.app, func() {
		// SetData sorted dt.entries in place
		for _, entry := range dt.entries {
			if entry.Snapshot.Name == "daily-1" {
				entry.Snapshot.Properties.Holds = 1
			}
		}
	})
	dt.holds = []zfs.Hold{{Snapshot: "pool/data@daily-1", Tag: zfs.HoldTag}}

	// menu: 1 create, 2 clone, 3 hold, 4 release
	dt.press(tcell.KeyEnter, 0)
	dt.waitForDialog("SnapshotActionDialog")
	require.Eventually(t, func() bool { return strings.Contains(dt.screenText(), "Release hold") }, 2*time.Second, 10*time.Millisecond)
	dt.press(tcell.KeyRune, '4')
	dt.press(tcell.KeyEnter, 0)
	dt.waitForDialog("SuccessDialog")

	dt.mu.Lock()
	defer dt.mu.Unlock()
	require.Len(t, dt.releaseCalls, 1)
	assert.Equal(t, holdCall{snapshots: []string{"pool/data@daily-1"}}, dt.releaseCalls[0])
	assert.Empty(t, dt.holdCalls)
}

func TestSnapshotBrowser_HoldMultiSelection(t *testing.T) {
	dt := newDestroyTest(t)
	dt.selectAll()

	// multi menu: 1 hold all
	dt.press(tcell.KeyEnter, 0)
	dt.waitForDialog("MultiSnapshotActionDialog")
	dt.press(tcell.KeyRune, '1')
	dt.press(tcell.KeyEnter, 0)
	dt.waitForDialog("SuccessDialog")

	dt.mu.Lock()
	require.Len(t, dt.holdCalls, 1)
	assert.ElementsMatch(t, []string{"pool/data@daily-1", "pool/data@daily-2"}, dt.holdCalls[0].snapshots, "one call for all")
	dt.mu.Unlock()
	require.Eventually(t, func() bool {
		count := -1
		onUiThread(t, dt.app, func() { count = len(dt.browser.tableContainer.GetMultiSelection()) })
		return count == 0
	}, 2*time.Second, 10*time.Millisecond, "the multi selection is cleared")
}

func TestSnapshotBrowser_HeldSnapshotIsNotDestroyed(t *testing.T) {
	dt := newDestroyTest(t)
	dt.holds = []zfs.Hold{{Snapshot: "pool/data/child@daily-1", Tag: "syncoid_host"}}

	// the hold is on the snapshot of a child dataset: a plain destroy works, a recursive one does not
	dt.press(tcell.KeyDelete, 0)
	dt.waitForDialog("DestroySnapshotsDialog")
	dt.press(tcell.KeyEscape, 0)
	require.Eventually(t, func() bool { return !dt.hasDialog("DestroySnapshotsDialog") }, 2*time.Second, 10*time.Millisecond)

	// menu: 5 destroy (recursive)
	dt.press(tcell.KeyEnter, 0)
	dt.waitForDialog("SnapshotActionDialog")
	dt.press(tcell.KeyRune, '5')
	dt.press(tcell.KeyEnter, 0)
	dt.waitForDialog("ErrorDialog")
	assert.False(t, dt.hasDialog("DestroySnapshotsDialog"))
	require.Eventually(t, func() bool {
		return strings.Contains(dt.screenText(), "pool/data/child@daily-1 (syncoid_host)")
	}, 2*time.Second, 10*time.Millisecond)

	previews, destroys := dt.calls()
	assert.Len(t, previews, 1, "no dry run for the held snapshots")
	assert.Empty(t, destroys)
	dt.mu.Lock()
	defer dt.mu.Unlock()
	require.Len(t, dt.holdsListed, 2)
	assert.False(t, dt.holdsListed[0].recursive)
	assert.True(t, dt.holdsListed[1].recursive, "holds are listed like the destroy works")
	assert.False(t, dt.holdsListed[1].onUiThread)
}
