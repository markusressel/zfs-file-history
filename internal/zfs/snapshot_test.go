package zfs

import (
	"os"
	"path/filepath"
	"testing"
	"time"
	"zfs-file-history/internal/data/diff_state"
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
