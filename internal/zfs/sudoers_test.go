package zfs

import (
	"errors"
	"slices"
	"strings"
	"testing"
	"zfs-file-history/internal/disk"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSudoersRuleLines(t *testing.T) {
	assert.Equal(t, "alice ALL=(root) NOPASSWD: /usr/sbin/zpool events -v -f, /sbin/zpool events -v -f, /usr/bin/zpool events -v -f\n",
		SudoersRuleEvents.Lines("alice"))
	assert.Equal(t, "alice ALL=(root) NOPASSWD: /usr/sbin/zpool status -p -v, /sbin/zpool status -p -v, /usr/bin/zpool status -p -v\n",
		SudoersRuleDataErrors.Lines("alice"))

	rules := strings.Split(strings.TrimSpace(SudoersRulePoolActions.Lines("alice")), "\n")
	require.Len(t, rules, 2)
	for _, path := range zpoolPaths {
		for _, action := range []string{"scrub", "scrub -p", "scrub -s", "trim", "trim -c", "clear"} {
			assert.Contains(t, rules[0], " "+path+" "+action+" [a-zA-Z]*")
		}
		// the last rule wins: no further arguments for clear (e.g. -F)
		assert.Contains(t, rules[1], "!"+path+" clear * *")
	}
	assert.True(t, strings.HasPrefix(rules[0], "alice ALL=(root) NOPASSWD: "))
	assert.True(t, strings.HasPrefix(rules[1], "alice ALL=(root) !"))
}

func TestSudoersRuleById(t *testing.T) {
	for _, rule := range SudoersRules {
		assert.Same(t, rule, SudoersRuleById(rule.Id))
	}
	assert.Nil(t, SudoersRuleById("unknown"))
}

func TestParseSudoersFile(t *testing.T) {
	tests := []struct {
		name    string
		content string
		granted []*SudoersRule
	}{
		{"empty", "", nil},
		{"version 2.1: only the events", "# installed by zfs-file-history setup\n" +
			"alice ALL=(root) NOPASSWD: /usr/sbin/zpool events -v -f, /sbin/zpool events -v -f, /usr/bin/zpool events -v -f\n",
			[]*SudoersRule{SudoersRuleEvents}},
		{"version 2.2: events and damaged files in one line, pool actions", "# installed by zfs-file-history setup\n" +
			"alice ALL=(root) NOPASSWD: /usr/sbin/zpool events -v -f, /usr/sbin/zpool status -p -v, /sbin/zpool events -v -f\n" +
			SudoersRulePoolActions.Lines("alice"),
			[]*SudoersRule{SudoersRuleEvents, SudoersRuleDataErrors, SudoersRulePoolActions}},
		{"only the denied clear", "alice ALL=(root) !/usr/bin/zpool clear * *\n", []*SudoersRule{SudoersRulePoolActions}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			file := ParseSudoersFile(test.content)
			for _, rule := range SudoersRules {
				assert.Equal(t, slices.Contains(test.granted, rule), file.Has("alice", rule), rule.Id)
				assert.False(t, file.Has("bob", rule), rule.Id)
			}
		})
	}
}

func TestSudoersFile_Grant(t *testing.T) {
	file := ParseSudoersFile("# installed by zfs-file-history setup\n" +
		"alice ALL=(root) NOPASSWD: /usr/sbin/zpool events -v -f, /sbin/zpool events -v -f, /usr/bin/zpool events -v -f\n" +
		"Defaults:alice !lecture\n")

	file.Grant("alice", SudoersRuleDataErrors)
	file.Grant("bob", SudoersRulePoolActions)

	assert.Equal(t, sudoersFileHeader+
		"# Live updates\n"+SudoersRuleEvents.Lines("alice")+
		"# Damaged files\n"+SudoersRuleDataErrors.Lines("alice")+
		"# Pool actions\n"+SudoersRulePoolActions.Lines("bob")+
		"Defaults:alice !lecture\n", file.String())

	// written and read again: the same
	again := ParseSudoersFile(file.String())
	assert.Equal(t, file.String(), again.String())
	assert.True(t, again.Has("alice", SudoersRuleEvents))
	assert.True(t, again.Has("alice", SudoersRuleDataErrors))
	assert.False(t, again.Has("alice", SudoersRulePoolActions))
	assert.True(t, again.Has("bob", SudoersRulePoolActions))
}

