package util

import (
	"os"
	"syscall"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestUnixPermissions(t *testing.T) {
	t.Parallel()

	mode := os.FileMode(0o755)
	if got, want := UnixPermissions(mode), uint32(0o755); got != want {
		t.Fatalf("UnixPermissions() = %04o, want %04o", got, want)
	}

	mode = os.FileMode(0o755) | os.ModeSetuid
	if got, want := UnixPermissions(mode), uint32(0o4755); got != want {
		t.Fatalf("UnixPermissions() with setuid = %04o, want %04o", got, want)
	}
}

func TestUnixPerm(t *testing.T) {
	t.Parallel()

	mode := os.FileMode(0o755)
	assert.Equal(t, uint32(0o755), UnixPerm(mode))

	mode = os.FileMode(0o755) | os.ModeSetuid
	assert.Equal(t, uint32(0o4755), UnixPerm(mode))

	mode = os.FileMode(0o755) | os.ModeSetgid
	assert.Equal(t, uint32(0o2755), UnixPerm(mode))

	mode = os.FileMode(0o755) | os.ModeSticky
	assert.Equal(t, uint32(0o1755), UnixPerm(mode))
}

func TestFileExists(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	tmpFile, _ := os.CreateTemp(tmpDir, "exists-test")
	defer tmpFile.Close()

	assert.True(t, FileExists(tmpDir))
	assert.True(t, FileExists(tmpFile.Name()))
	assert.False(t, FileExists("/non/existent/path"))
}

func TestListFilesIn(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	f1, _ := os.CreateTemp(tmpDir, "file1")
	f2, _ := os.CreateTemp(tmpDir, "file2")
	defer f1.Close()
	defer f2.Close()

	files, err := ListFilesIn(tmpDir)
	assert.NoError(t, err)
	assert.ElementsMatch(t, []string{f1.Name(), f2.Name()}, files)

	// Test non-existent directory
	files, err = ListFilesIn("/non/existent/dir")
	assert.NoError(t, err)
	assert.Empty(t, files)
}

func TestUnixPermSymbolic(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		mode os.FileMode
		want string
	}{
		{name: "regular", mode: 0o755, want: "-rwxr-xr-x"},
		{name: "directory", mode: os.ModeDir | 0o755, want: "drwxr-xr-x"},
		{name: "symlink", mode: os.ModeSymlink | 0o777, want: "lrwxrwxrwx"},
		{name: "sticky without execute", mode: 0o766 | os.ModeSticky, want: "-rwxrw-rwT"},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := UnixPermSymbolic(tt.mode); got != tt.want {
				t.Fatalf("UnixPermSymbolic() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestUnixOwnerIDs(t *testing.T) {
	t.Parallel()

	tmpFile, err := os.CreateTemp(t.TempDir(), "zfh-owner-test")
	if err != nil {
		t.Fatalf("failed creating temp file: %v", err)
	}
	_ = tmpFile.Close()

	stat, err := os.Lstat(tmpFile.Name())
	if err != nil {
		t.Fatalf("failed to stat temp file: %v", err)
	}

	uid, gid, ok := UnixOwnerIDs(stat)
	if !ok {
		t.Fatalf("UnixOwnerIDs() should return ok=true for local file")
	}

	statT, castOk := stat.Sys().(*syscall.Stat_t)
	if !castOk || statT == nil {
		t.Fatalf("unexpected stat type for local file")
	}

	if uid != statT.Uid || gid != statT.Gid {
		t.Fatalf("UnixOwnerIDs() = (%d,%d), want (%d,%d)", uid, gid, statT.Uid, statT.Gid)
	}
}

func TestLookupCurrentUserAndGroupName(t *testing.T) {
	t.Parallel()

	stat, err := os.Lstat(".")
	if err != nil {
		t.Fatalf("failed to stat current directory: %v", err)
	}

	uid, gid, ok := UnixOwnerIDs(stat)
	if !ok {
		t.Fatalf("UnixOwnerIDs() should return ok=true for current directory")
	}

	username, err := LookupUserName(uid)
	if err != nil {
		t.Fatalf("LookupUserName(%d) failed: %v", uid, err)
	}
	if username == "" {
		t.Fatalf("LookupUserName(%d) returned empty username", uid)
	}

	groupName, err := LookupGroupName(gid)
	if err != nil {
		t.Fatalf("LookupGroupName(%d) failed: %v", gid, err)
	}
	if groupName == "" {
		t.Fatalf("LookupGroupName(%d) returned empty group name", gid)
	}
}

func TestUnixPermSymbolic_SpecialFiles(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		mode os.FileMode
		want string
	}{
		{name: "named pipe", mode: os.ModeNamedPipe | 0o600, want: "prw-------"},
		{name: "socket", mode: os.ModeSocket | 0o666, want: "srw-rw-rw-"},
		{name: "char device", mode: os.ModeDevice | os.ModeCharDevice | 0o600, want: "crw-------"},
		{name: "block device", mode: os.ModeDevice | 0o600, want: "brw-------"},
		{name: "setuid with execute", mode: os.ModeSetuid | 0o700, want: "-rws------"},
		{name: "setuid without execute", mode: os.ModeSetuid | 0o600, want: "-rwS------"},
		{name: "setgid with execute", mode: os.ModeSetgid | 0o070, want: "----rws---"},
		{name: "setgid without execute", mode: os.ModeSetgid | 0o060, want: "----rwS---"},
		{name: "sticky with execute", mode: os.ModeSticky | 0o007, want: "-------rwt"},
		{name: "sticky without execute", mode: os.ModeSticky | 0o006, want: "-------rwT"},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := UnixPermSymbolic(tt.mode); got != tt.want {
				t.Fatalf("UnixPermSymbolic() = %q, want %q", got, tt.want)
			}
		})
	}
}

type dummyFileInfo struct {
	os.FileInfo
	sys interface{}
}

func (d *dummyFileInfo) Sys() interface{} {
	return d.sys
}

func TestUnixOwnerIDs_EdgeCases(t *testing.T) {
	t.Parallel()

	// stat is nil
	uid, gid, ok := UnixOwnerIDs(nil)
	assert.False(t, ok)
	assert.Zero(t, uid)
	assert.Zero(t, gid)

	// stat.Sys() is nil
	uid, gid, ok = UnixOwnerIDs(&dummyFileInfo{sys: nil})
	assert.False(t, ok)
	assert.Zero(t, uid)
	assert.Zero(t, gid)

	// stat.Sys() is wrong type
	uid, gid, ok = UnixOwnerIDs(&dummyFileInfo{sys: "not-stat_t"})
	assert.False(t, ok)
	assert.Zero(t, uid)
	assert.Zero(t, gid)
}

func TestLookupUserName_Error(t *testing.T) {
	t.Parallel()
	// Lookup a highly unlikely UID
	username, err := LookupUserName(999999)
	assert.Error(t, err)
	assert.Empty(t, username)
}

func TestLookupGroupName_Error(t *testing.T) {
	t.Parallel()
	// Lookup a highly unlikely GID
	groupName, err := LookupGroupName(999999)
	assert.Error(t, err)
	assert.Empty(t, groupName)
}
