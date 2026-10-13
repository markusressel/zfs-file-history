package ui

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExistingFolderOf(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "kept"), 0o755))

	tests := []struct {
		path   string
		folder string
		name   string
	}{
		{filepath.Join(root, "kept", "file"), filepath.Join(root, "kept"), "file"},
		// Regression: a file only in snapshots, in a folder deleted as well, opened a folder that does not exist
		{filepath.Join(root, "deleted", "sub", "file"), root, "deleted"},
		{"/file", "/", "file"},
	}
	for _, test := range tests {
		folder, name := existingFolderOf(test.path)
		assert.Equal(t, test.folder, folder, test.path)
		assert.Equal(t, test.name, name, test.path)
	}
}
