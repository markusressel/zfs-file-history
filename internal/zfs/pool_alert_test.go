package zfs

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPoolStatus_Alerts_Healthy(t *testing.T) {
	// features that are not enabled are no problem
	status, err := parsePoolStatus(readTestData(t, "zpool-status-healthy.txt"))
	require.NoError(t, err)

	assert.Empty(t, status.Alerts())
	assert.Empty(t, status.DataErrors())
}

func TestPoolStatus_Alerts_DataErrors(t *testing.T) {
	status, err := parsePoolStatus(readTestData(t, "zpool-status-data-errors.txt"))
	require.NoError(t, err)

	assert.Equal(t, "18 data errors", status.DataErrors())
	assert.Equal(t, []PoolAlert{
		{Pool: "rpool", Severity: PoolAlertError, Kind: PoolAlertDataErrors, Message: "18 data errors in files or metadata"},
	}, status.Alerts())
}

func TestPoolStatus_Alerts_Degraded(t *testing.T) {
	status, err := parsePoolStatus(readTestData(t, "zpool-status-resilvering.txt"))
	require.NoError(t, err)

	// the errors first, then the warnings
	assert.Equal(t, []PoolAlert{
		{Pool: "tank", Severity: PoolAlertError, Kind: PoolAlertDataErrors, Message: "1 data errors in files or metadata"},
		{Pool: "tank", Severity: PoolAlertError, Kind: PoolAlertDeviceState, Message: "Devices not available: sdc FAULTED (too many errors)"},
		{Pool: "tank", Severity: PoolAlertWarning, Kind: PoolAlertState, Message: "Pool is DEGRADED: One or more devices is currently being resilvered."},
		{Pool: "tank", Severity: PoolAlertWarning, Kind: PoolAlertDeviceErrors, Message: "Device errors: sdb 2 checksum; sdc 12 read, 340 write"},
	}, status.Alerts())
}

func TestPoolStatus_Alerts_States(t *testing.T) {
	tests := []struct {
		state    string
		expected []PoolAlert
	}{
		{PoolStateOnline, nil},
		{"", nil},
		{PoolStateDegraded, []PoolAlert{{Pool: "tank", Severity: PoolAlertWarning, Kind: PoolAlertState, Message: "Pool is DEGRADED"}}},
		{PoolStateSuspended, []PoolAlert{{Pool: "tank", Severity: PoolAlertError, Kind: PoolAlertState, Message: "Pool is SUSPENDED"}}},
		{PoolStateFaulted, []PoolAlert{{Pool: "tank", Severity: PoolAlertError, Kind: PoolAlertState, Message: "Pool is FAULTED"}}},
	}
	for _, test := range tests {
		t.Run(test.state, func(t *testing.T) {
			status := &PoolStatus{Name: "tank", State: test.state, Errors: noDataErrors}
			assert.Equal(t, test.expected, status.Alerts())
		})
	}
}

func TestPoolStatus_Alerts_Devices(t *testing.T) {
	status := &PoolStatus{Name: "tank", State: PoolStateOnline, Devices: []*PoolDevice{
		// the pool itself sums up the errors of its devices
		{Name: "tank", Depth: 0, State: PoolStateOnline, Checksum: 7},
		{Name: "mirror-0", Depth: 1, State: PoolStateOnline},
		{Name: "/dev/disk/by-id/ata-DISK_SERIAL0001", Depth: 2, State: PoolStateOnline, Checksum: 7},
		// taken offline on purpose
		{Name: "sdb", Depth: 2, State: PoolStateOffline},
		{Name: "sdc", Depth: 2, State: PoolStateRemoved},
		{Name: "logs"},
	}}

	assert.Equal(t, []PoolAlert{
		{Pool: "tank", Severity: PoolAlertError, Kind: PoolAlertDeviceState, Message: "Devices not available: sdc REMOVED"},
		{Pool: "tank", Severity: PoolAlertWarning, Kind: PoolAlertDeviceErrors, Message: "Device errors: ata-DISK_SERIAL0001 7 checksum"},
	}, status.Alerts())
}

func TestPoolStatus_DataErrors(t *testing.T) {
	tests := map[string]string{
		"":                                     "",
		"No known data errors":                 "",
		"3 data errors, use '-v' for a list":   "3 data errors",
		"Permanent errors have been detected.": "Permanent errors have been detected.",
		// with -v, the files follow in the next lines
		"Permanent errors have been detected in the following files:\n/tank/file": "Permanent errors have been detected in the following files:",
	}
	for errors, expected := range tests {
		assert.Equal(t, expected, (&PoolStatus{Errors: errors}).DataErrors(), errors)
	}
}

func TestFirstSentence(t *testing.T) {
	assert.Equal(t, "One or more devices has experienced an error resulting in data corruption.",
		firstSentence("One or more devices has experienced an error resulting in data\ncorruption.  Applications may be affected."))
	assert.Equal(t, "No dot", firstSentence("No dot"))
	assert.Empty(t, firstSentence(""))
}

func TestPoolStatus_Alerts_Spares(t *testing.T) {
	status := &PoolStatus{Name: "tank", State: PoolStateOnline, Devices: []*PoolDevice{
		{Name: "tank", Depth: 0, State: PoolStateOnline},
		{Name: "sda", Depth: 1, State: PoolStateOnline},
		{Name: "spares"},
		{Name: "sdf", Depth: 1, State: PoolStateUnavail, Note: "cannot open"},
		{Name: "logs"},
		{Name: "nvme0n1", Depth: 1, State: PoolStateFaulted},
	}}

	// Regression: a spare that cannot be used was an error, though no data is at risk
	assert.Equal(t, []PoolAlert{
		{Pool: "tank", Severity: PoolAlertError, Kind: PoolAlertDeviceState, Message: "Devices not available: nvme0n1 FAULTED"},
		{Pool: "tank", Severity: PoolAlertWarning, Kind: PoolAlertSpareState, Message: "Spares not available: sdf UNAVAIL (cannot open)"},
	}, status.Alerts())
}
