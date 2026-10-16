package disk_browser

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"
	"zfs-file-history/internal/disk"
	"zfs-file-history/internal/testutil"
	"zfs-file-history/internal/ui/table"
	"zfs-file-history/internal/ui/theme"
	"zfs-file-history/internal/ui/txwidgets"
	uiutil "zfs-file-history/internal/ui/util"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
			{Id: 5, Name: "Reallocated_Sector_Ct", Value: "8", Raw: 8, Normalized: 199, Worst: 199, Threshold: 140, Important: true,
				Used: 1.0 / 60, UsedKnown: true},
			{Id: 9, Name: "Power_On_Hours", Value: "8760", Raw: 8760, Normalized: 52, Worst: 52, Threshold: 0},
			{Id: 194, Name: "Temperature_Celsius", Value: "35 (Min/Max 20/45)", Raw: 35, Normalized: 115, Worst: 100, Threshold: 0},
			{Id: 190, Name: "Airflow_Temperature_Cel", Value: "62", Raw: 62, Normalized: 38, Worst: 30, Threshold: 40, Failed: "now",
				Used: 1, UsedKnown: true},
		},
	}
}

// detailsLines returns the lines of the details of the entry without style tags.
func detailsLines(entry *DiskEntry) []string {
	return strings.Split(testutil.StripTags(strings.Join(formatDetails(entry, 0), "\n")), "\n")
}

func TestFormatDetails(t *testing.T) {
	report := ataReport()
	report.Counters = disk.SmartCounters{WearPercent: -1, BytesWritten: 43_915_542_390_272}
	entry := &DiskEntry{Devices: []PoolDevice{{Pool: "tank", Device: "/dev/disk/by-id/ata-DISK_1-part1"}}, Report: report,
		Reading: disk.Reading{IsDisk: true, Known: true, Temperature: disk.Temperature{Celsius: 40, Critical: 60}}}
	assert.Equal(t, "sda · WDC WD40EFRX", detailsTitle(entry))
	assert.Equal(t, []string{
		"● 8 reallocated sectors · SATA HDD in tank",
		"",
		"Disk                                  Health",
		"Type      SATA HDD                    Temp      █████▏       40°C · critical at 60°C",
		"Pools     tank  ata-DISK_1-part1      Check     PASSED",
		"Serial    WD-1                        Age       1.0 years · 8,760 h",
		"Firmware  82.00A82                    Written   44 TB",
		"Size      4.0 TB                      Problems  8 reallocated sectors",
		"                                      Read      " + uiutil.FormatTime(readAt),
	}, detailsLines(entry), "what it is next to its health, the attributes are in their own table")
	// the verdict in the color of the problem
	assert.True(t, strings.HasPrefix(formatDetails(entry, 0)[0], txwidgets.ColorTag(uiutil.InfoWarning.Color())+"●"))

	tests := []struct {
		name    string
		report  disk.SmartReport
		verdict string
		health  string
	}{
		{"asleep", disk.SmartReport{Disk: "sdb", Standby: true, CheckedAt: readAt},
			"● health unknown: asleep · SSD in tank", "SMART  asleep, not woken up to read it"},
		{"needs root", disk.SmartReport{Disk: "sdb", Err: disk.ErrSmartNeedsRoot},
			"● health unknown: SMART needs root · SSD in tank", "SMART  only root may read them: allow \"Disk health\" in the command menu (:)"},
		{"no smartctl", disk.SmartReport{Disk: "sdb", Err: disk.ErrSmartctlMissing},
			"● health unknown · SSD in tank", "SMART  not read: smartctl is not installed (smartmontools)"},
		{"other error", disk.SmartReport{Disk: "sdb", Err: errors.New("smartctl failed: no")},
			"● health unknown · SSD in tank", "Error  smartctl failed: no"},
		{"failed, no hours", disk.SmartReport{Disk: "sdb", ReadAt: readAt, HealthKnown: true, PowerOnHours: -1, Counters: disk.SmartCounters{WearPercent: -1, BytesWritten: -1}},
			"● health check failed · SSD in tank", "Check  FAILED"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			entry := &DiskEntry{Devices: []PoolDevice{{Pool: "tank", Device: "sdb"}}, Report: test.report}
			lines := detailsLines(entry)
			assert.Equal(t, test.verdict, lines[0])
			assert.True(t, slices.ContainsFunc(lines, func(line string) bool { return strings.HasSuffix(line, test.health) }),
				"%q in %q", test.health, lines)
		})
	}
}