// sudoList is the output of "sudo -l" on a system with the rules for the events of version 2.1 installed.
const sudoList = `Matching Defaults entries for alice on host:
    editor=/usr/bin/vim, env_reset, timestamp_timeout=-1

User alice may run the following commands on host:
    (ALL) ALL
    (root) NOPASSWD: /usr/bin/systemctl * wg-quick@wg0.service
    (root) NOPASSWD: /usr/sbin/zpool events -v -f, /sbin/zpool events -v -f,
        /usr/bin/zpool events -v -f
    (root) SETENV: NOPASSWD: /usr/bin/zpool status -p -v, PASSWD: /usr/bin/zpool scrub [a-zA-Z]*
    (bob) NOPASSWD: /usr/bin/zpool trim [a-zA-Z]*
    (root) !/usr/bin/zpool clear * *
`

func TestParseNoPasswordCommands(t *testing.T) {
	commands, all := parseNoPasswordCommands(sudoList)

	assert.False(t, all)
	assert.Equal(t, map[string]bool{
		"/usr/bin/systemctl * wg-quick@wg0.service": true,
		"/usr/sbin/zpool events -v -f":              true,
		"/sbin/zpool events -v -f":                  true,
		"/usr/bin/zpool events -v -f":               true,
		"/usr/bin/zpool status -p -v":               true,
	}, commands)

	_, all = parseNoPasswordCommands("User alice may run the following commands on host:\n    (ALL : ALL) NOPASSWD: ALL\n")
	assert.True(t, all)
}

func TestAllowedSudoersRules(t *testing.T) {
	originalList, originalRoot := listSudoRules, isRoot
	t.Cleanup(func() { listSudoRules, isRoot = originalList, originalRoot })
	isRoot = func() bool { return false }

	listSudoRules = func() (string, error) {
		return "User alice may run the following commands on host:\n" +
			"    (root) NOPASSWD: " + ZpoolPath() + " events -v -f, " + ZpoolPath() + " status -p -v\n", nil
	}
	assert.Equal(t, map[string]bool{"events": true, "data-errors": true, "smart": false, "pool-actions": false}, AllowedSudoersRules())

	// all commands of the rule are needed
	listSudoRules = func() (string, error) {
		return "User alice may run the following commands on host:\n" +
			"    (root) NOPASSWD: " + ZpoolPath() + " scrub [a-zA-Z]*\n", nil
	}
	assert.Equal(t, map[string]bool{"events": false, "data-errors": false, "smart": false, "pool-actions": false}, AllowedSudoersRules())

	listSudoRules = func() (string, error) {
		return "User alice may run the following commands on host:\n" +
			"    (root) NOPASSWD: " + disk.SmartctlPath() + " -j -i -H -A -n standby -- /dev/*\n", nil
	}
	assert.Equal(t, map[string]bool{"events": false, "data-errors": false, "smart": true, "pool-actions": false}, AllowedSudoersRules())

	listSudoRules = func() (string, error) { return "", errors.New("sudo: a password is required") }
	assert.Equal(t, map[string]bool{}, AllowedSudoersRules())

	isRoot = func() bool { return true }
	assert.Equal(t, map[string]bool{"events": true, "data-errors": true, "smart": true, "pool-actions": true}, AllowedSudoersRules())
}

func TestSudoersRuleSmart(t *testing.T) {
	assert.Equal(t, "alice ALL=(root) NOPASSWD: /usr/sbin/smartctl -j -i -H -A -n standby -- /dev/*, "+
		"/usr/bin/smartctl -j -i -H -A -n standby -- /dev/*, /sbin/smartctl -j -i -H -A -n standby -- /dev/*\n",
		SudoersRuleSmart.Lines("alice"))

	// the rule without -i is dropped, so it can be granted again
	former := ParseSudoersFile(sudoersFileHeader + "# Disk health\nalice ALL=(root) NOPASSWD: /usr/sbin/smartctl -j -H -A -n standby -- /dev/*, " +
		"/usr/bin/smartctl -j -H -A -n standby -- /dev/*, /sbin/smartctl -j -H -A -n standby -- /dev/*\n" + SudoersRuleEvents.Lines("alice"))
	assert.False(t, former.Has("alice", SudoersRuleSmart))
	assert.True(t, former.Has("alice", SudoersRuleEvents))
	assert.NotContains(t, former.String(), "smartctl")
	// read back
	file := ParseSudoersFile(SudoersRuleSmart.Lines("alice"))
	assert.True(t, file.Has("alice", SudoersRuleSmart))
	assert.False(t, file.Has("alice", SudoersRuleEvents))

	original := disk.SmartctlInstalled
	t.Cleanup(func() { disk.SmartctlInstalled = original })
	disk.SmartctlInstalled = func() bool { return false }
	assert.False(t, SudoersRuleSmart.IsAvailable(), "not offered without smartctl")
	assert.True(t, SudoersRuleEvents.IsAvailable())
	disk.SmartctlInstalled = func() bool { return true }
	assert.True(t, SudoersRuleSmart.IsAvailable())
}
