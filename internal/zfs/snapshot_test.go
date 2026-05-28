package zfs

import (
	"os"
	"path/filepath"
	"testing"
	"time"
	"zfs-file-history/internal/data/diff_state"
)

func TestDetermineDiffState(t *testing.T) {
	tempDir := t.TempDir()

	// Setup mock dataset and snapshot structure
	datasetPath := filepath.Join(tempDir, "dataset")
	err := os.MkdirAll(datasetPath, 0755)
	if err != nil {
		t.Fatalf("failed to create dataset path: %v", err)
	}

	snapDirBase := filepath.Join(datasetPath, ".zfs", "snapshot")
	snapName := "snap1"
	snapPath := filepath.Join(snapDirBase, snapName)
	err = os.MkdirAll(snapPath, 0755)
	if err != nil {
		t.Fatalf("failed to create snapshot path: %v", err)
	}

	dataset := &Dataset{
		Path:          datasetPath,
		HiddenZfsPath: filepath.Join(datasetPath, ".zfs"),
	}

	snapshot := &Snapshot{
		Name:          snapName,
		Path:          snapPath,
		ParentDataset: dataset,
	}

	t.Run("Added", func(t *testing.T) {
		// File exists in real but NOT in snapshot
		filePath := filepath.Join(datasetPath, "newfile.txt")
		err := os.WriteFile(filePath, []byte("content"), 0644)
		if err != nil {
			t.Fatalf("failed to create file: %v", err)
		}
		defer os.Remove(filePath)

		state := snapshot.DetermineDiffState(filePath)
		if state != diff_state.Added {
			t.Errorf("expected Added, got %v", state)
		}
	})

	t.Run("Equal", func(t *testing.T) {
		// File exists in both and is identical
		fileName := "equal.txt"
		realFilePath := filepath.Join(datasetPath, fileName)
		snapFilePath := filepath.Join(snapPath, fileName)

		content := []byte("identical content")
		now := time.Now().Truncate(time.Second) // filesystem might truncate

		err := os.WriteFile(realFilePath, content, 0644)
		if err != nil {
			t.Fatalf("failed to create real file: %v", err)
		}
		defer os.Remove(realFilePath)

		err = os.WriteFile(snapFilePath, content, 0644)
		if err != nil {
			t.Fatalf("failed to create snap file: %v", err)
		}
		defer os.Remove(snapFilePath)

		// Ensure same mod time
		err = os.Chtimes(realFilePath, now, now)
		if err != nil {
			t.Fatalf("failed to set times: %v", err)
		}
		err = os.Chtimes(snapFilePath, now, now)
		if err != nil {
			t.Fatalf("failed to set times: %v", err)
		}

		state := snapshot.DetermineDiffState(realFilePath)
		if state != diff_state.Equal {
			t.Errorf("expected Equal, got %v", state)
		}
	})

	t.Run("Modified", func(t *testing.T) {
		// File exists in both but is different
		fileName := "modified.txt"
		realFilePath := filepath.Join(datasetPath, fileName)
		snapFilePath := filepath.Join(snapPath, fileName)

		err := os.WriteFile(realFilePath, []byte("new content"), 0644)
		if err != nil {
			t.Fatalf("failed to create real file: %v", err)
		}
		defer os.Remove(realFilePath)

		err = os.WriteFile(snapFilePath, []byte("old content"), 0644)
		if err != nil {
			t.Fatalf("failed to create snap file: %v", err)
		}
		defer os.Remove(snapFilePath)

		// Set different mod times to ensure IsRealFileDifferent returns true
		now := time.Now().Truncate(time.Second)
		err = os.Chtimes(realFilePath, now, now)
		if err != nil {
			t.Fatalf("failed to set real times: %v", err)
		}
		err = os.Chtimes(snapFilePath, now.Add(-time.Hour), now.Add(-time.Hour))
		if err != nil {
			t.Fatalf("failed to set snap times: %v", err)
		}

		state := snapshot.DetermineDiffState(realFilePath)
		if state != diff_state.Modified {
			t.Errorf("expected Modified, got %v", state)
		}
	})
}