// Where both sections do not fit side by side, the health is below what the disk is, the labels of both aligned.
func TestFormatDetails_Stacked(t *testing.T) {
	report := ataReport()
	report.Counters = disk.SmartCounters{WearPercent: -1, BytesWritten: 43_915_542_390_272}
	entry := &DiskEntry{Devices: []PoolDevice{{Pool: "tank", Device: "/dev/disk/by-id/ata-DISK_1-part1"}}, Report: report,
		Reading: disk.Reading{IsDisk: true, Known: true, Temperature: disk.Temperature{Celsius: 40, Critical: 60}}}

	sideBySide := formatDetails(entry, 0)
	assert.Equal(t, sideBySide, formatDetails(entry, 90), "both fit")

	lines := formatDetails(entry, 50)
	for _, line := range lines {
		assert.LessOrEqual(t, tview.TaggedStringWidth(line), 50, testutil.StripTags(line))
	}
	assert.Equal(t, []string{
		"● 8 reallocated sectors · SATA HDD in tank",
		"",
		"Disk",
		"Type      SATA HDD",
		"Pools     tank  ata-DISK_1-part1",
		"Serial    WD-1",
		"Firmware  82.00A82",
		"Size      4.0 TB",
		"",
		"Health",
		"Temp      █████▏       40°C · critical at 60°C",
		"Check     PASSED",
		"Age       1.0 years · 8,760 h",
		"Written   44 TB",
		"Problems  8 reallocated sectors",
		"Read      " + uiutil.FormatTime(readAt),
	}, strings.Split(testutil.StripTags(strings.Join(lines, "\n")), "\n"))

	// narrower, the values are wrapped next to their labels
	for _, line := range formatDetails(entry, 40) {
		assert.LessOrEqual(t, tview.TaggedStringWidth(line), 40, testutil.StripTags(line))
	}
	assert.Contains(t, strings.Split(testutil.StripTags(strings.Join(formatDetails(entry, 40), "\n")), "\n"),
		"          at 60°C")
}

// The names of the pools can be clicked where they are shown: side by side, below each other, and below their label in
// a narrow pane.
func TestFormatDetails_PoolLinks(t *testing.T) {
	entry := &DiskEntry{Devices: []PoolDevice{
		{Pool: "bpool", Device: "/dev/disk/by-id/ata-DISK_1-part2"},
		{Pool: "rpool", Device: "/dev/disk/by-id/ata-DISK_1-part3"},
	}, Report: ataReport()}
	for _, width := range []int{0, 90, 50, 30, 16} {
		t.Run(fmt.Sprint(width), func(t *testing.T) {
			lines, links := formatDetailsWithPoolLinks(entry, width)
			require.Len(t, links, 2)
			for i, link := range links {
				line := []rune(testutil.StripTags(lines[link.line]))
				require.LessOrEqual(t, link.column+link.width, len(line), string(line))
				assert.Equal(t, entry.Devices[i].Pool, string(line[link.column:link.column+link.width]), string(line))
			}
			// a wrapped device name moves the next one down
			assert.Greater(t, links[1].line, links[0].line, "one below the other")
		})
	}
}

func TestFormatUsed(t *testing.T) {
	assert.Equal(t, "", formatUsed(disk.SmartAttribute{Used: 0.5}), "not known: cannot fail")
	assert.Equal(t, "           0%", testutil.StripTags(formatUsed(disk.SmartAttribute{UsedKnown: true})), "an empty bar")
	assert.Equal(t, "████      50%", testutil.StripTags(formatUsed(disk.SmartAttribute{Used: 0.5, UsedKnown: true})))
	assert.Equal(t, "████████ 100%", testutil.StripTags(formatUsed(disk.SmartAttribute{Used: 1, UsedKnown: true})))
}

func TestFormatDetailsTemperature(t *testing.T) {
	reading := func(celsius float64, critical float64) disk.Reading {
		return disk.Reading{IsDisk: true, Known: true, Temperature: disk.Temperature{Celsius: celsius, Critical: critical}}
	}
	text := func(reading disk.Reading) string {
		// without the bar
		_, after, _ := strings.Cut(testutil.StripTags(formatDetailsTemperature(reading)), " ")
		return strings.TrimSpace(after)
	}
	assert.Equal(t, "40°C · critical at 60°C", text(reading(40, 60)), "the limit, not a target")
	assert.Equal(t, "62°C · 2°C above critical (60°C)", text(reading(62, 60)))
	assert.Contains(t, formatDetailsTemperature(reading(62, 60)), txwidgets.ColorTag(uiutil.InfoError.Color())+" · 2°C above")
	assert.Equal(t, "40°C", text(reading(40, 0)), "no critical temperature known")
}

func TestFormatDetails_Ssd(t *testing.T) {
	report := healthy("nvme0n1")
	report.Protocol = "NVMe"
	report.Counters = disk.SmartCounters{WearPercent: 4, BytesWritten: -1}
	entry := &DiskEntry{Devices: []PoolDevice{{Pool: "bpool", Device: "nvme0n1p2"}, {Pool: "rpool", Device: "nvme0n1p3"}}, Report: report}
	lines := detailsLines(entry)
	assert.Equal(t, "● healthy · NVMe SSD in bpool, rpool", lines[0])
	assert.True(t, strings.HasPrefix(formatDetails(entry, 0)[0], txwidgets.ColorTag(uiutil.InfoOk.Color())+"●"))
	text := strings.Join(lines, "\n")
	assert.Contains(t, text, "Pools  bpool  nvme0n1p2")
	assert.Contains(t, text, "       rpool  nvme0n1p3")
	assert.Contains(t, text, "Wear   ▌            4% of its rated endurance")
	assert.NotContains(t, text, "Temp", "no temperature read")
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
