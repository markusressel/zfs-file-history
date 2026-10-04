package data

import (
	"os"
	"testing"
	"zfs-file-history/internal/data/diff_state"
	"zfs-file-history/internal/zfs"

	"github.com/stretchr/testify/assert"
)

func TestFileBrowserEntry_HasReal(t *testing.T) {
	entry := &FileBrowserEntry{
		RealFile: &RealFile{Path: "/foo"},
	}
	assert.True(t, entry.HasReal())

	entry = &FileBrowserEntry{
		RealFile: nil,
	}
	assert.False(t, entry.HasReal())
}

func TestFileBrowserEntry_HasSnapshot(t *testing.T) {
	entry := &FileBrowserEntry{
		SnapshotFiles: []*SnapshotFile{{Path: "/foo"}},
	}
	assert.True(t, entry.HasSnapshot())

	entry = &FileBrowserEntry{
		SnapshotFiles: []*SnapshotFile{},
	}
	assert.False(t, entry.HasSnapshot())
}

func TestFileBrowserEntry_GetRealPath(t *testing.T) {
	realFile := &RealFile{Path: "/real/path"}
	snapshotFile := &SnapshotFile{OriginalPath: "/original/path"}

	entry := &FileBrowserEntry{
		RealFile: realFile,
	}
	assert.Equal(t, "/real/path", entry.GetRealPath())

	entry = &FileBrowserEntry{
		RealFile:      nil,
		SnapshotFiles: []*SnapshotFile{snapshotFile},
	}
	assert.Equal(t, "/original/path", entry.GetRealPath())
}

func TestFileBrowserEntry_Equal(t *testing.T) {
	e1 := FileBrowserEntry{RealFile: &RealFile{Path: "/foo"}}
	e2 := FileBrowserEntry{RealFile: &RealFile{Path: "/foo"}}
	e3 := FileBrowserEntry{RealFile: &RealFile{Path: "/bar"}}

	assert.True(t, e1.Equal(e2))
	assert.False(t, e1.Equal(e3))
}

func TestFileBrowserEntry_HasDiff(t *testing.T) {
	entry := &FileBrowserEntry{
		DiffState:     diff_state.Modified,
		SnapshotFiles: []*SnapshotFile{{Path: "/foo"}},
	}
	assert.True(t, entry.HasDiff())

	entry.DiffState = diff_state.Equal
	assert.False(t, entry.HasDiff())

	entry.DiffState = diff_state.Modified
	entry.SnapshotFiles = nil
	assert.False(t, entry.HasDiff())
}

func TestFileBrowserEntry_TableRowId(t *testing.T) {
	entry := FileBrowserEntry{RealFile: &RealFile{Path: "/foo"}}
	assert.Equal(t, "/foo", entry.TableRowId())
}

func TestRealFile_Equal(t *testing.T) {
	f1 := RealFile{Name: "a", Path: "p"}
	f2 := RealFile{Name: "a", Path: "p"}
	f3 := RealFile{Name: "b", Path: "p"}

	assert.True(t, f1.Equal(f2))
	assert.False(t, f1.Equal(f3))
}

func TestSnapshotFile_Equal(t *testing.T) {
	snap := &zfs.Snapshot{}
	f1 := SnapshotFile{Path: "p", OriginalPath: "o", Snapshot: snap}
	f2 := SnapshotFile{Path: "p", OriginalPath: "o", Snapshot: snap}
	f3 := SnapshotFile{Path: "p2", OriginalPath: "o", Snapshot: snap}

	assert.True(t, f1.Equal(f2))
	assert.False(t, f1.Equal(f3))
}

func TestFileBrowserEntry_GetStatAndNew(t *testing.T) {
	tmpDir := t.TempDir()
	fileStat, err := os.Stat(tmpDir)
	assert.NoError(t, err)

	entry := NewFileBrowserEntry("tmp", &RealFile{Path: tmpDir, Stat: fileStat}, nil, Directory)
	assert.Equal(t, "tmp", entry.Name)
	assert.Equal(t, Directory, entry.Type)
	assert.Equal(t, fileStat, entry.GetStat())

	canEnter, err := entry.CanEnter()
	assert.NoError(t, err)
	assert.True(t, canEnter)

	// Non-existent path
	badEntry := NewFileBrowserEntry("bad", &RealFile{Path: "/nonexistent/path/xyz"}, nil, File)
	canEnter, err = badEntry.CanEnter()
	assert.Error(t, err)
	assert.False(t, canEnter)

	// Snapshot-only stat
	snapEntry := NewFileBrowserEntry("snap", nil, []*SnapshotFile{{OriginalPath: tmpDir, Stat: fileStat}}, File)
	assert.Equal(t, fileStat, snapEntry.GetStat())
}

func TestSnapshotBrowserEntry_TableRowId(t *testing.T) {
	snap := &zfs.Snapshot{Path: "pool/ds@snap1"}
	entry := SnapshotBrowserEntry{Snapshot: snap}
	assert.Equal(t, "pool/ds@snap1", entry.TableRowId())
}
