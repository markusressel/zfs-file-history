package pool_activity

import (
	"testing"
	"time"
	"zfs-file-history/internal/disk"
	"zfs-file-history/internal/disk/disktest"
	"zfs-file-history/internal/testutil"
	"zfs-file-history/internal/zfs"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// withDisks returns the status of a healthy pool with a mirror of the disks.
func withDisks(name string, disks ...string) *zfs.PoolStatus {
	status := idle(name)
	status.Devices = []*zfs.PoolDevice{
		{Name: name, State: zfs.PoolStateOnline},
		{Name: "mirror-0", Depth: 1, State: zfs.PoolStateOnline},
	}
	for _, name := range disks {
		status.Devices = append(status.Devices, &zfs.PoolDevice{Name: name, Depth: 2, State: zfs.PoolStateOnline})
	}
	return status
}

func critical(celsius float64) disk.Reading {
	return disk.Reading{IsDisk: true, Known: true, Temperature: disk.Temperature{Celsius: celsius, Warning: 70, Critical: 85}}
}

func TestTemperatureAlert(t *testing.T) {
	status := withDisks("tank", "/dev/disk/by-id/ata-DISK_1", "sdb", "sdc", "sdd")
	readings := map[string]disk.Reading{
		"/dev/disk/by-id/ata-DISK_1": critical(85),
		"sdb":                        critical(84),
		"sdc":                        critical(91.4),
		// outdated
		"sdd": {IsDisk: true, Known: true, Idle: true, Temperature: disk.Temperature{Celsius: 90, Critical: 85}},
	}
	alert, ok := temperatureAlert(status, readings)
	require.True(t, ok)
	assert.Equal(t, zfs.PoolAlert{Pool: "tank", Severity: zfs.PoolAlertError, Kind: zfs.PoolAlertTemperature,
		Message: "Critical temperature: ata-DISK_1 85°C (critical: 85°C); sdc 91°C (critical: 85°C)"}, alert)

	_, ok = temperatureAlert(status, map[string]disk.Reading{"sdb": critical(84)})
	assert.False(t, ok)
}

func TestMonitor_TemperatureAlerts(t *testing.T) {
	degraded := withDisks("bpool", "sda")
	degraded.State = zfs.PoolStateDegraded
	app, monitor, _, fake, _ := setupAlertTest(t, []*zfs.PoolStatus{degraded, withDisks("tank", "sdb", "sdc")})
	sampler := disktest.NewSampler(map[string]disk.Reading{"sdb": critical(86), "sdc": critical(40)})
	testutil.OnUiThread(t, app, func() {
		monitor.SetTemperatureSampler(sampler)
		monitor.Refresh()
	})

	alerts := func() []zfs.PoolAlert {
		var alerts []zfs.PoolAlert
		testutil.OnUiThread(t, app, func() { alerts = monitor.Alerts() })
		return alerts
	}
	require.Eventually(t, func() bool { return len(alerts()) == 2 }, 2*time.Second, 10*time.Millisecond)
	assert.Equal(t, []zfs.PoolAlert{
		{Pool: "tank", Severity: zfs.PoolAlertError, Kind: zfs.PoolAlertTemperature, Message: "Critical temperature: sdb 86°C (critical: 85°C)"},
		{Pool: "bpool", Severity: zfs.PoolAlertWarning, Kind: zfs.PoolAlertState, Message: "Pool is DEGRADED"},
	}, alerts(), "the most severe first")

	// cooled down: gone with the next load
	sampler.Set(map[string]disk.Reading{"sdb": critical(80)})
	fake.set([]*zfs.PoolStatus{degraded, withDisks("tank", "sdb", "sdc")}, nil)
	testutil.OnUiThread(t, app, func() { monitor.Refresh() })
	require.Eventually(t, func() bool { return len(alerts()) == 1 }, 2*time.Second, 10*time.Millisecond)
	assert.Equal(t, zfs.PoolAlertState, alerts()[0].Kind)
}

func smartReport(problems ...disk.SmartProblem) []disk.SmartReport {
	return []disk.SmartReport{{Disk: "sdx", ReadAt: time.Now(), HealthKnown: true, Passed: true, Problems: problems}}
}

var (
	smartWarning = disk.SmartProblem{Severity: disk.SmartWarning, Short: "8 realloc", Text: "8 reallocated sectors"}
	smartError   = disk.SmartProblem{Severity: disk.SmartError, Short: "FAILED", Text: "the health check of the disk failed"}
)

func TestSmartAlert(t *testing.T) {
	status := withDisks("tank", "/dev/disk/by-id/ata-DISK_1", "sdb", "sdc")

	alert, ok := smartAlert(status, map[string][]disk.SmartReport{
		"/dev/disk/by-id/ata-DISK_1": smartReport(smartWarning),
		"sdb":                        smartReport(),
	})
	require.True(t, ok)
	assert.Equal(t, zfs.PoolAlert{Pool: "tank", Severity: zfs.PoolAlertWarning, Kind: zfs.PoolAlertSmart,
		Message: "SMART: ata-DISK_1 8 reallocated sectors"}, alert)

	alert, ok = smartAlert(status, map[string][]disk.SmartReport{
		"/dev/disk/by-id/ata-DISK_1": smartReport(smartWarning),
		"sdc":                        smartReport(smartError, smartWarning),
	})
	require.True(t, ok)
	assert.Equal(t, zfs.PoolAlertError, alert.Severity, "the most severe problem decides")
	assert.Equal(t, "SMART: ata-DISK_1 8 reallocated sectors; sdc the health check of the disk failed", alert.Message)

	_, ok = smartAlert(status, map[string][]disk.SmartReport{"sdb": smartReport(), "sdc": {{Disk: "sdc", Err: disk.ErrSmartNeedsRoot}}})
	assert.False(t, ok, "healthy, or not readable")
}

func TestMonitor_SmartAlerts(t *testing.T) {
	originalInterval := smartInterval
	t.Cleanup(func() { smartInterval = originalInterval })
	smartInterval = time.Hour

	app, monitor, _, _, _ := setupAlertTest(t, []*zfs.PoolStatus{withDisks("tank", "sda", "sdb")})
	smart := disktest.NewSmart(map[string][]disk.SmartReport{"sdb": smartReport(smartError)})
	testutil.OnUiThread(t, app, func() {
		monitor.SetSmartSource(smart)
		monitor.Refresh()
	})

	alerts := func() []zfs.PoolAlert {
		var alerts []zfs.PoolAlert
		testutil.OnUiThread(t, app, func() { alerts = monitor.Alerts() })
		return alerts
	}
	require.Eventually(t, func() bool { return len(alerts()) == 1 }, 2*time.Second, 10*time.Millisecond)
	assert.Equal(t, zfs.PoolAlert{Pool: "tank", Severity: zfs.PoolAlertError, Kind: zfs.PoolAlertSmart,
		Message: "SMART: sdb the health check of the disk failed"}, alerts()[0])

	// not read again with every load of the statuses: sudo logs every call
	testutil.OnUiThread(t, app, func() { monitor.Refresh() })
	testutil.OnUiThread(t, app, func() { monitor.Refresh() })
	time.Sleep(50 * time.Millisecond)
	assert.Len(t, smart.MaxAges(), 1)
	assert.Zero(t, smart.Resets())

	// e.g. once allowed to run smartctl: read again, without the old reports
	testutil.OnUiThread(t, app, func() { monitor.RefreshSmart() })
	require.Eventually(t, func() bool { return len(smart.MaxAges()) == 2 }, 2*time.Second, 10*time.Millisecond)
	assert.Equal(t, 1, smart.Resets())
}
