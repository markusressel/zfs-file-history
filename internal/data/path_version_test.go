package data

import (
	"io/fs"
	"os"
	"testing"
	"time"
	"zfs-file-history/internal/zfs"

	"github.com/stretchr/testify/assert"
)

type fileInfo struct {
	size    int64
	modTime time.Time
}

func (f fileInfo) Name() string       { return "x" }
func (f fileInfo) Size() int64        { return f.size }
func (f fileInfo) Mode() fs.FileMode  { return 0o644 }
func (f fileInfo) ModTime() time.Time { return f.modTime }
func (f fileInfo) IsDir() bool        { return false }
func (f fileInfo) Sys() any           { return nil }

func TestNewVersions(t *testing.T) {
	day := func(n int) time.Time { return time.Date(2026, 10, n, 0, 0, 0, 0, time.UTC) }
	version := func(name string, created int, info os.FileInfo) PathVersion {
		return PathVersion{Snapshot: &zfs.Snapshot{Name: name, Properties: zfs.SnapshotProperties{CreationDate: day(created)}}, Info: info}
	}
	v1, v2 := fileInfo{size: 1, modTime: day(1)}, fileInfo{size: 2, modTime: day(3)}
	// in any order: s0 without the file, created in s1, the same in s2, changed in s3, the same in s4, deleted in s5
	versions := []PathVersion{
		version("s5", 6, nil), version("s3", 4, v2), version("s1", 2, v1),
		version("s0", 1, nil), version("s4", 5, v2), version("s2", 3, v1),
	}
	assert.Equal(t, map[string]bool{"s0": false, "s1": true, "s2": false, "s3": true, "s4": false, "s5": true}, NewVersions(versions))

	// the oldest snapshot contains the file: its first version
	assert.Equal(t, map[string]bool{"s1": true, "s2": false}, NewVersions([]PathVersion{version("s2", 3, v1), version("s1", 2, v1)}))
}
