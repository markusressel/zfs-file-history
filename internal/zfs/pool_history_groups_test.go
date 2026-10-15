package zfs

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// groupHistory groups the records of a history of the pool tank, written like "zpool history -il" prints it.
func groupHistory(t *testing.T, records string) []*PoolHistoryGroup {
	t.Helper()
	entries := parsePoolHistory("History for 'tank':\n" + records)
	groups := GroupPoolHistory(entries)
	total := 0
	for _, group := range groups {
		total += len(group.Entries)
	}
	require.Equal(t, len(entries), total, "each record is in one group")
	return groups
}

// groupEvents returns the events of the records of each group.
func groupEvents(groups []*PoolHistoryGroup) [][]string {
	var all [][]string
	for _, group := range groups {
		var events []string
		for _, entry := range group.Entries {
			events = append(events, entry.Event)
		}
		all = append(all, events)
	}
	return all
}

// groupKinds returns the kind of each group.
func groupKinds(groups []*PoolHistoryGroup) []PoolHistoryGroupKind {
	var kinds []PoolHistoryGroupKind
	for _, group := range groups {
		kinds = append(kinds, group.Kind)
	}
	return kinds
}

func TestGroupPoolHistory_CommandWithItsEvents(t *testing.T) {
	groups := groupHistory(t, `2024-01-02.01:00:00 [txg:5] snapshot tank/a@daily (12) [on host]
2024-01-02.01:00:00 [txg:5] snapshot tank/a/b@daily (13) [on host]
2024-01-02.01:00:00 [txg:5] snapshot tank@daily (14) [on host]
2024-01-02.01:00:00 zfs snapshot -r tank@daily [user 0 (root) on host:linux]
2024-01-02.03:00:00 [txg:9] destroy tank/c@old (20) [on host]
2024-01-02.03:00:00 zfs destroy tank/a@old [user 0 (root) on host:linux]
`)

	assert.Equal(t, []PoolHistoryGroupKind{PoolHistoryCommand, PoolHistorySingle, PoolHistorySingle}, groupKinds(groups))
	assert.Equal(t, [][]string{{"snapshot", "snapshot", "snapshot", "zfs snapshot"}, {"destroy"}, {"zfs destroy"}}, groupEvents(groups))
}

// ZFS logs the events of a command while it runs, so the events of commands that run in a row are logged between
// them: they belong to the command that names their datasets.
func TestGroupPoolHistory_EventsLoggedBeforeAnotherCommand(t *testing.T) {
	groups := groupHistory(t, `2024-01-02.01:00:00 [txg:5] destroy tank/docker/a (12) [on host]
2024-01-02.01:00:00 [txg:5] destroy tank/docker/b@s (13) [on host]
2024-01-02.01:00:00 zfs destroy -r tank/docker/a [user 0 (root) on host:linux]
2024-01-02.01:00:00 [txg:6] destroy tank/docker/b (14) [on host]
2024-01-02.01:00:00 zfs destroy -r tank/docker/b [user 0 (root) on host:linux]
`)

	require.Equal(t, []PoolHistoryGroupKind{PoolHistoryCommands}, groupKinds(groups))
	entries := groups[0].Entries
	assert.Equal(t, []string{"tank/docker/a", "tank/docker/a", "tank/docker/b@s", "tank/docker/b", "tank/docker/b"},
		[]string{entries[0].Dataset(), entries[1].Dataset(), entries[2].Dataset(), entries[3].Dataset(), entries[4].Dataset()})
}

// the holds of zfs send are logged when it starts, the command when it ended, possibly hours later
func TestGroupPoolHistory_SendHolds(t *testing.T) {
	groups := groupHistory(t, `2024-01-02.01:00:00 [txg:5] hold tank/a@s (12) tag=.send-123-1 temp=1 refs=1 [on host]
2024-01-02.03:00:00 [txg:9] release tank/a@s (12) tag=.send-123-1 refs=0 [on host]
2024-01-02.03:00:00 zfs send tank/a@s [user 1000 (alice) on host:linux]
`)

	assert.Equal(t, []PoolHistoryGroupKind{PoolHistoryCommand}, groupKinds(groups))
}

