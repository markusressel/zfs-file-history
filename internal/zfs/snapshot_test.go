package zfs

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"syscall"
	"testing"
	"time"
	"zfs-file-history/internal/data/diff_state"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fileState struct {
	exists  bool
	content string
	modTime time.Time
}

func TestDetermineDiffState(t *testing.T) {
	// 1. Setup baseline shared structs
	tempDir := t.TempDir()
	datasetPath := filepath.Join(tempDir, "dataset")
	snapDirBase := filepath.Join(datasetPath, ".zfs", "snapshot")
	snapPath := filepath.Join(snapDirBase, "snap1")

	dataset := &Dataset{
		Path:          datasetPath,
		HiddenZfsPath: filepath.Join(datasetPath, ".zfs"),
	}
	snapshot := &Snapshot{
		Name:          "snap1",
		Path:          snapPath,
		ParentDataset: dataset,
	}

	now := time.Now().Truncate(time.Second)

	// 2. The Table: Declare all scenarios cleanly as data
	tests := []struct {
		name      string
		filename  string
		realState fileState
		snapState fileState
		wantState diff_state.DiffState
	}{
		{
			name:      "Added",
			filename:  "added.txt",
			realState: fileState{exists: true, content: "content"},
			snapState: fileState{exists: false},
			wantState: diff_state.Added,
		},
		{
			name:      "Equal",
			filename:  "equal.txt",
			realState: fileState{exists: true, content: "identical content", modTime: now},
			snapState: fileState{exists: true, content: "identical content", modTime: now},
			wantState: diff_state.Equal,
		},
		{
			name:      "Modified",
			filename:  "modified.txt",
			realState: fileState{exists: true, content: "new content", modTime: now},
			snapState: fileState{exists: true, content: "old content", modTime: now.Add(-time.Hour)},
			wantState: diff_state.Modified,
		},
		{
			name:      "Deleted",
			filename:  "deleted.txt",
			realState: fileState{exists: false},
			snapState: fileState{exists: true, content: "was here"},
			wantState: diff_state.Deleted,
		},
		{
			name:      "NeitherExists",
			filename:  "nonexistent.txt",
			realState: fileState{exists: false},
			snapState: fileState{exists: false},
			wantState: diff_state.Equal,
		},
	}

	// 3. Execution Loop
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			realFilePath := filepath.Join(datasetPath, tt.filename)
			snapFilePath := filepath.Join(snapPath, tt.filename)

			// Automatically handle filesystem setups based on table configurations
			setupFile(t, realFilePath, tt.realState)
			setupFile(t, snapFilePath, tt.snapState)

			// Run assertion
			gotState := snapshot.DetermineDiffState(realFilePath)
			if gotState != tt.wantState {
				t.Errorf("DetermineDiffState() = %v, want %v", gotState, tt.wantState)
			}
		})
	}
}

// Helper function to keep execution loop completely readable
func setupFile(t *testing.T, path string, state fileState) {
	t.Helper() // Flags this function as a test helper for clearer stack traces
	if !state.exists {
		return
	}

	// Ensure directories exist
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("failed to create dir for %s: %v", path, err)
	}

	// Write content
	if err := os.WriteFile(path, []byte(state.content), 0644); err != nil {
		t.Fatalf("failed to write file %s: %v", path, err)
	}

	// Adjust times if explicitly requested
	if !state.modTime.IsZero() {
		if err := os.Chtimes(path, state.modTime, state.modTime); err != nil {
			t.Fatalf("failed to set times on %s: %v", path, err)
		}
	}
}

func TestSnapshot_SuggestCloneName(t *testing.T) {
	snapshot := &Snapshot{Name: "daily-2026", FullName: "pool/data@daily-2026"}
	assert.Equal(t, "pool/data-daily-2026", snapshot.SuggestCloneName())

	assert.Equal(t, "", (&Snapshot{FullName: "invalid"}).SuggestCloneName())
}

func TestSnapshot_CloneRequiresAName(t *testing.T) {
	snapshot := &Snapshot{Name: "daily-2026", FullName: "pool/data@daily-2026"}
	assert.Error(t, snapshot.Clone(""))
	assert.Error(t, snapshot.Clone("   "))
	assert.Error(t, snapshot.Clone("-o"))
}

