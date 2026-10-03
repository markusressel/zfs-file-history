package folder_listing

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
	"zfs-file-history/internal/zfs"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var fileTime = time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

func summary(changes []*Change) map[string]ChangeKind {
	result := map[string]ChangeKind{}
	for _, change := range changes {
		result[change.Name] = change.Kind
	}
	return result
}

func TestCompare(t *testing.T) {
	file := func(name string, size int64, modTime time.Time) Entry {
		return Entry{Name: name, Type: File, Size: size, Mode: 0o644, ModTime: modTime}
	}
	before := Listing{Exists: true, Entries: map[string]Entry{
		"same":    file("same", 1, fileTime),
		"size":    file("size", 1, fileTime),
		"time":    file("time", 1, fileTime),
		"gone":    file("gone", 1, fileTime),
		"type":    file("type", 1, fileTime),
		"subdir":  {Name: "subdir", Type: Directory, Size: 4096, ModTime: fileTime},
		"subdir2": {Name: "subdir2", Type: Directory, Size: 4096, ModTime: fileTime},
	}}
	after := Listing{Exists: true, Entries: map[string]Entry{
		"same":    file("same", 1, fileTime),
		"size":    file("size", 2, fileTime),
		"time":    file("time", 1, fileTime.Add(time.Second)),
		"new":     file("new", 1, fileTime),
		"type":    {Name: "type", Type: Link, Mode: os.ModeSymlink, ModTime: fileTime},
		"subdir":  {Name: "subdir", Type: Directory, Size: 8192, ModTime: fileTime}, // size of directories is ignored
		"subdir2": {Name: "subdir2", Type: Directory, Size: 4096, ModTime: fileTime.Add(time.Hour)},
	}}

	changes := Compare(before, after)

	var names []string
	for _, change := range changes {
		names = append(names, change.Name)
	}
	assert.Equal(t, []string{"gone", "new", "size", "subdir2", "time", "type"}, names, "sorted by name")
	assert.Equal(t, map[string]ChangeKind{
		"gone": Deleted, "new": Added, "size": Modified,
		"subdir2": Modified, "time": Modified, "type": Modified,
	}, summary(changes))
	added, deleted, modified := CountChanges(changes)
	assert.Equal(t, []int{1, 1, 4}, []int{added, deleted, modified})
	assert.Nil(t, changes[0].After)
	assert.Nil(t, changes[1].Before)
}

func TestRead(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "file"), []byte("abc"), 0o644))
	require.NoError(t, os.Mkdir(filepath.Join(root, "dir"), 0o755))
	require.NoError(t, os.Symlink("file", filepath.Join(root, "link")))

	listing := Read(root)

	assert.True(t, listing.Exists)
	assert.Equal(t, File, listing.Entries["file"].Type)
	assert.Equal(t, int64(3), listing.Entries["file"].Size)
	assert.Equal(t, Directory, listing.Entries["dir"].Type)
	assert.Equal(t, Link, listing.Entries["link"].Type, "links are not followed")
	assert.Equal(t, int64(3), listing.TotalSize())

	assert.False(t, Read(filepath.Join(root, "missing")).Exists)
}

func TestReadInSnapshots(t *testing.T) {
	root := t.TempDir()
	dataset := &zfs.Dataset{Path: root, HiddenZfsPath: filepath.Join(root, ".zfs")}
	var snapshots []*zfs.Snapshot
	for i, name := range []string{"s1", "s2", "s3"} {
		base := filepath.Join(root, ".zfs", "snapshot", name)
		snapshots = append(snapshots, &zfs.Snapshot{Name: name, Path: base, ParentDataset: dataset})
		if i > 0 {
			// the folder does not exist in s1, and has i files in s2 and s3
			require.NoError(t, os.MkdirAll(filepath.Join(base, "docs"), 0o755))
			for j := 0; j < i; j++ {
				require.NoError(t, os.WriteFile(filepath.Join(base, "docs", string(rune('a'+j))), nil, 0o644))
			}
		}
	}

	listings, err := ReadInSnapshots(context.Background(), filepath.Join(root, "docs"), snapshots)
	require.NoError(t, err)
	require.Len(t, listings, 3)
	assert.False(t, listings[0].Exists)
	assert.Len(t, listings[1].Entries, 1)
	assert.Len(t, listings[2].Entries, 2, "in the order of the snapshots")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = ReadInSnapshots(ctx, filepath.Join(root, "docs"), snapshots)
	assert.ErrorIs(t, err, context.Canceled)
}

func TestNewEntry(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "file"), []byte("abc"), 0o644))
	info, err := os.Lstat(filepath.Join(root, "file"))
	require.NoError(t, err)

	// the same as in a listing, so entries made from file infos (e.g. of the file browser) compare equal
	assert.Equal(t, Read(root).Entries["file"], NewEntry("file", info))
}

func TestCompareSnapshots(t *testing.T) {
	root := t.TempDir()
	dataset := &zfs.Dataset{Path: root, HiddenZfsPath: filepath.Join(root, ".zfs")}
	write := func(path string, content string) {
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
		require.NoError(t, os.Chtimes(path, fileTime, fileTime))
	}
	// s1: no folder, s2: a, s3: a b, s4: a b(changed); now: b(changed) c
	files := map[string]map[string]string{
		"s2": {"a": "a"},
		"s3": {"a": "a", "b": "b"},
		"s4": {"a": "a", "b": "bb"},
	}
	var snapshots []*zfs.Snapshot
	for _, name := range []string{"s4", "s1", "s3", "s2"} { // in any order
		base := filepath.Join(root, ".zfs", "snapshot", name)
		day := map[string]int{"s1": 1, "s2": 2, "s3": 3, "s4": 4}[name]
		snapshots = append(snapshots, &zfs.Snapshot{Name: name, Path: base, ParentDataset: dataset,
			Properties: zfs.SnapshotProperties{CreationDate: fileTime.AddDate(0, 0, day)}})
		require.NoError(t, os.MkdirAll(base, 0o755))
		for file, content := range files[name] {
			write(filepath.Join(base, "docs", file), content)
		}
	}
	workingCopy := Listing{Exists: true, Entries: map[string]Entry{
		"b": {Name: "b", Type: File, Size: 2, Mode: 0o644, ModTime: fileTime},
		"c": {Name: "c", Type: File, Size: 1, Mode: 0o644, ModTime: fileTime},
	}}

	changes, err := CompareSnapshots(context.Background(), filepath.Join(root, "docs"), snapshots, workingCopy)
	require.NoError(t, err)
	assert.Equal(t, map[string]SnapshotChanges{
		"s1": {Exists: false, Initial: true},
		// created: all its entries are added
		"s2": {Exists: true, VsNow: Counts{Added: 2, Deleted: 1}, VsPrevious: Counts{Added: 1}},
		"s3": {Exists: true, VsNow: Counts{Added: 1, Deleted: 1, Modified: 1}, VsPrevious: Counts{Added: 1}},
		"s4": {Exists: true, VsNow: Counts{Added: 1, Deleted: 1}, VsPrevious: Counts{Modified: 1}},
	}, changes)
	assert.Equal(t, 2, changes["s4"].VsNow.Total())
}
