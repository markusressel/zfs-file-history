package zfs

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func readTestData(t *testing.T, name string) string {
	data, err := os.ReadFile(filepath.Join("testdata", name))
	require.NoError(t, err)
	return string(data)
}

func TestParsePoolList(t *testing.T) {
	output := strings.Join([]string{
		"rpool\t987842478080\t407037546496\t580804931584\t39\t41\t1.00\tONLINE",
		// e.g. a faulted pool, whose fragmentation is not known
		"tank\t1000\t0\t1000\t-\t0\t1.25\tFAULTED",
		"",
	}, "\n")

	result, err := parsePoolList(output)

	require.NoError(t, err)
	assert.Equal(t, []*PoolListEntry{
		{Name: "rpool", Size: 987842478080, Allocated: 407037546496, Free: 580804931584, Fragmentation: 39, Capacity: 41, DedupRatio: "1.00", Health: PoolStateOnline},
		{Name: "tank", Size: 1000, Allocated: 0, Free: 1000, Fragmentation: -1, Capacity: 0, DedupRatio: "1.25", Health: PoolStateFaulted},
	}, result)
}

func TestParsePoolList_Empty(t *testing.T) {
	result, err := parsePoolList("")
	require.NoError(t, err)
	assert.Empty(t, result)
}

func TestParsePoolList_Errors(t *testing.T) {
	tests := map[string]string{
		"missing fields":   "rpool\t1000\t0",
		"invalid size":     "rpool\tbig\t0\t1000\t39\t41\t1.00\tONLINE",
		"invalid capacity": "rpool\t1000\t0\t1000\t39\tfull\t1.00\tONLINE",
	}
	for name, output := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := parsePoolList(output)
			assert.Error(t, err)
		})
	}
}

func TestParsePercentProperty(t *testing.T) {
	value, err := parsePercentProperty("39")
	require.NoError(t, err)
	assert.Equal(t, 39, value)

	value, err = parsePercentProperty("41%")
	require.NoError(t, err)
	assert.Equal(t, 41, value)

	value, err = parsePercentProperty("-")
	require.NoError(t, err)
	assert.Equal(t, -1, value)
}

func TestPoolListEntry_TableRowId(t *testing.T) {
	assert.Equal(t, "rpool", PoolListEntry{Name: "rpool"}.TableRowId())
}

func TestParsePoolStatus_Healthy(t *testing.T) {
	status, err := parsePoolStatus(readTestData(t, "zpool-status-healthy.txt"))

	require.NoError(t, err)
	assert.Equal(t, "rpool", status.Name)
	assert.Equal(t, PoolStateOnline, status.State)
	// the continuation lines are kept as lines
	assert.Equal(t, "Some supported and requested features are not enabled on the pool.\n"+
		"The pool can still be used, but some features are unavailable.", status.Status)
	assert.Equal(t, "Enable all features using 'zpool upgrade'. Once this is done,\n"+
		"the pool may no longer be accessible by software that does not support\n"+
		"the features. See zpool-features(7) for details.", status.Action)
	assert.Equal(t, "scrub repaired 0B in 00:06:46 with 0 errors on Thu Oct  1 12:50:53 2026", status.Scan)
	assert.False(t, status.IsScanning())
	assert.Equal(t, -1.0, status.ScanProgress)
	assert.Equal(t, "No known data errors", status.Errors)
	assert.Empty(t, status.See)
	assert.Empty(t, status.Other)

	assert.Equal(t, []*PoolDevice{
		{Name: "rpool", Depth: 0, State: PoolStateOnline},
		{Name: "mirror-0", Depth: 1, State: PoolStateOnline},
		{Name: "/dev/disk/by-id/nvme-SSD_1TB_SERIAL0001-part3", Depth: 2, State: PoolStateOnline},
		{Name: "/dev/disk/by-id/nvme-SSD_1TB_SERIAL0002-part3", Depth: 2, State: PoolStateOnline},
	}, status.Devices)
}

