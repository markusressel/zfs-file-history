package zfs

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseZpoolEvents(t *testing.T) {
	// GIVEN
	file, err := os.Open("testdata/zpool-events-test-data.txt")
	if err != nil {
		file, err = os.Open("zpool-events-test-data.txt")
	}
	require.NoError(t, err, "Failed to open test data file")
	defer file.Close()

	eventCount := 0
	onEvent := func() {
		eventCount++
	}

	// Set a startTime far in the past so all events in the 2026 test file are processed
	startTime := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)

	// WHEN
	parseZpoolEvents(file, startTime, onEvent, func() { t.Error("no pool events expected") }, nil)

	// THEN
	assert.Equal(t, 14, eventCount, "Expected exactly 14 snapshot/destroy events to be parsed")
}

func TestParseZpoolEvents_FiltersOldEvents(t *testing.T) {
	// GIVEN
	file, err := os.Open("testdata/zpool-events-test-data.txt")
	if err != nil {
		file, err = os.Open("zpool-events-test-data.txt")
	}
	require.NoError(t, err, "Failed to open test data file")
	defer file.Close()

	eventCount := 0
	onEvent := func() {
		eventCount++
	}

	// Set a startTime in the future so ALL events in the test file are ignored
	startTime := time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)

	// WHEN
	parseZpoolEvents(file, startTime, onEvent, onEvent, nil)

	// THEN
	assert.Equal(t, 0, eventCount, "Expected all historical events to be completely filtered out")
}

func TestParseZpoolEvents_IgnoresOtherEvents(t *testing.T) {
	// GIVEN
	dummyData := `
Jul  5 2026 00:55:17.359478925 sysevent.fs.zfs.vdev_read
        version = 0x0
        class = "sysevent.fs.zfs.vdev_read"
        pool = "rpool"

Jul  5 2026 00:55:17.641483878 sysevent.fs.zfs.history_event
        version = 0x0
        class = "sysevent.fs.zfs.history_event"
        history_str = "zpool scrub rpool"

Jul  5 2026 00:55:17.857487671 sysevent.fs.zfs.history_event
        version = 0x0
        class = "sysevent.fs.zfs.history_event"
        history_internal_name = "snapshot"
        time = 0x6a498f55 0x156d368d
`
	reader := strings.NewReader(dummyData)
	eventCount := 0

	// Start time must be before the dummy event timestamp
	startTime := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)

	// WHEN
	parseZpoolEvents(reader, startTime, func() {
		eventCount++
	}, func() { t.Error("no pool events expected") }, nil)

	// THEN
	assert.Equal(t, 1, eventCount)
}

func TestParseZpoolEvents_PoolEvents(t *testing.T) {
	// GIVEN
	event := func(class string, lines ...string) string {
		return "Oct 13 2026 10:00:01.000000000 " + class + "\n" +
			"        class = \"" + class + "\"\n" +
			"        pool = \"tank\"\n" +
			strings.Join(lines, "") +
			"        time = 0x6a498f55 0x156d368d\n\n"
	}
	data := event("sysevent.fs.zfs.statechange") +
		event("sysevent.fs.zfs.scrub_start") +
		event("sysevent.fs.zfs.scrub_finish") +
		event("sysevent.fs.zfs.resilver_finish") +
		event("sysevent.fs.zfs.vdev_online") +
		event("sysevent.fs.zfs.config_sync") +
		event("ereport.fs.zfs.checksum") +
		event("resource.fs.zfs.removed") +
		// the pools, and with them the datasets, changed
		event("sysevent.fs.zfs.pool_import") +
		event("sysevent.fs.zfs.pool_export") +
		// a history event about something else than snapshots, e.g. "zpool scrub"
		event("sysevent.fs.zfs.history_event", "        history_str = \"zpool scrub tank\"\n") +
		event("sysevent.fs.zfs.history_event", "        history_internal_name = \"snapshot\"\n") +
		// not about the state of the pool
		event("sysevent.fs.zfs.vdev_read") +
		// without a time: ignored, like old events
		"Oct 13 2026 10:00:01.000000000 sysevent.fs.zfs.statechange\n        class = \"sysevent.fs.zfs.statechange\"\n"
	datasetEvents, poolEvents := 0, 0

	// WHEN
	parseZpoolEvents(strings.NewReader(data), time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC),
		func() { datasetEvents++ }, func() { poolEvents++ }, nil)

	// THEN
	assert.Equal(t, 3, datasetEvents, "the pool import and export and the snapshot")
	assert.Equal(t, 8, poolEvents)
}

