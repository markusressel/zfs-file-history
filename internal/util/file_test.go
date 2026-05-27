package util

import (
	"os"
	"syscall"
	"testing"
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
