package dialog

import (
	"os"
	"path/filepath"
	"testing"
	"time"
	"zfs-file-history/internal/data/diff_state"
	"zfs-file-history/internal/zfs"

	"github.com/stretchr/testify/assert"
)

func TestHistoryScanner_DetermineDiffStateBetween(t *testing.T) {
	filePath := "/pool/ds1/file.txt"
	snap1 := &zfs.Snapshot{
		Name: "snap-1",
		ParentDataset: &zfs.Dataset{
			Path:          "/pool/ds1",
			HiddenZfsPath: "/pool/ds1/.zfs",
		},
	}
	snap2 := &zfs.Snapshot{
		Name: "snap-2",
		ParentDataset: &zfs.Dataset{
			Path:          "/pool/ds1",
			HiddenZfsPath: "/pool/ds1/.zfs",
		},
	}

	snap1Path := snap1.GetSnapshotPath(filePath)
	snap2Path := snap2.GetSnapshotPath(filePath)

	now := time.Now()

	tests := []struct {
		name      string
		snap1Meta fileMeta
		snap2Meta fileMeta
		expected  diff_state.DiffState
	}{
		{
			name: "Both files exist and are equal",
			snap1Meta: fileMeta{
				exists:  true,
				isDir:   false,
				size:    100,
				mode:    0644,
				modTime: now,
			},
			snap2Meta: fileMeta{
				exists:  true,
				isDir:   false,
				size:    100,
				mode:    0644,
				modTime: now,
			},
			expected: diff_state.Equal,
		},
		{
			name: "Second snapshot file was modified",
			snap1Meta: fileMeta{
				exists:  true,
				isDir:   false,
				size:    100,
				mode:    0644,
				modTime: now,
			},
			snap2Meta: fileMeta{
				exists:  true,
				isDir:   false,
				size:    200, // modified size
				mode:    0644,
				modTime: now,
			},
			expected: diff_state.Modified,
		},
		{
			name: "Second snapshot file was added (first snapshot file did not exist)",
			snap1Meta: fileMeta{
				exists: false,
			},
			snap2Meta: fileMeta{
				exists:  true,
				isDir:   false,
				size:    100,
				mode:    0644,
				modTime: now,
			},
			expected: diff_state.Added,
		},
		{
			name: "Second snapshot file was deleted (first snapshot file existed)",
			snap1Meta: fileMeta{
				exists:  true,
				isDir:   false,
				size:    100,
				mode:    0644,
				modTime: now,
			},
			snap2Meta: fileMeta{
				exists: false,
			},
			expected: diff_state.Deleted,
		},
		{
			name: "Neither file exists",
			snap1Meta: fileMeta{
				exists: false,
			},
			snap2Meta: fileMeta{
				exists: false,
			},
			expected: diff_state.Equal,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &historyScanner{
				filePath:  filePath,
				metaCache: make(map[string]fileMeta),
			}
			s.metaCache[snap1Path] = tt.snap1Meta
			s.metaCache[snap2Path] = tt.snap2Meta

			// Test state between snap2 and snap1 (snap2 is the current snapshot, snap1 is the previous snapshot)
			state, err := s.determineDiffStateBetween(snap2, snap1)
			assert.NoError(t, err)
			assert.Equal(t, tt.expected, state)
		})
	}
}

func TestHistoryScanner_DetermineDiffStateBetween_PrevNil(t *testing.T) {
	filePath := "/pool/ds1/file.txt"
	snap := &zfs.Snapshot{
		Name: "snap-1",
		ParentDataset: &zfs.Dataset{
			Path:          "/pool/ds1",
			HiddenZfsPath: "/pool/ds1/.zfs",
		},
	}
	snapPath := snap.GetSnapshotPath(filePath)

	s := &historyScanner{
		filePath:  filePath,
		metaCache: make(map[string]fileMeta),
	}

	// Case 1: prev is nil, snap exists -> Added
	s.metaCache[snapPath] = fileMeta{exists: true}
	state, err := s.determineDiffStateBetween(snap, nil)
	assert.NoError(t, err)
	assert.Equal(t, diff_state.Added, state)

	// Case 2: prev is nil, snap does not exist -> Equal
	s.metaCache[snapPath] = fileMeta{exists: false}
	state, err = s.determineDiffStateBetween(snap, nil)
	assert.NoError(t, err)
	assert.Equal(t, diff_state.Equal, state)
}

type mockFileInfo struct {
	isDir   bool
	size    int64
	mode    os.FileMode
	modTime time.Time
}

