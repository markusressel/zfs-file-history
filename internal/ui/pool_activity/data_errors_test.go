package pool_activity

import (
	"context"
	"sync"
	"testing"
	"time"
	"zfs-file-history/internal/testutil"
	"zfs-file-history/internal/zfs"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeDataErrors counts the loads of the damaged files, and returns an index with one damaged file.
type fakeDataErrors struct {
	mu    sync.Mutex
	loads int
}

func (f *fakeDataErrors) load() (*zfs.DataErrorIndex, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.loads++
	return zfs.NewDataErrorIndex([]zfs.DataError{{Pool: "rpool", Entry: "/home/file", LivePath: "/home/file"}}, nil), nil
}

func (f *fakeDataErrors) getLoads() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.loads
}

func TestMonitor_DataErrors(t *testing.T) {
	dataErrors := &fakeDataErrors{}
	// a scrub runs, so the statuses are reloaded every activeRefreshInterval
	running := scrubbing("tank", 10)
	app, monitor, _, fake, _ := setupAlertTestWith(t, []*zfs.PoolStatus{withDataErrors("rpool"), running}, dataErrors.load)

	var mu sync.Mutex
	changes := 0
	testutil.OnUiThread(t, app, func() {
		monitor.OnDataErrorsChanged(func() {
			mu.Lock()
			changes++
			mu.Unlock()
		})
	})
	require.Eventually(t, func() bool { return dataErrors.getLoads() == 1 }, 2*time.Second, 10*time.Millisecond)
	testutil.OnUiThread(t, app, func() { assert.Equal(t, 1, monitor.DataErrors().Len()) })

	// not loaded again while the data errors stay the same, though the statuses are
	loads := fake.getLoads()
	require.Eventually(t, func() bool { return fake.getLoads() > loads+2 }, 2*time.Second, 10*time.Millisecond)
	assert.Equal(t, 1, dataErrors.getLoads())

	// but when they changed
	more := withDataErrors("rpool")
	more.Errors = "19 data errors, use '-v' for a list"
	fake.set([]*zfs.PoolStatus{more, running}, nil)
	require.Eventually(t, func() bool { return dataErrors.getLoads() == 2 }, 2*time.Second, 10*time.Millisecond)

	// and when asked to, e.g. after a snapshot was destroyed
	testutil.OnUiThread(t, app, monitor.RefreshDataErrors)
	require.Eventually(t, func() bool { return dataErrors.getLoads() == 3 }, 2*time.Second, 10*time.Millisecond)

	// none left: no list
	fake.set([]*zfs.PoolStatus{idle("rpool"), running}, nil)
	require.Eventually(t, func() bool {
		var index *zfs.DataErrorIndex
		testutil.OnUiThread(t, app, func() { index = monitor.DataErrors() })
		return index == nil
	}, 2*time.Second, 10*time.Millisecond)
	assert.Equal(t, 3, dataErrors.getLoads())
	mu.Lock()
	defer mu.Unlock()
	assert.Positive(t, changes)
}

func TestMonitor_NoDataErrors(t *testing.T) {
	dataErrors := &fakeDataErrors{}
	app, monitor, _, fake, _ := setupAlertTestWith(t, []*zfs.PoolStatus{idle("rpool")}, dataErrors.load)
	require.Eventually(t, func() bool { return fake.getLoads() == 1 }, 2*time.Second, 10*time.Millisecond)

	// healthy pools: zpool status -v (with sudo) is not run
	testutil.OnUiThread(t, app, monitor.RefreshDataErrors)
	require.Eventually(t, func() bool { return fake.getLoads() == 2 }, 2*time.Second, 10*time.Millisecond)
	testutil.OnUiThread(t, app, func() { assert.Nil(t, monitor.DataErrors()) })
	assert.Equal(t, 0, dataErrors.getLoads())
}

func TestDataErrorSummary(t *testing.T) {
	assert.Empty(t, dataErrorSummary([]*zfs.PoolStatus{idle("rpool")}))
	assert.Equal(t, "rpool: 18 data errors\ntank: 18 data errors",
		dataErrorSummary([]*zfs.PoolStatus{withDataErrors("rpool"), idle("bpool"), withDataErrors("tank")}))
}

