package zfs

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParseZpoolEvents(t *testing.T) {
	// GIVEN
	file, err := os.Open("testdata/zpool-events-test-data.txt")
	if err != nil {
		// Fallback in case it's in the same directory instead of testdata/
		file, err = os.Open("zpool-events-test-data.txt")
	}
	assert.NoError(t, err, "Failed to open test data file")
	defer file.Close()

	eventCount := 0
	onEvent := func() {
		eventCount++
	}

	// WHEN
	parseZpoolEvents(file, onEvent)

	// THEN
	// The provided text file contains 14 sysevent.fs.zfs.history_event blocks
	// (13 destroys and 1 snapshot)
	assert.Equal(t, 14, eventCount, "Expected exactly 14 snapshot/destroy events to be parsed")
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
`
	reader := strings.NewReader(dummyData)
	eventCount := 0

	// WHEN
	parseZpoolEvents(reader, func() {
		eventCount++
	})

	// THEN
	// It should ignore vdev_read and scrub history, but catch the snapshot
	assert.Equal(t, 1, eventCount)
}