// a rollback changes the dataset of the snapshot it was given: it destroys the newer snapshots
func TestGroupPoolHistory_Rollback(t *testing.T) {
	groups := groupHistory(t, `2024-01-02.01:00:00 [txg:5] destroy tank/a@newer (12) [on host]
2024-01-02.01:00:00 [txg:6] clone swap tank/a/%rollback (13) parent=a [on host]
2024-01-02.01:00:00 zfs rollback -r tank/a@older [user 0 (root) on host:linux]
`)

	assert.Equal(t, []PoolHistoryGroupKind{PoolHistoryCommand}, groupKinds(groups))
}

// events that name no dataset belong to a command that names none at about the same time
func TestGroupPoolHistory_Import(t *testing.T) {
	groups := groupHistory(t, `2024-01-02.01:00:00 [txg:5] open pool version 5000; software version zfs-2.1.0 [on host]
2024-01-02.01:00:00 [txg:7] import pool version 5000; software version zfs-2.1.0 [on host]
2024-01-02.01:00:01 zpool import -N tank [user 0 (root) on host:linux]
2024-01-02.02:00:00 [txg:9] set tank (54) compression=15 [on host]
2024-01-02.02:00:00 zfs set compression=lz4 tank [user 0 (root) on host:linux]
`)

	assert.Equal(t, []PoolHistoryGroupKind{PoolHistoryCommand, PoolHistoryCommand}, groupKinds(groups))
}

// "zpool scrub -s" is logged after "scan cancelled", and is part of the scrub it cancelled
func TestGroupPoolHistory_Scrub(t *testing.T) {
	groups := groupHistory(t, `2024-01-02.01:00:00 [txg:5] scan setup func=1 mintxg=0 maxtxg=5 [on host]
2024-01-02.01:00:01 zpool scrub tank [user 0 (root) on host:linux]
2024-01-02.01:05:00 [txg:9] scan cancelled errors=1 [on host]
2024-01-02.01:05:00 zpool scrub -s tank [user 0 (root) on host:linux]
2024-01-02.02:00:00 [txg:15] scan setup func=1 mintxg=0 maxtxg=15 [on host]
2024-01-02.02:00:01 zpool scrub tank [user 0 (root) on host:linux]
2024-01-02.02:10:00 [txg:20] scan done errors=3 [on host]
`)

	require.Equal(t, []PoolHistoryGroupKind{PoolHistoryScrub, PoolHistoryScrub}, groupKinds(groups))
	assert.Equal(t, [][]string{{"scan setup", "zpool scrub", "scan cancelled", "zpool scrub"}, {"scan setup", "zpool scrub", "scan done"}},
		groupEvents(groups))
	assert.Equal(t, PoolHistoryCancelled, groups[0].Outcome)
	assert.Equal(t, PoolHistoryDone, groups[1].Outcome)
	assert.Equal(t, uint64(3), groups[1].Errors)
}

// a device taken offline and back online during a scrub: ZFS stops the scrub for a resilver
func TestGroupPoolHistory_ScrubRestartedForResilver(t *testing.T) {
	groups := groupHistory(t, `2024-01-02.01:00:00 [txg:5] scan setup func=1 mintxg=0 maxtxg=5 [on host]
2024-01-02.01:00:01 zpool scrub tank [user 0 (root) on host:linux]
2024-01-02.01:06:00 zpool offline -t tank /dev/sda1 [user 0 (root) on host:linux]
2024-01-02.01:07:00 [txg:9] scan aborted, restarting errors=0 [on host]
2024-01-02.01:07:00 [txg:9] starting deferred resilver errors=0 [on host]
2024-01-02.01:07:00 [txg:9] scan setup func=2 mintxg=7 maxtxg=9 [on host]
2024-01-02.01:07:01 [txg:11] scan done errors=0 [on host]
2024-01-02.01:07:01 zpool online tank /dev/sda1 [user 0 (root) on host:linux]
`)

	require.Equal(t, []PoolHistoryGroupKind{PoolHistoryScrub, PoolHistorySingle, PoolHistoryResilver, PoolHistorySingle}, groupKinds(groups))
	assert.Equal(t, PoolHistoryRestarted, groups[0].Outcome)
	assert.Equal(t, [][]string{{"scan setup", "zpool scrub", "scan aborted, restarting"}, {"zpool offline"},
		{"starting deferred resilver", "scan setup", "scan done"}, {"zpool online"}}, groupEvents(groups))
	assert.Equal(t, PoolHistoryDone, groups[2].Outcome)
}

