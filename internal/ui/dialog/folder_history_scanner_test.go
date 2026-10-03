package dialog

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
	"zfs-file-history/internal/folder_listing"
	"zfs-file-history/internal/zfs"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeDataset simulates a dataset with snapshots in a temporary directory: the snapshots are directories in
// <root>/.zfs/snapshot/<name>, like ZFS shows them.
type fakeDataset struct {
	t         *testing.T
	root      string
	dataset   *zfs.Dataset
	snapshots []*zfs.Snapshot
}

func newFakeDataset(t *testing.T) *fakeDataset {
	root := t.TempDir()
	return &fakeDataset{
		t:       t,
		root:    root,
		dataset: &zfs.Dataset{Path: root, HiddenZfsPath: filepath.Join(root, ".zfs")},
	}
}

// writeFile writes a file relative to the dataset (base is "" for the working copy) with a fixed modification time.
func (f *fakeDataset) writeFile(base string, relativePath string, content string, modTime time.Time) {
	path := filepath.Join(base, relativePath)
	require.NoError(f.t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(f.t, os.WriteFile(path, []byte(content), 0o644))
	require.NoError(f.t, os.Chtimes(path, modTime, modTime))
}

// addSnapshot creates a snapshot with the given files (relative path -> content), all with the same modification
// time, so unchanged files compare as equal.
func (f *fakeDataset) addSnapshot(name string, created time.Time, files map[string]string) *zfs.Snapshot {
	base := filepath.Join(f.root, ".zfs", "snapshot", name)
	snapshot := &zfs.Snapshot{Name: name, Path: base, ParentDataset: f.dataset, Properties: zfs.SnapshotProperties{CreationDate: created}}
	require.NoError(f.t, os.MkdirAll(base, 0o755))
	for path, content := range files {
		f.writeFile(base, path, content, fileTime)
	}
	f.snapshots = append(f.snapshots, snapshot)
	return snapshot
}

// snapshot returns the snapshot with the given name.
func (f *fakeDataset) snapshot(name string) *zfs.Snapshot {
	for _, snapshot := range f.snapshots {
		if snapshot.Name == name {
			return snapshot
		}
	}
	f.t.Fatalf("no snapshot %q", name)
	return nil
}

var fileTime = time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

func day(n int) time.Time {
	return time.Date(2026, 10, n, 0, 0, 0, 0, time.UTC)
}

func changeSummary(changes []*folder_listing.Change) map[string]folder_listing.ChangeKind {
	result := map[string]folder_listing.ChangeKind{}
	for _, change := range changes {
		result[change.Name] = change.Kind
	}
	return result
}

func TestScanFolderHistory(t *testing.T) {
	ds := newFakeDataset(t)
	// added in reverse order: the scanner sorts by creation date
	ds.addSnapshot("d5", day(5), map[string]string{"docs/b.txt": "bb", "docs/c.txt": "c"})                    // a deleted
	ds.addSnapshot("d4", day(4), map[string]string{"docs/a.txt": "a", "docs/b.txt": "bb", "docs/c.txt": "c"}) // b modified, c added
	ds.addSnapshot("d3", day(3), map[string]string{"docs/a.txt": "a", "docs/b.txt": "b"})                     // unchanged
	ds.addSnapshot("d2", day(2), map[string]string{"docs/a.txt": "a", "docs/b.txt": "b"})                     // created
	ds.addSnapshot("d1", day(1), map[string]string{"other.txt": "x"})                                         // missing
	ds.writeFile(ds.root, "docs/c.txt", "c", fileTime)

	history, err := scanFolderHistory(context.Background(), filepath.Join(ds.root, "docs"), ds.snapshots)
	require.NoError(t, err)

	var states []folderVersionState
	for _, version := range history.All {
		states = append(states, version.State)
	}
	assert.Equal(t, []folderVersionState{folderVersionMissing, folderVersionCreated, folderVersionUnchanged, folderVersionChanged, folderVersionChanged}, states,
		"created in d2: it was not in the older d1")
	assert.Equal(t, 2, history.All[2].Index)

	var changedNames []string
	for _, version := range history.Changed {
		changedNames = append(changedNames, version.Snapshot.Name)
	}
	assert.Equal(t, []string{"d5", "d4", "d2"}, changedNames, "newest first, only snapshots with changes")

	assert.Equal(t, map[string]folder_listing.ChangeKind{"a.txt": folder_listing.Deleted}, changeSummary(history.Changed[0].Changes))
	assert.Equal(t, map[string]folder_listing.ChangeKind{"b.txt": folder_listing.Modified, "c.txt": folder_listing.Added}, changeSummary(history.Changed[1].Changes))
	assert.Equal(t, map[string]folder_listing.ChangeKind{"a.txt": folder_listing.Added, "b.txt": folder_listing.Added}, changeSummary(history.Changed[2].Changes))

	assert.True(t, history.WorkingCopy.Exists)
	assert.Len(t, history.WorkingCopy.Entries, 1)
	assert.Equal(t, int64(3), history.Changed[0].Listing.TotalSize(), "b.txt and c.txt")
}

func TestScanFolderHistory_FolderDeletedAndCanceled(t *testing.T) {
	ds := newFakeDataset(t)
	ds.addSnapshot("d1", day(1), map[string]string{"docs/a.txt": "a"})
	ds.addSnapshot("d2", day(2), map[string]string{"other.txt": "x"})

	history, err := scanFolderHistory(context.Background(), filepath.Join(ds.root, "docs"), ds.snapshots)
	require.NoError(t, err)
	assert.Equal(t, folderVersionInitial, history.All[0].State, "in the oldest snapshot")
	assert.Equal(t, folderVersionDeleted, history.Changed[0].State)
	assert.Equal(t, map[string]folder_listing.ChangeKind{"a.txt": folder_listing.Deleted}, changeSummary(history.Changed[0].Changes))
	assert.False(t, history.WorkingCopy.Exists)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = scanFolderHistory(ctx, filepath.Join(ds.root, "docs"), ds.snapshots)
	assert.ErrorIs(t, err, context.Canceled)
}
