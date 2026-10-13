package cmd

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSudoersRules(t *testing.T) {
	events := sudoersRules("alice", false)
	assert.Contains(t, events, "zpool events -v -f")
	assert.Contains(t, events, "zpool status -p -v")
	assert.NotContains(t, events, "scrub")

	withActions := sudoersRules("alice", true)
	assert.True(t, strings.HasPrefix(withActions, events))
	assert.Contains(t, withActions, "zpool scrub [a-zA-Z]*")
}

func TestInstallSudoersFile(t *testing.T) {
	if _, err := exec.LookPath("visudo"); err != nil {
		t.Skip("visudo is not installed")
	}
	path := filepath.Join(t.TempDir(), "zfs-file-history")

	require.NoError(t, installSudoersFile(path, sudoersRules("alice", true)))
	content, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, sudoersRules("alice", true), string(content))
	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0440), info.Mode().Perm())

	// invalid rules are not installed, the old file is kept
	assert.Error(t, installSudoersFile(path, "alice ALL=(root) NOPASSWD: /usr/bin/zpool scrub,\n"))
	content, err = os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, sudoersRules("alice", true), string(content))
	entries, err := os.ReadDir(filepath.Dir(path))
	require.NoError(t, err)
	assert.Len(t, entries, 1, "no temporary file left")
}
