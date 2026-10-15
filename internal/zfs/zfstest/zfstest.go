// Package zfstest has helpers for the tests of the packages that use internal/zfs.
package zfstest

import (
	"testing"
	"zfs-file-history/internal/testutil"
	"zfs-file-history/internal/util"
	"zfs-file-history/internal/zfs"
)

// UseNewEmitters replaces the emitters of the zfs package with new ones until the end of the test (see
// testutil.Replace), for a test that creates something that subscribes to them (e.g. the pages). Subscriptions are
// never removed: without new emitters, an emit of a later test reached the UI of every earlier one, and queued
// updates for apps that had stopped long ago.
func UseNewEmitters(t testing.TB) {
	t.Helper()
	testutil.Replace(t, &zfs.DatasetsLoaded, util.NewEmitter[struct{}]())
	testutil.Replace(t, &zfs.PermissionsChanged, util.NewEmitter[struct{}]())
	testutil.Replace(t, &zfs.PoolsChanged, util.NewEmitter[struct{}]())
	testutil.Replace(t, &zfs.PoolHistoryRecorded, util.NewEmitter[zfs.PoolHistoryEntry]())
	testutil.Replace(t, &zfs.SudoersChanged, util.NewEmitter[struct{}]())
}
