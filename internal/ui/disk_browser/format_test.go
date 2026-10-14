package disk_browser

import (
	"errors"
	"slices"
	"strings"
	"testing"
	"time"
	"zfs-file-history/internal/disk"
	"zfs-file-history/internal/testutil"
	"zfs-file-history/internal/ui/table"
	"zfs-file-history/internal/ui/theme"
	uiutil "zfs-file-history/internal/ui/util"

	"github.com/gdamore/tcell/v2"
	"github.com/stretchr/testify/assert"
)

var readAt = time.Date(2026, 10, 14, 12, 0, 0, 0, time.Local)

func healthy(name string) disk.SmartReport {
	return disk.SmartReport{Disk: name, ReadAt: readAt, CheckedAt: readAt, HealthKnown: true, Passed: true, PowerOnHours: 100}
}

func withProblems(name string, problems ...disk.SmartProblem) disk.SmartReport {
	report := healthy(name)
	report.Problems = problems
	return report
}

var reallocated = disk.SmartProblem{Severity: disk.SmartWarning, Short: "8 realloc", Text: "8 reallocated sectors"}
var failed = disk.SmartProblem{Severity: disk.SmartError, Short: "FAILED", Text: "the health check of the disk failed"}

// ataReport is a report of an ATA disk with a problem and a failing attribute.
func ataReport() disk.SmartReport {
	return disk.SmartReport{
		Disk: "sda", ReadAt: readAt, CheckedAt: readAt, Model: "WDC WD40EFRX", Serial: "WD-1", Firmware: "82.00A82", Capacity: 4000787030016, Protocol: "ATA",
		Rotational: true, HealthKnown: true, Passed: true, PowerOnHours: 8760,
		Problems: []disk.SmartProblem{reallocated},
		Attributes: []disk.SmartAttribute{
			{Id: 5, Name: "Reallocated_Sector_Ct", Value: "8", Raw: 8, Normalized: 199, Worst: 199, Threshold: 140, Important: true},
			{Id: 9, Name: "Power_On_Hours", Value: "8760", Raw: 8760, Normalized: 52, Worst: 52, Threshold: 0},
			{Id: 194, Name: "Temperature_Celsius", Value: "35 (Min/Max 20/45)", Raw: 35, Normalized: 115, Worst: 100, Threshold: 0},
			{Id: 190, Name: "Airflow_Temperature_Cel", Value: "62", Raw: 62, Normalized: 38, Worst: 30, Threshold: 40, Failed: "now"},
		},
	}
}

func TestFormatDetails(t *testing.T) {
	entry := &DiskEntry{Devices: []PoolDevice{{Pool: "tank", Device: "/dev/disk/by-id/ata-DISK_1-part1"}}, Report: ataReport()}
	assert.Equal(t, []string{
		"sda  SATA HDD  in tank",
		"  Devices:  ata-DISK_1-part1  in tank",
		"  Disk:     WDC WD40EFRX  serial WD-1 · 4.0 TB · firmware 82.00A82",
		"  Health:   PASSED",
		"  Power on: 8,760 h (1.0 years)",
		"  Read:     " + uiutil.FormatTime(readAt),
		"  Problems: 8 reallocated sectors",
	}, strings.Split(testutil.StripTags(strings.Join(formatDetails(entry), "\n")), "\n"), "the attributes are in their own table")

	tests := []struct {
		name   string
		report disk.SmartReport
		lines  []string
	}{
		{"asleep", disk.SmartReport{Disk: "sdb", Standby: true, CheckedAt: readAt},
			[]string{"sdb  SSD  in tank", "  SMART:    asleep, not woken up to read it"}},
		{"needs root", disk.SmartReport{Disk: "sdb", Err: disk.ErrSmartNeedsRoot},
			[]string{"sdb  SSD  in tank", "  SMART:    only root may read them: allow \"Disk health\" in the command menu (:)"}},
		{"no smartctl", disk.SmartReport{Disk: "sdb", Err: disk.ErrSmartctlMissing},
			[]string{"sdb  SSD  in tank", "  SMART:    not read: smartctl is not installed (smartmontools)"}},
		{"other error", disk.SmartReport{Disk: "sdb", Err: errors.New("smartctl failed: no")},
			[]string{"sdb  SSD  in tank", "  Error:    smartctl failed: no"}},
		{"failed, no hours", disk.SmartReport{Disk: "sdb", ReadAt: readAt, HealthKnown: true, PowerOnHours: -1},
			[]string{"sdb  SSD  in tank", "  Health:   FAILED", "  Read:     " + uiutil.FormatTime(readAt)}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			entry := &DiskEntry{Devices: []PoolDevice{{Pool: "tank", Device: "sdb"}}, Report: test.report}
			assert.Equal(t, test.lines, strings.Split(testutil.StripTags(strings.Join(formatDetails(entry), "\n")), "\n"))
		})
	}
}