func TestParsePoolStatus_Resilvering(t *testing.T) {
	status, err := parsePoolStatus(readTestData(t, "zpool-status-resilvering.txt"))

	require.NoError(t, err)
	assert.Equal(t, "tank", status.Name)
	assert.Equal(t, PoolStateDegraded, status.State)
	assert.Equal(t, "Wait for the resilver to complete.", status.Action)
	assert.True(t, status.IsScanning())
	assert.InDelta(t, 31.77, status.ScanProgress, 0.001)
	assert.Equal(t, "resilver in progress since Tue Oct 13 09:12:44 2026\n"+
		"1.43T / 3.21T scanned at 412M/s, 1.02T / 3.21T issued at 294M/s\n"+
		"254G resilvered, 31.77% done, 02:10:31 to go", status.Scan)
	assert.Equal(t, "1 data errors, use '-v' for a list", status.Errors)

	assert.Equal(t, []*PoolDevice{
		{Name: "tank", Depth: 0, State: PoolStateDegraded},
		{Name: "raidz2-0", Depth: 1, State: PoolStateDegraded},
		{Name: "sda", Depth: 2, State: PoolStateOnline},
		{Name: "sdb", Depth: 2, State: PoolStateOnline, Checksum: 2},
		{Name: "replacing-2", Depth: 2, State: PoolStateDegraded},
		{Name: "sdc", Depth: 3, State: PoolStateFaulted, Read: 12, Write: 340, Note: "too many errors"},
		{Name: "sde", Depth: 3, State: PoolStateOnline, Note: "(resilvering)"},
		{Name: "sdd", Depth: 2, State: PoolStateOnline},
		// the groups are on the level of the pool
		{Name: "logs", Depth: 0},
		{Name: "mirror-1", Depth: 1, State: PoolStateOnline},
		{Name: "nvme0n1p1", Depth: 2, State: PoolStateOnline},
		{Name: "nvme1n1p1", Depth: 2, State: PoolStateOnline},
		{Name: "cache", Depth: 0},
		{Name: "nvme0n1p2", Depth: 1, State: PoolStateOnline},
		{Name: "spares", Depth: 0},
		// no error counters
		{Name: "sdf", Depth: 1, State: PoolStateAvail},
		{Name: "sdg", Depth: 1, State: PoolStateInUse, Note: "currently in use"},
	}, status.Devices)

	assert.True(t, status.Devices[8].IsGroup())
	assert.False(t, status.Devices[0].IsGroup())
	assert.True(t, status.Devices[3].HasErrors())
	assert.True(t, status.Devices[5].HasErrors())
	assert.False(t, status.Devices[6].HasErrors())
}

func TestParsePoolStatus_ScrubbingAndOtherFields(t *testing.T) {
	status, err := parsePoolStatus(readTestData(t, "zpool-status-scrubbing.txt"))

	require.NoError(t, err)
	// only the first pool
	assert.Equal(t, "backup", status.Name)
	assert.Len(t, status.Devices, 2)
	assert.Empty(t, status.Status)

	// running, but no progress yet
	assert.True(t, status.IsScanning())
	assert.Equal(t, 0.0, status.ScanProgress)

	assert.Equal(t, []PoolStatusField{{
		Name: "remove",
		Value: "Removal of vdev 1 copied 12.3G in 0h1m, completed on Mon Oct 12 08:00:00 2026\n" +
			"1.20M memory used for removed device mappings",
	}}, status.Other)
}

func TestParsePoolStatus_FieldNameWithSpace(t *testing.T) {
	output := strings.Join([]string{
		"  pool: tank",
		" state: ONLINE",
		"  scan: scrub repaired 0B in 00:00:01 with 0 errors on Tue Oct 13 10:00:01 2026",
		"scan warning: skipped blocks that are only referenced by the checkpoint.",
		"errors: No known data errors",
	}, "\n")

	status, err := parsePoolStatus(output)

	require.NoError(t, err)
	assert.Equal(t, []PoolStatusField{{Name: "scan warning", Value: "skipped blocks that are only referenced by the checkpoint."}}, status.Other)
	assert.Equal(t, "No known data errors", status.Errors)
}