func TestIsPoolEvent(t *testing.T) {
	assert.True(t, isPoolEvent("sysevent.fs.zfs.statechange"))
	assert.True(t, isPoolEvent("sysevent.fs.zfs.trim_finish"))
	assert.True(t, isPoolEvent("sysevent.fs.zfs.errorscrub_start"))
	assert.True(t, isPoolEvent("ereport.fs.zfs.io"))
	assert.False(t, isPoolEvent("sysevent.fs.zfs.history_event"))
	assert.False(t, isPoolEvent("sysevent.fs.zfs.pool_import"), "reloads everything instead")
	assert.False(t, isPoolEvent(""))
}

// The records ZFS logged in the history of a pool are the same as the ones of "zpool history -il", so the ones
// reported while the application runs are added to the history read before (see PoolHistoryRecorded).
func TestParseZpoolEvents_History(t *testing.T) {
	// GIVEN
	file, err := os.Open("testdata/zpool-events-test-data.txt")
	require.NoError(t, err)
	defer file.Close()
	var entries []PoolHistoryEntry

	// WHEN
	parseZpoolEvents(file, time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC), func() {}, func() {},
		func(entry PoolHistoryEntry) { entries = append(entries, entry) })

	// THEN
	require.Len(t, entries, 14)
	// as zpool prints the first one, in local time
	recorded := time.Unix(0x6a498f55, 0).Format(historyTimeLayout)
	listed := parsePoolHistory("History for 'rpool':\n" + recorded + " [txg:21098344] destroy " +
		"rpool/arch/DATA/default/home@2026-07-03-130000 (135693)   [on Silver]\n")
	require.Len(t, listed, 1)
	assert.Equal(t, listed[0], entries[0])
	assert.Equal(t, "destroy", entries[0].Event)
	assert.Equal(t, []string{"rpool/arch/DATA/default/home@2026-07-03-130000"}, entries[0].Datasets)
	assert.Equal(t, "Silver", entries[0].Host)
}

func TestParseZpoolEvents_HistoryValues(t *testing.T) {
	event := func(lines ...string) string {
		return "Oct 13 2026 10:00:01.000000000 sysevent.fs.zfs.history_event\n" +
			"        class = \"sysevent.fs.zfs.history_event\"\n" +
			strings.Join(lines, "") +
			"        time = 0x6a498f55 0x156d368d\n\n"
	}
	values := "        pool = \"tank\"\n        history_txg = 0x10\n        history_time = 0x6a498f55\n"
	tests := []struct {
		name  string
		event string
		want  *PoolHistoryEntry
	}{
		{"a property set", event(values, "        history_dsname = \"tank/data\"\n        history_dsid = 0x36\n",
			"        history_internal_str = \"compression=15\"\n        history_internal_name = \"set\"\n"),
			&PoolHistoryEntry{Event: "set", Datasets: []string{"tank/data"}, Text: "compression=15"}},
		{"without a dataset", event(values, "        history_internal_str = \"errors=0\"\n",
			"        history_internal_name = \"scan done\"\n"),
			&PoolHistoryEntry{Event: "scan done", Text: "errors=0"}},
		{"without a txg", event("        pool = \"tank\"\n        history_time = 0x6a498f55\n",
			"        history_internal_name = \"snapshot\"\n"), nil},
		{"a command", event(values, "        history_str = \"zpool scrub tank\"\n"), nil},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var entries []PoolHistoryEntry
			parseZpoolEvents(strings.NewReader(test.event), time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC), func() {}, func() {},
				func(entry PoolHistoryEntry) { entries = append(entries, entry) })
			if test.want == nil {
				assert.Empty(t, entries)
				return
			}
			want := *test.want
			want.Pool, want.Time, want.Internal, want.Txg = "tank", time.Unix(0x6a498f55, 0), true, 16
			assert.Equal(t, []PoolHistoryEntry{want}, entries)
		})
	}
}