func TestGroupPoolHistory_ScanEnds(t *testing.T) {
	for _, test := range []struct {
		name    string
		records string
		kind    PoolHistoryGroupKind
		outcome PoolHistoryOutcome
	}{
		{"older versions of ZFS log no scan setup", `2024-01-02.01:00:00 zpool scrub tank [user 0 (root) on host:linux]
2024-01-02.01:10:00 [txg:9] scan done errors=0 [on host]`, PoolHistoryScrub, PoolHistoryDone},
		{"a scan whose start is not logged", `2024-01-02.01:10:00 [txg:9] scan done errors=0 [on host]`, PoolHistoryScan, PoolHistoryDone},
		{"running", `2024-01-02.01:00:00 [txg:5] scan setup func=2 mintxg=0 maxtxg=5 [on host]`, PoolHistoryResilver, PoolHistoryRunning},
		{"paused", `2024-01-02.01:00:00 [txg:5] scan setup func=1 mintxg=0 maxtxg=5 [on host]
2024-01-02.01:00:01 zpool scrub tank [user 0 (root) on host:linux]
2024-01-02.01:05:00 zpool scrub -p tank [user 0 (root) on host:linux]`, PoolHistoryScrub, PoolHistoryPaused},
	} {
		t.Run(test.name, func(t *testing.T) {
			groups := groupHistory(t, test.records+"\n")

			require.Len(t, groups, 1)
			assert.Equal(t, test.kind, groups[0].Kind)
			assert.Equal(t, test.outcome, groups[0].Outcome)
		})
	}

	groups := groupHistory(t, `2024-01-02.01:00:00 [txg:5] scan setup func=1 mintxg=0 maxtxg=5 [on host]
2024-01-02.02:00:00 [txg:9] scan setup func=1 mintxg=0 maxtxg=9 [on host]
`)
	require.Len(t, groups, 2)
	assert.Equal(t, PoolHistoryEndUnknown, groups[0].Outcome)
	assert.Equal(t, PoolHistoryRunning, groups[1].Outcome)
}

// a TRIM runs on each device on its own, and ends when it ended on all; "zpool trim -c" is logged after
func TestGroupPoolHistory_Trim(t *testing.T) {
	groups := groupHistory(t, `2024-01-02.01:00:00 [txg:5] trim vdev=/dev/sda1 activated [on host]
2024-01-02.01:00:00 [txg:5] trim vdev=/dev/sdb1 activated [on host]
2024-01-02.01:00:00 zpool trim tank [user 0 (root) on host:linux]
2024-01-02.01:20:00 [txg:9] trim vdev=/dev/sda1 complete [on host]
2024-01-02.01:30:00 [txg:12] trim vdev=/dev/sdb1 canceled [on host]
2024-01-02.01:30:00 zpool trim -c tank [user 0 (root) on host:linux]
`)

	require.Equal(t, []PoolHistoryGroupKind{PoolHistoryTrim}, groupKinds(groups))
	assert.Len(t, groups[0].Entries, 6)
	assert.Equal(t, PoolHistoryCancelled, groups[0].Outcome)
	assert.Equal(t, []string{"/dev/sda1", "/dev/sdb1"}, groups[0].Devices)
}

