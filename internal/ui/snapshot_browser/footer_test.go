package snapshot_browser

import (
	"testing"
	"zfs-file-history/internal/data"
	"zfs-file-history/internal/zfs"

	"github.com/stretchr/testify/assert"
)

func TestFormatFooter(t *testing.T) {
	assert.Equal(t, "48 snapshots", formatFooter(48, 48, false))
	assert.Equal(t, "", formatFooter(0, 0, false))
	assert.Equal(t, "3 of 48 snapshots", formatFooter(3, 48, true))
	assert.Equal(t, "0 of 48 snapshots", formatFooter(0, 48, true))
}

func TestSnapshotMatchesFilter(t *testing.T) {
	entry := &data.SnapshotBrowserEntry{Snapshot: &zfs.Snapshot{Name: "zfs-auto-snap_Daily-2026-10-01"}}
	assert.True(t, snapshotMatchesFilter(entry, "daily"))
	assert.True(t, snapshotMatchesFilter(entry, "2026-10"))
	assert.False(t, snapshotMatchesFilter(entry, "weekly"))
	// glob
	assert.True(t, snapshotMatchesFilter(entry, "zfs-*-2026-??-01"))
	assert.False(t, snapshotMatchesFilter(entry, "daily*"))
}
