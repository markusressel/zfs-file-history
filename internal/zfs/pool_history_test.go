package zfs

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
	"zfs-file-history/internal/testutil"

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

// fakeHistoryCommand replaces streamCommand: it passes the lines of the output of each command (by its arguments
// after "-il"; "" for all pools) to onLine, until it returns false. A missing output fails like a denied sudo.
func fakeHistoryCommand(t *testing.T, outputs map[string]string) (calls *[][]string, read *int) {
	calls, read = &[][]string{}, new(int)
	testutil.Replace(t, &streamCommand, func(ctx context.Context, onLine func(line []byte) bool, name string, args ...string) error {
		*calls = append(*calls, append([]string{name}, args...))
		_, pool, _ := strings.Cut(strings.Join(args, " "), "history -il")
		output, ok := outputs[strings.TrimSpace(pool)]
		if !ok {
			return errors.New("sudo: a password is required")
		}
		for line := range strings.SplitSeq(output, "\n") {
			*read++
			if !onLine([]byte(line)) {
				return nil
			}
		}
		return nil
	})
	return calls, read
}

func TestSystemPoolHistory(t *testing.T) {
	tank := "History for 'tank':\n2024-01-02.03:04:05 zpool scrub tank [user 0 (root) on host:linux]\n"
	calls, _ := fakeHistoryCommand(t, map[string]string{"tank": tank})

	// as root zpool runs directly
	testutil.Replace(t, &isRoot, func() bool { return true })
	var progress []PoolHistoryProgress
	entries, err := System{}.PoolHistory(context.Background(), "tank", func(p PoolHistoryProgress) { progress = append(progress, p) })
	require.NoError(t, err)
	assert.Len(t, entries, 1)
	assert.Equal(t, [][]string{{ZpoolPath(), "history", "-il", "tank"}}, *calls)
	assert.Equal(t, []PoolHistoryProgress{{Bytes: int64(len(tank)) + 1, Records: 1}}, progress, "the end is reported")

	// otherwise with sudo, as allowed by the rule of the setup command
	testutil.Replace(t, &isRoot, func() bool { return false })
	*calls = nil
	entries, err = System{}.PoolHistory(context.Background(), "tank", nil)
	require.NoError(t, err)
	assert.Len(t, entries, 1)
	assert.Equal(t, [][]string{{"sudo", "-n", ZpoolPath(), "history", "-il", "tank"}}, *calls)
	assert.Equal(t, []string{ZpoolPath() + " history -il [a-zA-Z]*"}, SudoersRuleHistory.AllowedCommands())

	// without the rule: how to allow it
	*calls = nil
	_, err = System{}.PoolHistory(context.Background(), "other", nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), SetupHistoryHint)
}

// With the rule of former versions, which only allows the history of all pools, the records of the pool are read
// from it, and zpool is stopped once they were read.
func TestSystemPoolHistory_AllPools(t *testing.T) {
	testutil.Replace(t, &isRoot, func() bool { return false })
	all := "History for 'bpool':\n2024-01-01.00:00:00 zpool create bpool sda\n" +
		"History for 'rpool':\n2024-01-02.00:00:00 zpool create rpool sdb\n2024-01-03.00:00:00 zfs snapshot rpool/a@s\n" +
		"History for 'tank':\n2024-01-04.00:00:00 zpool create tank sdc\n"
	calls, read := fakeHistoryCommand(t, map[string]string{"": all})

	entries, err := System{}.PoolHistory(context.Background(), "rpool", nil)
	require.NoError(t, err)
	assert.Equal(t, [][]string{{"sudo", "-n", ZpoolPath(), "history", "-il", "rpool"}, {"sudo", "-n", ZpoolPath(), "history", "-il"}}, *calls)
	require.Len(t, entries, 2)
	assert.Equal(t, "zpool create rpool sdb", entries[0].Text)
	assert.Equal(t, "zfs snapshot rpool/a@s", entries[1].Text)
	assert.Equal(t, 6, *read, "stopped at the history of the next pool")
}

// streamCommand passes the lines while the command runs (also ones longer than its buffer), stops it once onLine
// returns false, and fails with what the command printed.
func TestStreamCommand(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("no sh")
	}
	long := strings.Repeat("x", 3<<20)
	var lines []string
	err := streamCommand(context.Background(), func(line []byte) bool {
		lines = append(lines, string(line))
		return true
	}, "sh", "-c", fmt.Sprintf("echo a; head -c %d /dev/zero | tr '\\0' x; echo; printf b", len(long)))
	require.NoError(t, err)
	assert.Equal(t, []string{"a", long, "b"}, lines)

	// stopped: an endless command ends
	count := 0
	err = streamCommand(context.Background(), func(line []byte) bool {
		count++
		return count < 3
	}, "sh", "-c", "yes")
	require.NoError(t, err)
	assert.Equal(t, 3, count)

	// cancelled
	ctx, cancel := context.WithCancel(context.Background())
	err = streamCommand(ctx, func(line []byte) bool {
		cancel()
		return true
	}, "sh", "-c", "yes")
	assert.ErrorIs(t, err, context.Canceled)

	err = streamCommand(context.Background(), func(line []byte) bool { return true }, "sh", "-c", "echo denied >&2; exit 1")
	assert.EqualError(t, err, "denied")
}