func TestMonitor_FindObjects(t *testing.T) {
	object := zfs.DataError{Pool: "rpool", Entry: "rpool/home:<0x20>", Dataset: "rpool/home", Object: 0x20}
	app, monitor, _, _, _ := setupAlertTestWith(t, []*zfs.PoolStatus{withDataErrors("rpool")}, func() (*zfs.DataErrorIndex, error) {
		return zfs.NewDataErrorIndex([]zfs.DataError{object}, nil), nil
	})
	file := t.TempDir()
	testutil.Replace(t, &findObjects, func(ctx context.Context, index *zfs.DataErrorIndex, progress func(zfs.ObjectSearchProgress)) (map[zfs.ObjectKey][]zfs.ObjectLocation, error) {
		progress(zfs.ObjectSearchProgress{Total: 1, Done: 1, Found: 1, Wanted: 1})
		return map[zfs.ObjectKey][]zfs.ObjectLocation{
			{Dataset: "rpool/home", Object: 0x20}: {{Snapshot: "daily", Path: "/x", LivePath: "/home/x", File: file}},
		}, nil
	})
	require.Eventually(t, func() bool {
		loaded := false
		testutil.OnUiThread(t, app, func() { loaded = monitor.DataErrors().HasUnresolvedObjects() })
		return loaded
	}, 2*time.Second, 10*time.Millisecond)

	var mu sync.Mutex
	searchChanges := 0
	testutil.OnUiThread(t, app, func() {
		monitor.OnObjectSearchChanged(func() {
			mu.Lock()
			searchChanges++
			mu.Unlock()
		})
		monitor.FindObjects()
		assert.True(t, monitor.ObjectSearch().Running)
	})
	require.Eventually(t, func() bool {
		done := false
		testutil.OnUiThread(t, app, func() { done = !monitor.ObjectSearch().Running })
		return done
	}, 2*time.Second, 10*time.Millisecond)
	testutil.OnUiThread(t, app, func() {
		search := monitor.ObjectSearch()
		assert.NoError(t, search.Err)
		assert.Equal(t, 1, search.Progress.Found)
		assert.True(t, monitor.DataErrors().IsDamaged("/home/x", "rpool/home@daily"), "the found copy is marked")
	})
	mu.Lock()
	assert.GreaterOrEqual(t, searchChanges, 2, "started and ended")
	mu.Unlock()

	// reloaded: the locations stay, unless their file is gone (the temporary folder has another inode than the object)
	testutil.OnUiThread(t, app, monitor.RefreshDataErrors)
	require.Eventually(t, func() bool {
		pruned := false
		testutil.OnUiThread(t, app, func() {
			errors := monitor.DataErrors().Errors
			pruned = len(errors) == 1 && errors[0].Searched && len(errors[0].Locations) == 0
		})
		return pruned
	}, 2*time.Second, 10*time.Millisecond)
}

func TestMonitor_FindObjects_NothingToFind(t *testing.T) {
	app, monitor, _, fake, _ := setupAlertTest(t, []*zfs.PoolStatus{idle("rpool")})
	require.Eventually(t, func() bool { return fake.getLoads() == 1 }, 2*time.Second, 10*time.Millisecond)

	testutil.OnUiThread(t, app, func() {
		monitor.FindObjects()
		assert.Nil(t, monitor.ObjectSearch())
	})
}

func TestMonitor_RefreshDataErrorsThrottled(t *testing.T) {
	dataErrors := &fakeDataErrors{}
	app, monitor, _, fake, _ := setupAlertTestWith(t, []*zfs.PoolStatus{withDataErrors("rpool")}, dataErrors.load)
	require.Eventually(t, func() bool { return dataErrors.getLoads() == 1 }, 2*time.Second, 10*time.Millisecond)
	// within the interval (0 in these tests): the damaged files were loaded in the future
	testutil.OnUiThread(t, app, func() { monitor.dataErrorsLoaded = time.Now().Add(time.Hour) })

	// e.g. sanoid creates snapshots every minute: sudo zpool status -v is not run for each
	loads := fake.getLoads()
	testutil.OnUiThread(t, app, monitor.RefreshDataErrors)
	require.Eventually(t, func() bool { return fake.getLoads() > loads }, 2*time.Second, 10*time.Millisecond)
	time.Sleep(50 * time.Millisecond)
	assert.Equal(t, 1, dataErrors.getLoads())

	// but after the interval, with a later load of the statuses
	testutil.OnUiThread(t, app, func() { monitor.dataErrorsLoaded = time.Time{} })
	testutil.OnUiThread(t, app, monitor.Refresh)
	require.Eventually(t, func() bool { return dataErrors.getLoads() == 2 }, 2*time.Second, 10*time.Millisecond)
}

func TestMonitor_SearchEndsWhileLoading(t *testing.T) {
	object := zfs.DataError{Pool: "rpool", Entry: "rpool/home:<0x20>", Dataset: "rpool/home", Object: 0x20}
	release := make(chan struct{})
	var mu sync.Mutex
	loads := 0
	app, monitor, _, _, _ := setupAlertTestWith(t, []*zfs.PoolStatus{withDataErrors("rpool")}, func() (*zfs.DataErrorIndex, error) {
		mu.Lock()
		loads++
		second := loads == 2
		mu.Unlock()
		if second {
			<-release
		}
		return zfs.NewDataErrorIndex([]zfs.DataError{object}, nil), nil
	})
	testutil.Replace(t, &findObjects, func(ctx context.Context, index *zfs.DataErrorIndex, progress func(zfs.ObjectSearchProgress)) (map[zfs.ObjectKey][]zfs.ObjectLocation, error) {
		return map[zfs.ObjectKey][]zfs.ObjectLocation{
			{Dataset: "rpool/home", Object: 0x20}: {{Snapshot: "daily", Path: "/x", LivePath: "/home/x", File: "/nonexistent"}},
		}, nil
	})
	damaged := func() bool {
		result := false
		testutil.OnUiThread(t, app, func() { result = monitor.DataErrors().IsDamaged("/home/x", "rpool/home@daily") })
		return result
	}
	require.Eventually(t, func() bool {
		loaded := false
		testutil.OnUiThread(t, app, func() { loaded = monitor.DataErrors().HasUnresolvedObjects() })
		return loaded
	}, 2*time.Second, 10*time.Millisecond)

	// a reload starts (and waits), the search ends meanwhile
	testutil.OnUiThread(t, app, monitor.RefreshDataErrors)
	require.Eventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return loads == 2
	}, 2*time.Second, 10*time.Millisecond)
	testutil.OnUiThread(t, app, monitor.FindObjects)
	require.Eventually(t, damaged, 2*time.Second, 10*time.Millisecond)

	// Regression: the reload, which started before the search ended, dropped the found locations
	close(release)
	time.Sleep(100 * time.Millisecond)
	assert.True(t, damaged())
}
