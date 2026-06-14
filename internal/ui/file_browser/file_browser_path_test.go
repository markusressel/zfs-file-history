package file_browser

import (
	"os"
	"strings"
	"testing"
)

func TestTruncatePath(t *testing.T) {
	fb := &FileBrowserComponent{}
	sep := string(os.PathSeparator)

	tests := []struct {
		name     string
		path     string
		maxWidth int
		want     string
	}{
		{
			name:     "Short path, no truncation",
			path:     "/home/user",
			maxWidth: 20,
			want:     "/home/user",
		},
		{
			name:     "Long path, shorten components",
			path:     "/home/markus/projects/zfs-file-history",
			maxWidth: 25,
			want:     "...m…/p…/zfs-file-history",
		},
		{
			name:     "Very long path, shorten components and ellipsis",
			path:     "/home/markus/projects/zfs-file-history",
			maxWidth: 15,
			want:     "...file-history",
		},
		{
			name:     "Long path, shortening fits exactly",
			path:     "/home/markus/projects/zfs-file-history",
			maxWidth: 26,
			want:     "/h…/m…/p…/zfs-file-history",
		},
		{
			name:     "Path with empty components (leading slash)",
			path:     "/a/b/c/d/e/f/g/h",
			maxWidth: 10,
			want:     "...e/f/g/h",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// replace / with os.PathSeparator in test cases for cross-platform
			path := strings.ReplaceAll(tt.path, "/", sep)
			want := strings.ReplaceAll(tt.want, "/", sep)

			got := fb.truncatePath(path, tt.maxWidth)
			if got != want {
				t.Errorf("truncatePath() = %v, want %v", got, want)
			}
		})
	}
}
