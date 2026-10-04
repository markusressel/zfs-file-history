package zfs

import (
	"os"
	"path/filepath"
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

	err := snap.RestoreFile(srcFile)
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

	err = snap.RestoreRecursive(nestedDir)
	require.NoError(t, err)

	assert.FileExists(t, filepath.Join(datasetPath, "nested", "f1.txt"))
	assert.FileExists(t, filepath.Join(datasetPath, "nested", "child", "f2.txt"))

	// 4. Restore generic function on file and dir
	err = snap.Restore(srcFile)
	require.NoError(t, err)
	err = snap.Restore(nestedDir)
	require.NoError(t, err)
}