func TestSmartSummary(t *testing.T) {
	asleepAfterRead := healthy("sda")
	asleepAfterRead.Standby = true
	tests := []struct {
		name    string
		reports []disk.SmartReport
		text    string
		color   tcell.Color
	}{
		{"not on a disk", nil, "", tcell.ColorDefault},
		{"healthy", []disk.SmartReport{healthy("sda")}, "OK", theme.Colors.Pool.Online},
		{"warning", []disk.SmartReport{withProblems("sda", reallocated)}, "8 realloc", theme.Colors.Pool.Degraded},
		{"the worst of several disks", []disk.SmartReport{withProblems("sda", reallocated), withProblems("sdb", failed)}, "FAILED", theme.Colors.Pool.Faulted},
		{"needs root", []disk.SmartReport{{Disk: "sda", Err: disk.ErrSmartNeedsRoot}}, "?", theme.Colors.Pool.Hint},
		{"other error", []disk.SmartReport{{Disk: "sda", Err: errors.New("no")}}, "error", theme.Colors.Pool.Hint},
		{"asleep, never read", []disk.SmartReport{{Disk: "sda", Standby: true}}, "asleep", theme.Colors.Pool.Hint},
		{"asleep, read before", []disk.SmartReport{asleepAfterRead}, "OK", theme.Colors.Pool.Online},
		{"no smartctl", []disk.SmartReport{{Disk: "sda", Err: disk.ErrSmartctlMissing}}, "", tcell.ColorDefault},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			text, color := SmartSummary(test.reports)
			assert.Equal(t, test.text, text)
			assert.Equal(t, test.color, color)
		})
	}
}

func TestFormatTemperature(t *testing.T) {
	text, color := FormatTemperature(disk.Reading{IsDisk: true, Known: true, Temperature: disk.Temperature{Celsius: 20}})
	assert.Equal(t, "20°C", text)
	assert.Equal(t, theme.Colors.Pool.Temperature[0].Color, color)

	_, color = FormatTemperature(disk.Reading{IsDisk: true, Known: true, Temperature: disk.Temperature{Celsius: 61}})
	assert.Equal(t, theme.Colors.Pool.Temperature[len(theme.Colors.Pool.Temperature)-1].Color, color)

	_, color = FormatTemperature(disk.Reading{IsDisk: true, Known: true, Idle: true, Temperature: disk.Temperature{Celsius: 61}})
	assert.Equal(t, theme.Colors.Pool.TemperatureIdle, color, "may be outdated")

	text, color = FormatTemperature(disk.Reading{IsDisk: true, Idle: true})
	assert.Equal(t, ReadTemperatureIcon, text)
	assert.Equal(t, theme.Colors.Pool.ReadTemperature, color)

	text, _ = FormatTemperature(disk.Reading{})
	assert.Empty(t, text)
}

func TestTemperaturePosition(t *testing.T) {
	tests := []struct {
		name        string
		temperature disk.Temperature
		expected    float64
	}{
		{"cool", disk.Temperature{Celsius: 20}, 0},
		{"default warning", disk.Temperature{Celsius: 42.5}, 0.5},
		{"warning of the disk", disk.Temperature{Celsius: 55, Warning: 85}, 0.5},
		{"critical without warning", disk.Temperature{Celsius: 55, Critical: 85}, 0.5},
		{"implausible warning", disk.Temperature{Celsius: 42.5, Warning: 10}, 0.5},
		{"too hot", disk.Temperature{Celsius: 90, Warning: 85}, 1},
		// Regression: Seagate IronWolf Pro (drivetemp) report a warning of 40 °C, everything above was red
		{"critical before warning", disk.Temperature{Celsius: 50, Warning: 40, Critical: 60}, 25.0 / 35},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assert.InDelta(t, test.expected, temperaturePosition(test.temperature), 0.001)
		})
	}
}

func TestTypeOf(t *testing.T) {
	tests := []struct {
		protocol   string
		rotational bool
		expected   string
	}{
		{"ATA", true, "SATA HDD"},
		{"ATA", false, "SATA SSD"},
		{"NVMe", false, "NVMe SSD"},
		// the kernel reports unknown ones as rotational
		{"NVMe", true, "NVMe SSD"},
		{"SCSI", true, "SAS HDD"},
		// not read
		{"", true, "HDD"},
	}
	for _, test := range tests {
		assert.Equal(t, test.expected, typeOf(disk.SmartReport{Protocol: test.protocol, Rotational: test.rotational}))
	}
}

func TestFormatCounter(t *testing.T) {
	report := healthy("sda")
	report.Counters = disk.SmartCounters{BadSectors: 8, Uncorrectable: 0, CrcErrors: 1234, WearPercent: 93,
		BytesWritten: 43_915_542_390_272, PowerCycles: 21, UnsafeShutdowns: -1, LoadCycles: 48284}
	tests := []struct {
		column *table.Column
		text   string
		color  tcell.Color
	}{
		{columnBad, "8", theme.Colors.Pool.Degraded},
		{columnUncorrectable, "0", theme.Colors.Layout.Table.ZeroSize},
		{columnCrc, "1,234", theme.Colors.Pool.Degraded},
		{columnWear, "93%", theme.Colors.Pool.Degraded},
		{columnWritten, "44 TB", tcell.ColorLightGray},
		{columnPowerCycles, "21", tcell.ColorLightGray},
		{columnUnsafe, "", tcell.ColorDefault},
		{columnLoadCycles, "48,284", tcell.ColorLightGray},
	}
	for _, test := range tests {
		text, color := formatCounter(test.column, report)
		assert.Equal(t, test.text, text, test.column.Title)
		assert.Equal(t, test.color, color, test.column.Title)
	}

	report.ReadAt = time.Time{}
	text, _ := formatCounter(columnBad, report)
	assert.Empty(t, text, "not read")
	assert.Equal(t, int64(-1), counterOf(columnBad, report), "sorted first")
}

func TestCompareDiskNames(t *testing.T) {
	names := []string{"sdaa", "nvme10n1", "sdb", "nvme2n1", "sda", "sdz", "nvme0n1", "vda"}
	slices.SortFunc(names, compareDiskNames)
	assert.Equal(t, []string{"nvme0n1", "nvme2n1", "nvme10n1", "sda", "sdb", "sdz", "sdaa", "vda"}, names)
}
