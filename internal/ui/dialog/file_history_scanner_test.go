package dialog

import (
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
