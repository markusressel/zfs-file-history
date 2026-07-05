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
	parseZpoolEvents(file, startTime, onEvent)

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
	parseZpoolEvents(file, startTime, onEvent)

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
	})

	// THEN
	assert.Equal(t, 1, eventCount)
}
