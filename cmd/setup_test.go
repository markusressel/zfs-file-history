package cmd

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"zfs-file-history/internal/disk"
	"zfs-file-history/internal/zfs"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAskForRules(t *testing.T) {
	original := disk.SmartctlInstalled
	t.Cleanup(func() { disk.SmartctlInstalled = original })
	disk.SmartctlInstalled = func() bool { return true }
	file := zfs.ParseSudoersFile(zfs.SudoersRuleEvents.Lines("alice"))
	var out strings.Builder

	// data errors, history and SMART: Enter (allowed by default, they only read), pool actions: Enter (not allowed by
	// default)
	rules := askForRules(strings.NewReader("\n\n\n\n"), &out, file, "alice")

	assert.Equal(t, []*zfs.SudoersRule{zfs.SudoersRuleDataErrors, zfs.SudoersRuleHistory, zfs.SudoersRuleSmart}, rules)
	assert.Contains(t, out.String(), "Live updates: already allowed")
	assert.Contains(t, out.String(), "Allow for alice? [Y/n]")
	assert.Contains(t, out.String(), "Allow for alice? [y/N]")

	rules = askForRules(strings.NewReader("n\nn\nn\nyes\n"), &out, file, "alice")
	assert.Equal(t, []*zfs.SudoersRule{zfs.SudoersRulePoolActions}, rules)

	// without smartctl, its rule is not asked for
	disk.SmartctlInstalled = func() bool { return false }
	out.Reset()
	rules = askForRules(strings.NewReader("n\nn\nn\n"), &out, file, "alice")
	assert.Empty(t, rules)
	assert.Contains(t, out.String(), "Disk health: not installed")

	// closed input: nothing is allowed without an answer
	rules = askForRules(strings.NewReader(""), &out, file, "alice")
	assert.Empty(t, rules)
}

func TestAddRules(t *testing.T) {
	file := zfs.ParseSudoersFile(zfs.SudoersRuleEvents.Lines("alice"))

	added := addRules(file, "alice", []*zfs.SudoersRule{zfs.SudoersRuleEvents, zfs.SudoersRulePoolActions})

	assert.Equal(t, []string{"Pool actions"}, added)
	assert.True(t, file.Has("alice", zfs.SudoersRuleEvents), "kept")
	assert.True(t, file.Has("alice", zfs.SudoersRulePoolActions))
	assert.Empty(t, addRules(file, "alice", []*zfs.SudoersRule{zfs.SudoersRulePoolActions}))
}

func TestWrap(t *testing.T) {
	assert.Equal(t, "one two\nthree", wrap("one two three", 8))
	assert.Equal(t, "  one\n  two", indent("one\ntwo", "  "))
}

// allRules is the content of a sudoers file with all rules for alice.
func allRules() string {
	file := zfs.ParseSudoersFile("")
	addRules(file, "alice", zfs.SudoersRules)
	return file.String()
}

func TestInstallSudoersFile(t *testing.T) {
	if _, err := exec.LookPath("visudo"); err != nil {
		t.Skip("visudo is not installed")
	}
	path := filepath.Join(t.TempDir(), "zfs-file-history")

	require.NoError(t, installSudoersFile(path, allRules()))
	content, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, allRules(), string(content))
	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0440), info.Mode().Perm())

	// invalid rules are not installed, the old file is kept
	assert.Error(t, installSudoersFile(path, "alice ALL=(root) NOPASSWD: /usr/bin/zpool scrub,\n"))
	content, err = os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, allRules(), string(content))
	entries, err := os.ReadDir(filepath.Dir(path))
	require.NoError(t, err)
	assert.Len(t, entries, 1, "no temporary file left")
}