func TestSnapshot_PathsAndEqual(t *testing.T) {
	dataset := &Dataset{
		Path:          "/tank/data",
		HiddenZfsPath: "/tank/data/.zfs",
	}
	s1 := &Snapshot{
		Name:          "snap1",
		Path:          "/tank/data/.zfs/snapshot/snap1",
		ParentDataset: dataset,
	}
	s2 := &Snapshot{
		Name: "snap1",
		Path: "/tank/data/.zfs/snapshot/snap1",
	}
	s3 := &Snapshot{
		Name: "snap2",
		Path: "/tank/data/.zfs/snapshot/snap2",
	}

	assert.True(t, s1.Equal(*s2))
	assert.False(t, s1.Equal(*s3))

	assert.Equal(t, "/tank/data/.zfs/snapshot/snap1/folder/file.txt", s1.GetSnapshotPath("/tank/data/folder/file.txt"))
	assert.Equal(t, "/tank/data/folder/file.txt", s1.GetRealPath("/tank/data/.zfs/snapshot/snap1/folder/file.txt"))
}

func TestSnapshot_RestoreOperations(t *testing.T) {
	tmpDir := t.TempDir()
	datasetPath := filepath.Join(tmpDir, "dataset")
	snapBase := filepath.Join(datasetPath, ".zfs", "snapshot")
	snapPath := filepath.Join(snapBase, "snap1")

	require.NoError(t, os.MkdirAll(snapPath, 0755))
	require.NoError(t, os.MkdirAll(datasetPath, 0755))

	dataset := &Dataset{
		Path:          datasetPath,
		HiddenZfsPath: filepath.Join(datasetPath, ".zfs"),
	}
	snap := &Snapshot{
		Name:          "snap1",
		Path:          snapPath,
		ParentDataset: dataset,
	}

	// 1. Restore single file
	srcFile := filepath.Join(snapPath, "sub", "hello.txt")
	require.NoError(t, os.MkdirAll(filepath.Dir(srcFile), 0755))
	require.NoError(t, os.WriteFile(srcFile, []byte("hello world"), 0644))

	err := snap.RestoreFile(context.Background(), srcFile)
	require.NoError(t, err)

	dstFile := filepath.Join(datasetPath, "sub", "hello.txt")
	data, err := os.ReadFile(dstFile)
	require.NoError(t, err)
	assert.Equal(t, "hello world", string(data))

	// 2. Restore directory
	srcDir := filepath.Join(snapPath, "somedir")
	require.NoError(t, os.Mkdir(srcDir, 0755))
	stat, err := os.Lstat(srcDir)
	require.NoError(t, err)
	err = snap.RestoreDir(filepath.Join(datasetPath, "somedir"), stat)
	require.NoError(t, err)

	// 3. RestoreRecursive
	nestedDir := filepath.Join(snapPath, "nested")
	require.NoError(t, os.MkdirAll(filepath.Join(nestedDir, "child"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(nestedDir, "f1.txt"), []byte("file1"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(nestedDir, "child", "f2.txt"), []byte("file2"), 0644))

	err = snap.RestoreRecursive(context.Background(), nestedDir, nil)
	require.NoError(t, err)

	assert.FileExists(t, filepath.Join(datasetPath, "nested", "f1.txt"))
	assert.FileExists(t, filepath.Join(datasetPath, "nested", "child", "f2.txt"))

	// 4. Restore generic function on file and dir
	err = snap.Restore(context.Background(), srcFile)
	require.NoError(t, err)
	err = snap.Restore(context.Background(), nestedDir)
	require.NoError(t, err)
}

// Regression: restoring the executable of a running program failed with "text file busy", as it cannot be written
// to. It is replaced instead, the program keeps running its version.
func TestSnapshot_RestoreFileOfARunningProgram(t *testing.T) {
	sleep, err := exec.LookPath("sleep")
	if err != nil {
		t.Skip("no sleep command")
	}
	content, err := os.ReadFile(sleep)
	require.NoError(t, err)

	datasetPath := filepath.Join(t.TempDir(), "dataset")
	snapPath := filepath.Join(datasetPath, ".zfs", "snapshot", "snap1")
	require.NoError(t, os.MkdirAll(filepath.Join(snapPath, "bin"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(datasetPath, "bin"), 0o755))
	snap := &Snapshot{
		Name:          "snap1",
		Path:          snapPath,
		ParentDataset: &Dataset{Path: datasetPath, HiddenZfsPath: filepath.Join(datasetPath, ".zfs")},
	}

	// the snapshot version differs from the running one
	snapshotVersion := append(slices.Clone(content), 0)
	srcFile := filepath.Join(snapPath, "bin", "program")
	require.NoError(t, os.WriteFile(srcFile, snapshotVersion, 0o755))

	realFile := filepath.Join(datasetPath, "bin", "program")
	require.NoError(t, os.WriteFile(realFile, content, 0o755))
	program := exec.Command(realFile, "10")
	if err := program.Start(); err != nil {
		t.Skipf("cannot run programs in the temp dir: %v", err)
	}
	t.Cleanup(func() {
		_ = program.Process.Kill()
		_ = program.Wait()
	})

	require.NoError(t, snap.RestoreFile(context.Background(), srcFile))

	restored, err := os.ReadFile(realFile)
	require.NoError(t, err)
	assert.Equal(t, snapshotVersion, restored)
	stat, err := os.Lstat(realFile)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o755), stat.Mode())

	entries, err := os.ReadDir(filepath.Join(datasetPath, "bin"))
	require.NoError(t, err)
	assert.Len(t, entries, 1, "no temporary file is left")
}

// newRestoreTest returns a snapshot of a dataset in a temp dir, with its path in the snapshot and the path of the
// dataset.
func newRestoreTest(t *testing.T) (*Snapshot, string, string) {
	datasetPath := filepath.Join(t.TempDir(), "dataset")
	snapPath := filepath.Join(datasetPath, ".zfs", "snapshot", "snap1")
	require.NoError(t, os.MkdirAll(snapPath, 0o755))
	return &Snapshot{
		Name:          "snap1",
		Path:          snapPath,
		ParentDataset: &Dataset{Path: datasetPath, HiddenZfsPath: filepath.Join(datasetPath, ".zfs")},
	}, snapPath, datasetPath
}

// writeTree writes a folder with a file and a subfolder with a file into the snapshot, with old times, and returns
// the folder.
func writeTree(t *testing.T, snapPath string) string {
	folder := filepath.Join(snapPath, "folder")
	require.NoError(t, os.MkdirAll(filepath.Join(folder, "child"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(folder, "f1.txt"), []byte("file1"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(folder, "child", "f2.txt"), []byte("file2"), 0o644))
	old := time.Date(2020, 1, 2, 3, 4, 5, 0, time.UTC)
	for _, path := range []string{filepath.Join(folder, "child", "f2.txt"), filepath.Join(folder, "child"), filepath.Join(folder, "f1.txt"), folder} {
		require.NoError(t, os.Chtimes(path, old, old))
	}
	return folder
}

// Regression: the folders restored by RestoreRecursive got the modification time of the restore, as their entries
// were restored after their times were set; the restore ran twice to set them again, copying every file twice.
func TestSnapshot_RestoreRecursiveKeepsTheTimesOfFolders(t *testing.T) {
	snap, snapPath, datasetPath := newRestoreTest(t)
	folder := writeTree(t, snapPath)

	var progress []int
	require.NoError(t, snap.RestoreRecursive(context.Background(), folder, func(done int) { progress = append(progress, done) }))

	for _, name := range []string{"folder", "folder/child"} {
		snapshotStat, err := os.Lstat(filepath.Join(snapPath, name))
		require.NoError(t, err)
		realStat, err := os.Lstat(filepath.Join(datasetPath, name))
		require.NoError(t, err)
		assert.Equal(t, snapshotStat.ModTime(), realStat.ModTime(), name)
	}
	total, err := CountRestoreEntries(context.Background(), folder)
	require.NoError(t, err)
	assert.Equal(t, 4, total, "the folder, its subfolder and their files")
	assert.Equal(t, []int{1, 2, 3, 4}, progress)
}

// A folder without write permission is restored with its entries: its mode is set after them.
func TestSnapshot_RestoreRecursiveReadOnlyFolder(t *testing.T) {
	snap, snapPath, datasetPath := newRestoreTest(t)
	folder := writeTree(t, snapPath)
	require.NoError(t, os.Chmod(folder, 0o555))
	t.Cleanup(func() {
		_ = os.Chmod(folder, 0o755)
		_ = os.Chmod(filepath.Join(datasetPath, "folder"), 0o755)
	})

	require.NoError(t, snap.RestoreRecursive(context.Background(), folder, nil))
	assert.FileExists(t, filepath.Join(datasetPath, "folder", "child", "f2.txt"))
	stat, err := os.Lstat(filepath.Join(datasetPath, "folder"))
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o555), stat.Mode().Perm())
}

// A cancelled restore stops between entries: the ones restored stay restored.
func TestSnapshot_RestoreRecursiveCancelled(t *testing.T) {
	snap, snapPath, datasetPath := newRestoreTest(t)
	folder := writeTree(t, snapPath)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	restored := 0
	err := snap.RestoreRecursive(ctx, folder, func(done int) {
		restored = done
		cancel()
	})
	assert.ErrorIs(t, err, context.Canceled)
	// child/f2.txt, then child, whose entries are all restored then
	assert.Equal(t, 2, restored)
	assert.FileExists(t, filepath.Join(datasetPath, "folder", "child", "f2.txt"))
	assert.NoFileExists(t, filepath.Join(datasetPath, "folder", "f1.txt"))

	_, err = CountRestoreEntries(ctx, folder)
	assert.ErrorIs(t, err, context.Canceled)
}

// A file whose copy is cancelled stays as it was, without the temporary file.
func TestSnapshot_RestoreFileCancelled(t *testing.T) {
	snap, snapPath, datasetPath := newRestoreTest(t)
	require.NoError(t, os.WriteFile(filepath.Join(snapPath, "file.txt"), []byte("snapshot"), 0o644))
	realFile := filepath.Join(datasetPath, "file.txt")
	require.NoError(t, os.WriteFile(realFile, []byte("now"), 0o644))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	assert.ErrorIs(t, snap.RestoreFile(ctx, filepath.Join(snapPath, "file.txt")), context.Canceled)
	content, err := os.ReadFile(realFile)
	require.NoError(t, err)
	assert.Equal(t, "now", string(content))
	entries, err := os.ReadDir(datasetPath)
	require.NoError(t, err)
	assert.Len(t, entries, 2, ".zfs and the file, no temporary file")
}

// fileInfoOwnedBy is a FileInfo with another owner, as of a file in a snapshot that belongs to someone else.
type fileInfoOwnedBy struct {
	os.FileInfo
	stat syscall.Stat_t
}

func (f fileInfoOwnedBy) Sys() any { return &f.stat }

// Regression: restoring a file that belongs to a group the user is not in failed with "operation not permitted" (only
// root may give files to such a group), and stopped the restore. The user keeps them, like "cp -p".
func TestSyncFileProperties_GroupTheUserIsNotIn(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root may give files to any group")
	}
	groups, err := os.Getgroups()
	require.NoError(t, err)
	foreignGid := 0
	for slices.Contains(groups, foreignGid) || foreignGid == os.Getgid() {
		foreignGid++
	}

	path := filepath.Join(t.TempDir(), "file")
	require.NoError(t, os.WriteFile(path, []byte("content"), 0o600))
	stat, err := os.Lstat(path)
	require.NoError(t, err)
	sys := *stat.Sys().(*syscall.Stat_t)
	sys.Gid = uint32(foreignGid)
	sys.Mtim = syscall.NsecToTimespec(time.Date(2020, 1, 2, 3, 4, 5, 0, time.UTC).UnixNano())
	snapshotVersion := fileInfoOwnedBy{FileInfo: stat, stat: sys}

	require.NoError(t, syncFileProperties(path, snapshotVersion))
	restored, err := os.Lstat(path)
	require.NoError(t, err)
	assert.Equal(t, uint32(os.Getgid()), restored.Sys().(*syscall.Stat_t).Gid, "the group of the user")
	assert.Equal(t, time.Date(2020, 1, 2, 3, 4, 5, 0, time.UTC), restored.ModTime().UTC(), "the times are restored")
}

// Regression: the mode was set before the owner, and chown clears the setgid bit, so restored files lost it.
func TestSyncFileProperties_KeepsSetgid(t *testing.T) {
	path := filepath.Join(t.TempDir(), "file")
	require.NoError(t, os.WriteFile(path, []byte("content"), 0o600))
	require.NoError(t, os.Chmod(path, 0o755|os.ModeSetgid))
	stat, err := os.Lstat(path)
	require.NoError(t, err)
	require.NoError(t, os.Chmod(path, 0o600))

	require.NoError(t, syncFileProperties(path, stat))
	restored, err := os.Lstat(path)
	require.NoError(t, err)
	assert.Equal(t, 0o755|os.ModeSetgid, restored.Mode())
}
