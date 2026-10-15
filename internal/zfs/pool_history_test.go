package zfs

import (
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// readHistoryFixture returns the records of testdata/zpool-history-il.txt, excerpts of the history of two pools of a
// real system (host and serial numbers replaced, the dumped key of "zfs load-key" replaced by other bytes).
func readHistoryFixture(t *testing.T) []PoolHistoryEntry {
	output, err := os.ReadFile("testdata/zpool-history-il.txt")
	require.NoError(t, err)
	return parsePoolHistory(string(output))
}

func TestParsePoolHistory(t *testing.T) {
	entries := readHistoryFixture(t)

	// the ioctls (and their dumped arguments) are left out
	require.Len(t, entries, 22)
	for _, entry := range entries {
		assert.NotContains(t, entry.Event, "ioctl")
		assert.NotContains(t, entry.Text, "input")
	}

	created := entries[0]
	assert.Equal(t, "bpool", created.Pool)
	assert.Equal(t, time.Date(2021, 8, 11, 22, 14, 31, 0, time.Local), created.Time)
	assert.True(t, created.Internal)
	assert.Equal(t, uint64(4), created.Txg)
	assert.Equal(t, "create", created.Event)
	assert.Empty(t, created.Datasets)
	assert.Contains(t, created.Text, "pool version 5000")
	assert.Equal(t, "archiso", created.Host)
	assert.Empty(t, created.User)

	// a property of the pool, then one of its root dataset
	assert.Equal(t, "compatibility=grub2", entries[1].Text)
	assert.Empty(t, entries[1].Datasets)
	assert.Equal(t, []string{"bpool"}, entries[2].Datasets)

	command := entries[3]
	assert.False(t, command.Internal)
	assert.Equal(t, "zpool create", command.Event)
	assert.Equal(t, "root", command.User)
	assert.Equal(t, "archiso", command.Host)
	assert.Contains(t, command.Text, "zpool create -o compatibility=grub2")
	assert.NotContains(t, command.Text, "[user")
	// "bpool" alone is the pool, the devices are no datasets
	assert.Empty(t, command.Datasets)

	assert.Equal(t, "rpool", entries[len(entries)-1].Pool)
}

func TestParsePoolHistoryEvents(t *testing.T) {
	type expected struct {
		internal bool
		event    string
		datasets []string
		text     string
	}
	byLine := map[string]expected{}
	for _, entry := range readHistoryFixture(t) {
		byLine[entry.Time.Format(historyTimeLayout)+" "+entry.Event] = expected{entry.Internal, entry.Event, entry.Datasets, entry.Text}
	}

	tests := []struct {
		key      string
		expected expected
	}{
		{"2025-03-27.02:59:11 set", expected{true, "set", []string{"bpool/arch/BOOT/pac-ujk76f"}, "org.bieaz:be=1"}},
		{"2025-03-27.02:59:11 zfs clone", expected{false, "zfs clone", []string{"bpool/arch/BOOT/default@pac-ujk76f", "bpool/arch/BOOT/pac-ujk76f"},
			"zfs clone -o canmount=noauto -o mountpoint=legacy bpool/arch/BOOT/default@pac-ujk76f bpool/arch/BOOT/pac-ujk76f"}},
		// the dataset with its object id
		{"2025-03-30.15:48:50 snapshot", expected{true, "snapshot", []string{"bpool/arch/BOOT/default@pac-qlkf2n"}, ""}},
		{"2025-03-30.15:48:50 zfs snapshot", expected{false, "zfs snapshot", []string{"bpool/arch/BOOT/default@pac-qlkf2n"}, "zfs snapshot bpool/arch/BOOT/default@pac-qlkf2n"}},
		// the origin of a clone is a dataset of the record too
		{"2025-03-30.15:48:50 clone", expected{true, "clone", []string{"bpool/arch/BOOT/pac-qlkf2n", "bpool/arch/BOOT/default@pac-qlkf2n"}, "origin=bpool/arch/BOOT/default@pac-qlkf2n (359)"}},
		// "(bptree, …)" is no object id
		{"2025-05-04.15:42:17 destroy", expected{true, "destroy", []string{"bpool/arch/BOOT/pac-saq4z3"}, "(bptree, mintxg=2872115)"}},
		{"2026-10-14.04:07:09 zpool scrub", expected{false, "zpool scrub", nil, "zpool scrub bpool"}},
		// events of more than one word
		{"2026-10-14.04:07:10 scan done", expected{true, "scan done", nil, "errors=0"}},
		{"2021-09-22.23:00:14 permission update", expected{true, "permission update", []string{"rpool/arch/DATA/default/home"}, "ul$1000 destroy"}},
		{"2024-09-21.21:17:08 scan cancelled", expected{true, "scan cancelled", nil, "errors=1"}},
		{"2026-10-14.01:49:08 clone swap", expected{true, "clone swap", []string{"rpool/arch/DATA/default/var/log/%rollback"}, "parent=log"}},
		{"2026-10-15.10:21:45 scan aborted, restarting", expected{true, "scan aborted, restarting", nil, "errors=0"}},
		{"2026-10-15.10:21:45 starting deferred resilver", expected{true, "starting deferred resilver", nil, "errors=0"}},
		{"2026-10-15.10:21:45 scan setup", expected{true, "scan setup", nil, "func=2 mintxg=22874773 maxtxg=22874795"}},
		{"2021-08-11.22:41:31 zfs load-key", expected{false, "zfs load-key", []string{"rpool/arch"}, "zfs load-key rpool/arch"}},
	}
	for _, test := range tests {
		t.Run(test.key, func(t *testing.T) {
			assert.Equal(t, test.expected, byLine[test.key])
		})
	}
}

func TestParsePoolHistoryWithoutLongFormat(t *testing.T) {
	entries := parsePoolHistory("History for 'tank':\n2024-01-02.03:04:05 zfs set compression=zstd tank/data\n")

	require.Len(t, entries, 1)
	assert.Equal(t, "zfs set compression=zstd tank/data", entries[0].Text)
	assert.Equal(t, []string{"tank/data"}, entries[0].Datasets)
	assert.Empty(t, entries[0].User)
	assert.Empty(t, entries[0].Host)
}

func TestParsePoolHistoryUnknownUser(t *testing.T) {
	entries := parsePoolHistory("History for 'tank':\n2024-01-02.03:04:05 zfs snapshot tank@a [user 1234 on host:linux]\n")

	require.Len(t, entries, 1)
	assert.Equal(t, "1234", entries[0].User)
	assert.Equal(t, "host", entries[0].Host)
}

func TestParsePoolHistoryUnprintable(t *testing.T) {
	entries := parsePoolHistory("History for 'tank':\n2024-01-02.03:04:05 zfs set org:note=a\x01\xff tank\n")

	require.Len(t, entries, 1)
	assert.Equal(t, "zfs set org:note=a?? tank", entries[0].Text)
}

func TestPoolHistoryEntryIsAbout(t *testing.T) {
	entry := PoolHistoryEntry{Datasets: []string{"tank/data/home@daily"}}

	assert.True(t, entry.IsAbout("tank/data/home"))
	assert.True(t, entry.IsAbout("tank/data"))
	assert.True(t, entry.IsAbout("tank"))
	assert.False(t, entry.IsAbout("tank/data/ho"))
	assert.False(t, entry.IsAbout("tank/other"))
	assert.False(t, (&PoolHistoryEntry{}).IsAbout("tank"))
}

func TestSystemPoolHistory(t *testing.T) {
	originalPool, originalSudo, originalRoot := runPoolCommand, runSudoCommand, isRoot
	t.Cleanup(func() { runPoolCommand, runSudoCommand, isRoot = originalPool, originalSudo, originalRoot })
	output := "History for 'tank':\n2024-01-02.03:04:05 zpool scrub tank [user 0 (root) on host:linux]\n"
	var sudoArgs []string
	runPoolCommand = func(name string, args ...string) (string, error) {
		assert.Equal(t, []string{"history", "-il"}, args)
		return output, nil
	}
	runSudoCommand = func(args ...string) (string, error) {
		sudoArgs = args
		return output, nil
	}

	// as root zpool runs directly
	isRoot = func() bool { return true }
	entries, err := System{}.PoolHistory()
	require.NoError(t, err)
	assert.Len(t, entries, 1)
	assert.Nil(t, sudoArgs)

	// otherwise with sudo, as allowed by the rule of the setup command
	isRoot = func() bool { return false }
	entries, err = System{}.PoolHistory()
	require.NoError(t, err)
	assert.Len(t, entries, 1)
	assert.Equal(t, []string{"-n", ZpoolPath(), "history", "-il"}, sudoArgs)
	assert.Equal(t, []string{strings.Join(sudoArgs[1:], " ")}, SudoersRuleHistory.AllowedCommands())

	// without the rule: how to allow it
	runSudoCommand = func(args ...string) (string, error) { return "", errors.New("sudo: a password is required") }
	_, err = System{}.PoolHistory()
	require.Error(t, err)
	assert.Contains(t, err.Error(), SetupHistoryHint)
}