func (m mockFileInfo) Name() string       { return "file.txt" }
func (m mockFileInfo) Size() int64        { return m.size }
func (m mockFileInfo) Mode() os.FileMode  { return m.mode }
func (m mockFileInfo) ModTime() time.Time { return m.modTime }
func (m mockFileInfo) IsDir() bool        { return m.isDir }
func (m mockFileInfo) Sys() any           { return nil }

func TestHistoryScanner_DetermineDiffStateAgainstWorkingCopy(t *testing.T) {
	filePath := "/pool/ds1/file.txt"
	snap := &zfs.Snapshot{
		Name: "snap-1",
		ParentDataset: &zfs.Dataset{
			Path:          "/pool/ds1",
			HiddenZfsPath: "/pool/ds1/.zfs",
		},
	}
	snapPath := snap.GetSnapshotPath(filePath)

	now := time.Now()

	s := &historyScanner{
		filePath:          filePath,
		metaCache:         make(map[string]fileMeta),
		workingCopyExists: true,
		workingCopyStat: mockFileInfo{
			isDir:   false,
			size:    100,
			mode:    0644,
			modTime: now,
		},
	}

	// Case 1: Snapshot exists and is equal to working copy
	s.metaCache[snapPath] = fileMeta{
		exists:  true,
		isDir:   false,
		size:    100,
		mode:    0644,
		modTime: now,
	}
	state, err := s.determineDiffStateAgainstWorkingCopy(snap)
	assert.NoError(t, err)
	assert.Equal(t, diff_state.Equal, state)

	// Case 2: Snapshot exists but differs from working copy
	s.metaCache[snapPath] = fileMeta{
		exists:  true,
		isDir:   false,
		size:    200, // differs
		mode:    0644,
		modTime: now,
	}
	state, err = s.determineDiffStateAgainstWorkingCopy(snap)
	assert.NoError(t, err)
	assert.Equal(t, diff_state.Modified, state)

	// Case 3: Snapshot exists, working copy does not exist
	s.workingCopyExists = false
	s.metaCache[snapPath] = fileMeta{exists: true}
	state, err = s.determineDiffStateAgainstWorkingCopy(snap)
	assert.NoError(t, err)
	assert.Equal(t, diff_state.Deleted, state) // File only exists in snapshot, deleted in working copy

	// Case 4: Snapshot does not exist, working copy exists
	s.workingCopyExists = true
	s.metaCache[snapPath] = fileMeta{exists: false}
	state, err = s.determineDiffStateAgainstWorkingCopy(snap)
	assert.NoError(t, err)
	assert.Equal(t, diff_state.Added, state) // File only exists in working copy, added in working copy

	// Case 5: Neither exists
	s.workingCopyExists = false
	s.metaCache[snapPath] = fileMeta{exists: false}
	state, err = s.determineDiffStateAgainstWorkingCopy(snap)
	assert.NoError(t, err)
	assert.Equal(t, diff_state.Equal, state)
}

func TestComputeHistoryDiffText(t *testing.T) {
	tempDir := t.TempDir()
	fileA := filepath.Join(tempDir, "fileA.txt")
	err := os.WriteFile(fileA, []byte("Hello\nWorld\n"), 0644)
	assert.NoError(t, err)

	// Case 1: Binary
	res := computeHistoryDiffText(fileA, fileA, diffModeWorkingCopy, nil, true)
	assert.Equal(t, "Binary files differ, content preview not available.", res)

	// Case 2: Directory comparison
	res = computeHistoryDiffText(tempDir, fileA, diffModeWorkingCopy, nil, false)
	assert.Equal(t, "Directory content comparison not available.", res)

	// Case 3: Both missing
	res = computeHistoryDiffText(filepath.Join(tempDir, "nonexistent1"), filepath.Join(tempDir, "nonexistent2"), diffModeWorkingCopy, nil, false)
	assert.Equal(t, "", res)

	// Case 4: Working copy exists, snapshot missing (vs working copy)
	missingSnapPath := filepath.Join(tempDir, "missing_snap.txt")
	res = computeHistoryDiffText(fileA, missingSnapPath, diffModeWorkingCopy, nil, false)
	assert.Contains(t, res, "-Hello")
	assert.Contains(t, res, "-World")

	// Case 5: Working copy missing, snapshot exists (vs working copy)
	missingWcPath := filepath.Join(tempDir, "missing_wc.txt")
	res = computeHistoryDiffText(missingWcPath, fileA, diffModeWorkingCopy, nil, false)
	assert.Contains(t, res, "+Hello")
	assert.Contains(t, res, "+World")
}
