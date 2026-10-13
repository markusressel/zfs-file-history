package zfs

import (
	"context"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupSnapshotTree creates a dataset mounted at a temporary folder with the snapshots (newest first) and their
// files, like below .zfs/snapshot, and returns the mountpoint.
func setupSnapshotTree(t *testing.T, snapshots map[string][]string, order []string) string {
	mountpoint := t.TempDir()
	for name, files := range snapshots {
		for _, file := range files {
			filePath := filepath.Join(mountpoint, ".zfs", "snapshot", name, file)
			require.NoError(t, os.MkdirAll(filepath.Dir(filePath), 0o755))
			require.NoError(t, os.WriteFile(filePath, []byte(file), 0o644))
		}
	}
	originalMounts, originalSnapshots := readZfsMounts, listSnapshotNames
	readZfsMounts = func() ([]Mount, error) { return []Mount{{Name: "tank/home", Mountpoint: mountpoint}}, nil }
	listSnapshotNames = func(dataset string) ([]string, error) { return order, nil }
	t.Cleanup(func() { readZfsMounts, listSnapshotNames = originalMounts, originalSnapshots })
	return mountpoint
}

func TestFindObjects(t *testing.T) {
	mountpoint := setupSnapshotTree(t, map[string][]string{
		"new": {"user/kept"},
		"mid": {"user/kept", "user/deleted/file"},
		"old": {"user/deleted/file", "other"},
	}, []string{"new", "mid", "old"})
	snapshotFile := func(snapshot string, file string) string {
		return filepath.Join(mountpoint, ".zfs", "snapshot", snapshot, file)
	}
	// on ZFS, the file keeps its inode in all snapshots; here, the inode of a copy in one of them
	deleted := inodeOf(snapshotFile("mid", "user/deleted/file"))
	kept := inodeOf(snapshotFile("new", "user/kept"))
	require.NotZero(t, deleted)
	index := NewDataErrorIndex([]DataError{
		{Dataset: "tank/home", Object: deleted, Entry: "tank/home:<0x1>"},
		{Dataset: "tank/home", Object: kept, Snapshot: "new", Entry: "tank/home@new:<0x2>"},
		{Dataset: "tank/home", Object: 999999999, Entry: "tank/home:<0x3>"},
		// not searched: no dataset, or a path is known
		{Object: 5, Entry: "<0x5>:<0x5>"},
		{Dataset: "tank/home", Path: "/user/kept", Entry: "/user/kept"},
	}, nil)

	var progress []ObjectSearchProgress
	locations, err := FindObjects(context.Background(), index, func(p ObjectSearchProgress) { progress = append(progress, p) })

	require.NoError(t, err)
	assert.Equal(t, []ObjectLocation{{Snapshot: "mid", Path: "/user/deleted/file", LivePath: filepath.Join(mountpoint, "user/deleted/file"),
		File: snapshotFile("mid", "user/deleted/file")}}, locations[ObjectKey{"tank/home", deleted}],
		"found in mid; in old, the file at the same path has another inode here")
	assert.Equal(t, []ObjectLocation{{Snapshot: "new", Path: "/user/kept", LivePath: filepath.Join(mountpoint, "user/kept"),
		File: snapshotFile("new", "user/kept")}}, locations[ObjectKey{"tank/home", kept}], "only searched in its snapshot")
	notFound, searched := locations[ObjectKey{"tank/home", 999999999}]
	assert.True(t, searched, "searched")
	assert.Empty(t, notFound, "but not found")
	assert.Len(t, locations, 3)

	last := progress[len(progress)-1]
	assert.Equal(t, ObjectSearchProgress{Done: 3, Total: 3, Found: 2, Wanted: 3, Entries: last.Entries}, last)

	// the snapshot of a location was destroyed
	require.NoError(t, os.RemoveAll(filepath.Join(mountpoint, ".zfs", "snapshot", "mid")))
	existing := ExistingLocations(locations)
	assert.Empty(t, existing[ObjectKey{"tank/home", deleted}])
	assert.Len(t, existing[ObjectKey{"tank/home", kept}], 1)
}

func TestFindObjects_Canceled(t *testing.T) {
	setupSnapshotTree(t, map[string][]string{"new": {"a"}}, []string{"new"})
	index := NewDataErrorIndex([]DataError{{Dataset: "tank/home", Object: 1}}, nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := FindObjects(ctx, index, func(ObjectSearchProgress) {})

	assert.ErrorIs(t, err, context.Canceled)
}

func TestFindObjects_NotMounted(t *testing.T) {
	setupSnapshotTree(t, nil, nil)
	index := NewDataErrorIndex([]DataError{{Dataset: "tank/other", Object: 1}}, nil)

	locations, err := FindObjects(context.Background(), index, func(ObjectSearchProgress) {})

	// Regression: objects that could not be searched were reported as found in no snapshot
	require.NoError(t, err)
	assert.Empty(t, locations, "not searched, so not in the result")
	assert.True(t, index.HasUnresolvedObjects())
	assert.False(t, NewDataErrorIndex(nil, nil).HasUnresolvedObjects())
}

func TestParseDirents(t *testing.T) {
	// two struct linux_dirent64, padded to 8 bytes
	record := func(inode uint64, direntType byte, name string) []byte {
		length := (19 + len(name) + 1 + 7) / 8 * 8
		buffer := make([]byte, length)
		binary.NativeEndian.PutUint64(buffer, inode)
		binary.NativeEndian.PutUint16(buffer[16:], uint16(length))
		buffer[18] = direntType
		copy(buffer[19:], name)
		return buffer
	}
	buffer := append(record(42, 4, "folder"), record(0xa0f02, 8, "file name")...)

	type entry struct {
		name  string
		inode uint64
		kind  byte
	}
	var entries []entry
	parseDirents(buffer, func(name string, inode uint64, direntType byte) {
		entries = append(entries, entry{name, inode, direntType})
	})
	assert.Equal(t, []entry{{"folder", 42, 4}, {"file name", 0xa0f02, 8}}, entries)

	// cut off
	entries = nil
	parseDirents(buffer[:30], func(name string, inode uint64, direntType byte) {
		entries = append(entries, entry{name, inode, direntType})
	})
	assert.Empty(t, entries)
}

func TestFindObjects_SeveralSnapshots(t *testing.T) {
	mountpoint := setupSnapshotTree(t, map[string][]string{"a": {"file"}, "b": {"other"}, "c": {"file"}}, []string{"c", "b", "a"})
	object := inodeOf(filepath.Join(mountpoint, ".zfs", "snapshot", "a", "file"))
	// with head_errlog, ZFS reports an object for each snapshot that holds it
	index := NewDataErrorIndex([]DataError{
		{Dataset: "tank/home", Object: object, Snapshot: "a"},
		{Dataset: "tank/home", Object: object, Snapshot: "b"},
	}, nil)

	locations, err := FindObjects(context.Background(), index, func(ObjectSearchProgress) {})

	// Regression: only the last reported snapshot was searched
	require.NoError(t, err)
	found := locations[ObjectKey{"tank/home", object}]
	require.Len(t, found, 1, "not in c, which it was not reported in")
	assert.Equal(t, "a", found[0].Snapshot)
}
