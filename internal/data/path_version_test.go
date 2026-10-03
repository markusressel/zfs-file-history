package data

import (
	"os"
	"testing"
	"time"
	"zfs-file-history/internal/testutil"
	"zfs-file-history/internal/zfs"

	"github.com/stretchr/testify/assert"
)

func TestVersionChanges(t *testing.T) {
	day := func(n int) time.Time { return time.Date(2026, 10, n, 0, 0, 0, 0, time.UTC) }
	version := func(name string, created int, info os.FileInfo) PathVersion {
		return PathVersion{Snapshot: &zfs.Snapshot{Name: name, Properties: zfs.SnapshotProperties{CreationDate: day(created)}}, Info: info}
	}
	v1, v2 := testutil.File(100, day(1)), testutil.File(250, day(3))
	// in any order: s0 without the file, created in s1, the same in s2, grown in s3, the same in s4, deleted in s5
	versions := []PathVersion{
		version("s5", 6, nil), version("s3", 4, v2), version("s1", 2, v1),
		version("s0", 1, nil), version("s4", 5, v2), version("s2", 3, v1),
	}
	changes := VersionChanges(versions)
	assert.Equal(t, map[string]VersionChange{
		"s0": {Kind: VersionUnchanged},
		"s1": {Kind: VersionCreated, SizeDelta: 100},
		"s2": {Kind: VersionUnchanged},
		"s3": {Kind: VersionModified, SizeDelta: 150},
		"s4": {Kind: VersionUnchanged},
		"s5": {Kind: VersionDeleted, SizeDelta: -250},
	}, changes)
	assert.True(t, changes["s3"].IsNewVersion())
	assert.False(t, changes["s2"].IsNewVersion())

	// the oldest snapshot contains the file: its first version, nothing to compare with
	changes = VersionChanges([]PathVersion{version("s2", 3, v1), version("s1", 2, v1)})
	assert.Equal(t, map[string]VersionChange{"s1": {Kind: VersionInitial}, "s2": {Kind: VersionUnchanged}}, changes)
	assert.True(t, changes["s1"].IsNewVersion())

	// folders: the change of the number of items (their size on ZFS)
	changes = VersionChanges([]PathVersion{version("s1", 2, testutil.Folder(3, day(1))), version("s2", 3, testutil.Folder(5, day(2)))})
	assert.Equal(t, VersionChange{Kind: VersionModified, SizeDelta: 2}, changes["s2"])
}
