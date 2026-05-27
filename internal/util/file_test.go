package util

import (
	"os"
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