// the commands one user ran less than a minute apart, e.g. the runs of a backup tool, with the events of no command
func TestGroupPoolHistory_Runs(t *testing.T) {
	groups := groupHistory(t, `2024-01-02.01:00:00 [txg:5] snapshot tank/home@1 (12) [on host]
2024-01-02.01:00:00 zfs snapshot tank/home@1 [user 1000 (alice) on host:linux]
2024-01-02.01:00:30 zfs send tank/home@1 [user 1000 (alice) on host:linux]
2024-01-02.01:00:40 [txg:6] destroy tank/other@x (13) [on host]
2024-01-02.01:01:00 [txg:7] destroy tank/home@0 (11) [on host]
2024-01-02.01:01:00 zfs destroy tank/home@0 [user 1000 (alice) on host:linux]
2024-01-02.01:01:30 zfs set compression=lz4 tank/home [user 0 (root) on host:linux]
2024-01-02.01:05:00 zfs destroy tank/home@1 [user 1000 (alice) on host:linux]
2024-01-02.02:00:00 [txg:20] destroy tank/x (30) [on host]
2024-01-02.02:00:01 [txg:21] destroy tank/y (31) [on host]
`)

	assert.Equal(t, []PoolHistoryGroupKind{PoolHistoryCommands, PoolHistorySingle, PoolHistorySingle, PoolHistoryCommands}, groupKinds(groups))
	assert.Equal(t, [][]string{{"snapshot", "zfs snapshot", "zfs send", "destroy", "destroy", "zfs destroy"}, {"zfs set"},
		{"zfs destroy"}, {"destroy", "destroy"}}, groupEvents(groups))
}

// a scrub does not interrupt the commands run while it runs
func TestGroupPoolHistory_RunDuringScrub(t *testing.T) {
	groups := groupHistory(t, `2024-01-02.01:00:00 zfs snapshot tank/home@1 [user 1000 (alice) on host:linux]
2024-01-02.01:00:10 [txg:5] scan setup func=1 mintxg=0 maxtxg=5 [on host]
2024-01-02.01:00:11 zpool scrub tank [user 0 (root) on host:linux]
2024-01-02.01:00:20 zfs send tank/home@1 [user 1000 (alice) on host:linux]
`)

	assert.Equal(t, []PoolHistoryGroupKind{PoolHistoryCommands, PoolHistoryScrub}, groupKinds(groups))
}

func TestGroupPoolHistory_Fixture(t *testing.T) {
	output, err := os.ReadFile("testdata/zpool-history-il.txt")
	require.NoError(t, err)
	entries := parsePoolHistory(string(output))
	var rpool []PoolHistoryEntry
	for _, entry := range entries {
		if entry.Pool == "rpool" {
			rpool = append(rpool, entry)
		}
	}

	groups := GroupPoolHistory(rpool)

	var titles []string
	for _, group := range groups {
		titles = append(titles, strings.Join(groupEvents([]*PoolHistoryGroup{group})[0], ", "))
	}
	assert.Equal(t, []string{
		"permission update",
		"zfs load-key",
		"scan cancelled",
		"clone swap",
		"zpool offline",
		"scan aborted, restarting",
		"starting deferred resilver, scan setup, scan done",
		"zpool online",
	}, titles)
}

func TestDatasetCovers(t *testing.T) {
	for _, test := range []struct {
		given, name string
		covers      bool
	}{
		{"tank/a", "tank/a", true},
		{"tank/a", "tank/a/b", true},
		{"tank/a", "tank/a/b@s", true},
		{"tank/a", "tank/ab", false},
		{"tank@s", "tank/a@s", true},
		{"tank@s", "tank/a@t", false},
		{"tank@s", "tank/a", false},
		{"tank/a@s%u", "tank/a@t", true},
		{"tank/a@s,u", "tank/a@t", true},
		{"tank/a@s%u", "tank/a", false},
		{"tank/a#b", "tank/a#b", true},
	} {
		assert.Equal(t, test.covers, datasetCovers(test.given, test.name), "%s covers %s", test.given, test.name)
	}
}