func TestParsePoolStatus_ScanStarting(t *testing.T) {
	output := strings.Join([]string{
		"  pool: tank",
		" state: ONLINE",
		"  scan: scrub in progress since Tue Oct 13 10:00:01 2026",
		"config:",
		"",
		"\tNAME        STATE     READ WRITE CKSUM",
		"\ttank        ONLINE       0     0     0",
	}, "\n")

	status, err := parsePoolStatus(output)

	require.NoError(t, err)
	assert.True(t, status.IsScanning(), "running, though there is no progress yet")
	assert.Equal(t, 0.0, status.ScanProgress)
}

func TestParsePoolStatus_Errors(t *testing.T) {
	tests := map[string]string{
		"empty":                  "",
		"no pool":                "no pools available\n",
		"continuation first":     "\tsomething\n",
		"continuation in config": "  pool: tank\nconfig:\n\tNAME STATE READ WRITE CKSUM\nunexpected\n",
	}
	for name, output := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := parsePoolStatus(output)
			assert.Error(t, err)
		})
	}
}

func TestParsePoolDevice(t *testing.T) {
	tests := []struct {
		line     string
		expected *PoolDevice
	}{
		{"NAME        STATE     READ WRITE CKSUM", nil},
		{"   ", nil},
		{"tank        ONLINE       0     0     0", &PoolDevice{Name: "tank", State: PoolStateOnline}},
		{"    sda     REMOVED      1     2     3  was /dev/sda", &PoolDevice{Name: "sda", Depth: 2, State: PoolStateRemoved, Read: 1, Write: 2, Checksum: 3, Note: "was /dev/sda"}},
		{"  special", &PoolDevice{Name: "special", Depth: 1}},
		// counters that are no numbers are kept as the note
		{"  sdb  UNAVAIL  cannot open  the device", &PoolDevice{Name: "sdb", Depth: 1, State: PoolStateUnavail, Note: "cannot open the device"}},
		{"  sdc  UNAVAIL  cannot open", &PoolDevice{Name: "sdc", Depth: 1, State: PoolStateUnavail, Note: "cannot open"}},
	}
	for _, test := range tests {
		t.Run(test.line, func(t *testing.T) {
			assert.Equal(t, test.expected, parsePoolDevice(test.line))
		})
	}
}

func TestGetPoolStatus_EmptyName(t *testing.T) {
	_, err := GetPoolStatus("")
	assert.Error(t, err)
}

func TestPoolStatus_ScanKindAndFinished(t *testing.T) {
	tests := []struct {
		scan     string
		kind     string
		finished time.Time
	}{
		{"scrub repaired 0B in 00:06:46 with 0 errors on Thu Oct  1 12:50:53 2026", "scrub", time.Date(2026, 10, 1, 12, 50, 53, 0, time.Local)},
		{"resilvered 254G in 02:10:31 with 3 errors on Tue Oct 13 11:23:15 2026", "resilver", time.Date(2026, 10, 13, 11, 23, 15, 0, time.Local)},
		{"scrub in progress since Tue Oct 13 10:00:01 2026\n0B repaired, 0.00% done", "scrub", time.Time{}},
		{"resilver in progress since Tue Oct 13 09:12:44 2026", "resilver", time.Time{}},
		{"scrub canceled on Tue Oct 13 10:00:01 2026", "scrub", time.Time{}},
		{"scrub paused since Tue Oct 13 10:00:01 2026", "scrub", time.Time{}},
		{"none requested", "", time.Time{}},
		{"", "", time.Time{}},
		// not a date
		{"scrub repaired 0B in 00:06:46 with 0 errors on someday", "scrub", time.Time{}},
	}
	for _, test := range tests {
		t.Run(test.scan, func(t *testing.T) {
			status := &PoolStatus{Scan: test.scan}
			assert.Equal(t, test.kind, status.ScanKind())
			finished, ok := status.ScanFinished()
			assert.Equal(t, !test.finished.IsZero(), ok)
			assert.True(t, test.finished.Equal(finished), "%s", finished)
		})
	}
}
