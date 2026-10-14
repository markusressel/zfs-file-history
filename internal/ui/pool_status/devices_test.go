package pool_status

import (
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"
	"zfs-file-history/internal/disk"
	"zfs-file-history/internal/disk/disktest"
	"zfs-file-history/internal/testutil"
	"zfs-file-history/internal/ui/theme"
	"zfs-file-history/internal/zfs"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// resilveringDevices are the devices of a degraded pool with a resilver running, with logs, a cache and spares.
func resilveringDevices() []*zfs.PoolDevice {
	return []*zfs.PoolDevice{
		{Name: "tank", Depth: 0, State: zfs.PoolStateDegraded},
		{Name: "raidz2-0", Depth: 1, State: zfs.PoolStateDegraded},
		{Name: "sda", Depth: 2, State: zfs.PoolStateOnline},
		{Name: "replacing-1", Depth: 2, State: zfs.PoolStateDegraded},
		{Name: "sdc", Depth: 3, State: zfs.PoolStateFaulted, Read: 12, Write: 340, Note: "too many errors"},
		{Name: "sde", Depth: 3, State: zfs.PoolStateOnline, Note: "(resilvering)"},
		{Name: "sdb", Depth: 2, State: zfs.PoolStateOnline, Checksum: 2},
		{Name: "logs", Depth: 0},
		{Name: "nvme0n1p1", Depth: 1, State: zfs.PoolStateOnline},
		{Name: "spares", Depth: 0},
		{Name: "sdf", Depth: 1, State: zfs.PoolStateAvail},
		{Name: "sdg", Depth: 1, State: zfs.PoolStateInUse, Note: "currently in use"},
	}
}

func TestDevicePrefixes(t *testing.T) {
	assert.Equal(t, []string{
		"",
		"└─ ",
		"   ├─ ",
		"   ├─ ",
		"   │  ├─ ",
		"   │  └─ ",
		"   └─ ",
		"",
		"└─ ",
		"",
		"├─ ",
		"└─ ",
	}, devicePrefixes(resilveringDevices()))

	assert.Empty(t, devicePrefixes(nil))
}

func TestDevicePrefixes_SkippedLevel(t *testing.T) {
	// does not happen, but must not crash
	devices := []*zfs.PoolDevice{{Name: "tank"}, {Name: "deep", Depth: 2}, {Name: "sda", Depth: 1}}
	assert.Equal(t, []string{"", "   └─ ", "└─ "}, devicePrefixes(devices))
}

func TestIsLastSibling(t *testing.T) {
	devices := resilveringDevices()
	assert.False(t, isLastSibling(devices, 0), "the groups follow the pool")
	assert.True(t, isLastSibling(devices, 1))
	assert.False(t, isLastSibling(devices, 3), "sdb follows the children of replacing-1")
	assert.True(t, isLastSibling(devices, 6))
	assert.True(t, isLastSibling(devices, 11))
}

func TestFormatDevices(t *testing.T) {
	text, fields := formatDevices(resilveringDevices(), nil, nil)
	text = testutil.StripTags(text)
	assert.Empty(t, fields)

	lines := strings.Split(strings.TrimSuffix(text, "\n"), "\n")
	assert.Equal(t, []string{
		" NAME               STATE      READ WRITE CKSUM",
		" tank               DEGRADED      0     0     0",
		" └─ raidz2-0        DEGRADED      0     0     0",
		"    ├─ sda          ONLINE        0     0     0",
		"    ├─ replacing-1  DEGRADED      0     0     0",
		"    │  ├─ sdc       FAULTED      12   340     0  too many errors",
		"    │  └─ sde       ONLINE        0     0     0  (resilvering)",
		"    └─ sdb          ONLINE        0     0     2",
		" logs",
		" └─ nvme0n1p1       ONLINE        0     0     0",
		" spares",
		" ├─ sdf             AVAIL",
		" └─ sdg             INUSE                        currently in use",
	}, lines)
}

func TestFormatDevices_WideCounters(t *testing.T) {
	devices := []*zfs.PoolDevice{{Name: "tank", State: zfs.PoolStateOnline, Checksum: 1234567}}
	text, _ := formatDevices(devices, nil, nil)
	lines := strings.Split(testutil.StripTags(text), "\n")
	assert.Equal(t, " NAME  STATE      READ   WRITE   CKSUM", lines[0])
	assert.Equal(t, " tank  ONLINE        0       0 1234567", lines[1])
}

func TestDevicesTitle(t *testing.T) {
	assert.Equal(t, "Devices", devicesTitle(nil))
	assert.Equal(t, "Devices · 2 with errors", devicesTitle(resilveringDevices()))
}

func TestCounterAndNoteColors(t *testing.T) {
	assert.Equal(t, theme.Colors.Pool.NoErrors, counterColor(0))
	assert.Equal(t, theme.Colors.Pool.Errors, counterColor(1))

	assert.Equal(t, theme.Colors.Pool.Degraded, noteColor(&zfs.PoolDevice{State: zfs.PoolStateOnline, Note: "(resilvering)"}))
	assert.Equal(t, theme.Colors.Pool.Faulted, noteColor(&zfs.PoolDevice{State: zfs.PoolStateFaulted, Note: "too many errors"}))
	assert.Equal(t, tcell.ColorWhite, noteColor(&zfs.PoolDevice{State: "OTHER", Note: "?"}))
}

func TestPoolDevices_SetStatus(t *testing.T) {
	app := tview.NewApplication()
	devices := NewPoolDevices(app, disktest.NewSampler(nil), nil)

	devices.SetStatus(&zfs.PoolStatus{Name: "tank", Devices: resilveringDevices()})
	assert.Contains(t, devices.GetLayout().GetTitle(), "Devices · 2 with errors")
	assert.Contains(t, devices.GetLayout().GetText(true), "replacing-1")

	// the scroll position is kept while refreshed
	devices.GetLayout().ScrollTo(3, 0)
	devices.SetStatus(&zfs.PoolStatus{Name: "tank", Devices: resilveringDevices()})
	row, _ := devices.GetLayout().GetScrollOffset()
	assert.Equal(t, 3, row)

	devices.SetStatus(nil)
	assert.Equal(t, " Devices ", devices.GetLayout().GetTitle())
	assert.Empty(t, devices.GetLayout().GetText(true))
}

func TestPoolDevices_Focus(t *testing.T) {
	app := tview.NewApplication()
	devices := NewPoolDevices(app, disktest.NewSampler(nil), nil)
	assert.False(t, devices.HasFocus())
	devices.Focus()
	assert.True(t, devices.HasFocus())
}

// testReadings are readings of the devices of resilveringDevices: a hot one, an idle one that was read, one that
// was not, one without sensor, and a spare that is not stored on a disk.
func testReadings() map[string]disk.Reading {
	return map[string]disk.Reading{
		"sda":       {IsDisk: true, Known: true, Temperature: disk.Temperature{Celsius: 35.4, Warning: 60}},
		"sdc":       {IsDisk: true, Idle: true},
		"sde":       {IsDisk: true, Idle: true, Known: true, Temperature: disk.Temperature{Celsius: 31}},
		"sdb":       {IsDisk: true, NoSensor: true, NeedsDrivetemp: true},
		"nvme0n1p1": {IsDisk: true, Known: true, Temperature: disk.Temperature{Celsius: 85, Warning: 84.85}},
		"sdf":       {},
		"sdg":       {IsDisk: true, Known: true, Temperature: disk.Temperature{Celsius: 102}},
	}
}

func TestFormatDevices_Temperatures(t *testing.T) {
	text, fields := formatDevices(resilveringDevices(), testReadings(), nil)
	lines := strings.Split(strings.TrimSuffix(testutil.StripTags(text), "\n"), "\n")
	assert.Equal(t, []string{
		" NAME               STATE      READ WRITE CKSUM   TEMP",
		" tank               DEGRADED      0     0     0",
		" └─ raidz2-0        DEGRADED      0     0     0",
		"    ├─ sda          ONLINE        0     0     0   35°C",
		"    ├─ replacing-1  DEGRADED      0     0     0",
		"    │  ├─ sdc       FAULTED      12   340     0      ⟳  too many errors",
		"    │  └─ sde       ONLINE        0     0     0   31°C  (resilvering)",
		"    └─ sdb          ONLINE        0     0     2      –",
		" logs",
		" └─ nvme0n1p1       ONLINE        0     0     0   85°C",
		" spares",
		" ├─ sdf             AVAIL",
		" └─ sdg             INUSE                        102°C  currently in use",
	}, lines)

	// the ones that can be read: not the one without sensor, nor the one not on a disk
	column := len(" NAME               STATE      READ WRITE CKSUM  ")
	assert.Equal(t, []clickableField{
		{device: "sda", width: temperatureWidth, line: 3, column: column},
		{device: "sdc", width: temperatureWidth, line: 5, column: column},
		{device: "sde", width: temperatureWidth, line: 6, column: column},
		{device: "nvme0n1p1", width: temperatureWidth, line: 9, column: column},
		{device: "sdg", width: temperatureWidth, line: 12, column: column},
	}, fields)
}

func TestFormatDevices_SpareWithoutTemperature(t *testing.T) {
	devices := []*zfs.PoolDevice{
		{Name: "tank", State: zfs.PoolStateOnline},
		{Name: "sda", Depth: 1, State: zfs.PoolStateOnline},
		{Name: "spares", Depth: 0},
		{Name: "sdf", Depth: 1, State: zfs.PoolStateAvail},
		{Name: "sdg", Depth: 1, State: zfs.PoolStateInUse, Note: "currently in use"},
	}
	readings := map[string]disk.Reading{"sda": {IsDisk: true, Known: true, Temperature: disk.Temperature{Celsius: 40}}}
	text, _ := formatDevices(devices, readings, nil)
	lines := strings.Split(strings.TrimSuffix(testutil.StripTags(text), "\n"), "\n")
	assert.Equal(t, []string{
		" NAME    STATE    READ WRITE CKSUM   TEMP",
		" tank    ONLINE      0     0     0",
		" └─ sda  ONLINE      0     0     0   40°C",
		" spares",
		" ├─ sdf  AVAIL",
		" └─ sdg  INUSE                             currently in use",
	}, lines)
}

// setupDevicesTest shows the devices of resilveringDevices with the readings of the sampler on a simulation screen.
func setupDevicesTest(t *testing.T, sampler *disktest.Sampler) (*tview.Application, *PoolDevicesComponent, tcell.SimulationScreen) {
	app := tview.NewApplication()
	screen := tcell.NewSimulationScreen("UTF-8")
	app.SetScreen(screen)
	app.EnableMouse(true)
	screen.SetSize(100, 20)
	devices := NewPoolDevices(app, sampler, nil)
	app.SetRoot(devices.GetLayout(), true)
	go func() { _ = app.Run() }()
	t.Cleanup(app.Stop)
	testutil.OnUiThread(t, app, func() { devices.SetStatus(&zfs.PoolStatus{Name: "tank", Devices: resilveringDevices()}) })
	// like the load of the status, which is drawn
	app.QueueUpdateDraw(func() {})
	return app, devices, screen
}

func TestPoolDevices_SamplesWhileDrawn(t *testing.T) {
	sampler := disktest.NewSampler(testReadings())
	app, devices, _ := setupDevicesTest(t, sampler)

	require.Eventually(t, func() bool {
		var text string
		testutil.OnUiThread(t, app, func() { text = devices.GetLayout().GetText(true) })
		return strings.Contains(text, "35°C")
	}, 2*time.Second, 10*time.Millisecond)
	// drawn again, but not due yet
	testutil.OnUiThread(t, app, func() { app.ForceDraw() })
	assert.Equal(t, [][]string{nil}, sampler.Forced(), "nothing forced")

	testutil.OnUiThread(t, app, func() {
		assert.Contains(t, devices.footer.GetText(), "idle, not read: t")
		assert.Contains(t, devices.footer.GetText(), "modprobe drivetemp")
	})
}

func TestPoolDevices_ClickReadsIdleDisk(t *testing.T) {
	sampler := disktest.NewSampler(testReadings())
	app, devices, screen := setupDevicesTest(t, sampler)
	require.Eventually(t, func() bool { return len(sampler.Forced()) == 1 }, 2*time.Second, 10*time.Millisecond)

	// the icon of sdc, below the border and the header
	var x, y int
	require.Eventually(t, func() bool {
		testutil.OnUiThread(t, app, func() {
			app.ForceDraw()
			x, y = -1, -1
			for _, field := range devices.fields {
				if field.device == "sdc" {
					x, y = 1+field.column+temperatureWidth-1, 1+field.line
				}
			}
		})
		return x >= 0
	}, 2*time.Second, 10*time.Millisecond)
	screen.InjectMouse(x, y, tcell.Button1, tcell.ModNone)
	screen.InjectMouse(x, y, tcell.ButtonNone, tcell.ModNone)

	require.Eventually(t, func() bool {
		return slices.ContainsFunc(sampler.Forced(), func(forced []string) bool { return slices.Equal(forced, []string{"sdc"}) })
	},
		2*time.Second, 10*time.Millisecond)
	require.Eventually(t, func() bool {
		var line string
		testutil.OnUiThread(t, app, func() { line = strings.Split(devices.GetLayout().GetText(true), "\n")[5] })
		return strings.Contains(line, "45°C")
	}, 2*time.Second, 10*time.Millisecond)
}

func TestPoolDevices_KeyReadsAll(t *testing.T) {
	sampler := disktest.NewSampler(testReadings())
	app, devices, screen := setupDevicesTest(t, sampler)
	require.Eventually(t, func() bool { return len(sampler.Forced()) == 1 }, 2*time.Second, 10*time.Millisecond)
	testutil.OnUiThread(t, app, func() {
		devices.Focus()
		require.Len(t, devices.GetShortcutMap(), 1)
	})

	screen.InjectKey(tcell.KeyRune, 't', tcell.ModNone)
	require.Eventually(t, func() bool { return len(sampler.Forced()) == 2 }, 2*time.Second, 10*time.Millisecond)
	assert.Equal(t, zfs.LeafDevices(resilveringDevices()), sampler.Forced()[1])
}

func TestPoolDevices_NoDisks(t *testing.T) {
	app := tview.NewApplication()
	devices := NewPoolDevices(app, disktest.NewSampler(nil), nil)
	devices.SetStatus(&zfs.PoolStatus{Name: "tank", Devices: resilveringDevices()})
	assert.Empty(t, devices.GetShortcutMap())
	assert.NotContains(t, devices.GetLayout().GetText(true), "TEMP")
}

func TestFormatDevices_CriticalBlinks(t *testing.T) {
	devices := []*zfs.PoolDevice{
		{Name: "tank", State: zfs.PoolStateOnline},
		{Name: "sda", Depth: 1, State: zfs.PoolStateOnline},
		{Name: "sdb", Depth: 1, State: zfs.PoolStateOnline},
		{Name: "sdc", Depth: 1, State: zfs.PoolStateOnline},
	}
	readings := map[string]disk.Reading{
		"sda": {IsDisk: true, Known: true, Temperature: disk.Temperature{Celsius: 85, Warning: 60, Critical: 85}},
		"sdb": {IsDisk: true, Known: true, Temperature: disk.Temperature{Celsius: 84, Warning: 60, Critical: 85}},
		// outdated
		"sdc": {IsDisk: true, Known: true, Idle: true, Temperature: disk.Temperature{Celsius: 90, Critical: 85}},
	}
	text, _ := formatDevices(devices, readings, nil)
	lines := strings.Split(text, "\n")
	assert.Contains(t, lines[2], "[::l]")
	assert.NotContains(t, lines[3], "[::l]")
	assert.NotContains(t, lines[4], "[::l]")
	assert.Equal(t, " ├─ sda  ONLINE      0     0     0   85°C", testutil.StripTags(lines[2]), "the tags do not move the text")
}

// TestPoolDevices_CriticalBlinksOnScreen checks that tview draws the critical temperature with the blink attribute.
func TestPoolDevices_CriticalBlinksOnScreen(t *testing.T) {
	sampler := disktest.NewSampler(map[string]disk.Reading{
		"sda": {IsDisk: true, Known: true, Temperature: disk.Temperature{Celsius: 86, Critical: 85}},
	})
	app, devices, screen := setupDevicesTest(t, sampler)
	require.Eventually(t, func() bool {
		var fields []clickableField
		testutil.OnUiThread(t, app, func() {
			app.ForceDraw()
			fields = devices.fields
		})
		return len(fields) == 1
	}, 2*time.Second, 10*time.Millisecond)

	var x, y int
	testutil.OnUiThread(t, app, func() {
		field := devices.fields[0]
		x, y = 1+field.column+temperatureWidth-1, 1+field.line
	})
	text, style, _ := screen.Get(x, y)
	assert.Equal(t, "C", text)
	_, _, attributes := style.Decompose()
	assert.NotZero(t, attributes&tcell.AttrBlink)
	text, style, _ = screen.Get(x-temperatureWidth-2, y)
	assert.Equal(t, "0", text, "the checksum counter")
	_, _, attributes = style.Decompose()
	assert.Zero(t, attributes&tcell.AttrBlink)
}

var readAt = time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)

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

func TestFormatDevices_Smart(t *testing.T) {
	smart := map[string][]disk.SmartReport{
		"sda":       {healthy("sda")},
		"sdc":       {withProblems("sdc", failed, reallocated)},
		"sde":       {{Disk: "sde", Err: disk.ErrSmartNeedsRoot}},
		"nvme0n1p1": {healthy("nvme0n1")},
		"sdg":       {withProblems("sdg", reallocated)},
	}
	text, fields := formatDevices(resilveringDevices(), testReadings(), smart)
	lines := strings.Split(strings.TrimSuffix(testutil.StripTags(text), "\n"), "\n")
	assert.Equal(t, []string{
		" NAME               STATE      READ WRITE CKSUM   TEMP  SMART",
		" tank               DEGRADED      0     0     0",
		" └─ raidz2-0        DEGRADED      0     0     0",
		"    ├─ sda          ONLINE        0     0     0   35°C  OK",
		"    ├─ replacing-1  DEGRADED      0     0     0",
		"    │  ├─ sdc       FAULTED      12   340     0      ⟳  FAILED     too many errors",
		"    │  └─ sde       ONLINE        0     0     0   31°C  ?          (resilvering)",
		"    └─ sdb          ONLINE        0     0     2      –",
		" logs",
		" └─ nvme0n1p1       ONLINE        0     0     0   85°C  OK",
		" spares",
		" ├─ sdf             AVAIL",
		" └─ sdg             INUSE                        102°C  8 realloc  currently in use",
	}, lines)

	column := len(" NAME               STATE      READ WRITE CKSUM   TEMP  ")
	var smartFields []clickableField
	for _, field := range fields {
		if field.smart {
			smartFields = append(smartFields, field)
		}
	}
	assert.Equal(t, []clickableField{
		{device: "sda", smart: true, line: 3, column: column, width: 9},
		{device: "sdc", smart: true, line: 5, column: column, width: 9},
		{device: "sde", smart: true, line: 6, column: column, width: 9},
		{device: "nvme0n1p1", smart: true, line: 9, column: column, width: 9},
		{device: "sdg", smart: true, line: 12, column: column, width: 9},
	}, smartFields)
}

func TestFormatDevices_SmartWithoutTemperatures(t *testing.T) {
	devices := []*zfs.PoolDevice{
		{Name: "tank", State: zfs.PoolStateOnline},
		{Name: "sda", Depth: 1, State: zfs.PoolStateOnline, Note: "(trimming)"},
	}
	text, fields := formatDevices(devices, nil, map[string][]disk.SmartReport{"sda": {healthy("sda")}})
	lines := strings.Split(strings.TrimSuffix(testutil.StripTags(text), "\n"), "\n")
	assert.Equal(t, " NAME    STATE    READ WRITE CKSUM  SMART", lines[0])
	assert.Equal(t, " └─ sda  ONLINE      0     0     0  OK     (trimming)", lines[2])
	assert.Equal(t, []clickableField{{device: "sda", smart: true, line: 2, column: len(" NAME    STATE    READ WRITE CKSUM  "), width: 5}}, fields)

	// smartctl is not installed: no column
	text, _ = formatDevices(devices, nil, map[string][]disk.SmartReport{"sda": {{Disk: "sda", Err: disk.ErrSmartctlMissing}}})
	assert.NotContains(t, text, "SMART")
}

// setupSmartTest shows the devices of resilveringDevices with the SMART reports of smart. showDisk is the last disk
// shown on the disks page ("pool/device"), only accessed on the UI thread.
func setupSmartTest(t *testing.T, smart *disktest.Smart) (app *tview.Application, devices *PoolDevicesComponent, showDisk *string, screen tcell.SimulationScreen) {
	app = tview.NewApplication()
	screen = tcell.NewSimulationScreen("UTF-8")
	app.SetScreen(screen)
	app.EnableMouse(true)
	screen.SetSize(120, 30)
	devices = NewPoolDevices(app, disktest.NewSampler(nil), smart)
	app.SetRoot(devices.GetLayout(), true)
	go func() { _ = app.Run() }()
	t.Cleanup(app.Stop)
	shown := ""
	testutil.OnUiThread(t, app, func() {
		devices.SetStatus(&zfs.PoolStatus{Name: "tank", Devices: resilveringDevices()})
		devices.SetOnShowDisk(func(pool string, device string) { shown = pool + "/" + device })
		devices.Focus()
	})
	app.QueueUpdateDraw(func() {})
	return app, devices, &shown, screen
}

func TestPoolDevices_Smart(t *testing.T) {
	smart := disktest.NewSmart(map[string][]disk.SmartReport{"sda": {withProblems("sda", reallocated)}, "sdb": {healthy("sdb")}})
	app, devices, showDisk, screen := setupSmartTest(t, smart)

	// read while drawn, the ones older than 10 minutes (see disk_browser.HealthReader)
	require.Eventually(t, func() bool {
		var text string
		testutil.OnUiThread(t, app, func() { text = devices.GetLayout().GetText(true) })
		return strings.Contains(text, "8 realloc")
	}, 2*time.Second, 10*time.Millisecond)
	assert.Equal(t, []time.Duration{10 * time.Minute}, smart.MaxAges())
	testutil.OnUiThread(t, app, func() {
		names := []string{}
		for _, entry := range devices.GetShortcutMap() {
			names = append(names, entry.Name)
		}
		assert.Equal(t, []string{"Disk health", "Read SMART"}, names)
	})

	// s reads all again
	screen.InjectKey(tcell.KeyRune, 's', tcell.ModNone)
	require.Eventually(t, func() bool { return len(smart.MaxAges()) == 2 }, 2*time.Second, 10*time.Millisecond)
	assert.Equal(t, time.Duration(0), smart.MaxAges()[1])

	// a click on the SMART values of sda shows its disk on the disks page
	var x, y int
	testutil.OnUiThread(t, app, func() {
		app.ForceDraw()
		for _, field := range devices.fields {
			if field.smart && field.device == "sda" {
				x, y = 1+field.column, 1+field.line
			}
		}
	})
	screen.InjectMouse(x, y, tcell.Button1, tcell.ModNone)
	screen.InjectMouse(x, y, tcell.ButtonNone, tcell.ModNone)
	require.Eventually(t, func() bool {
		shown := ""
		testutil.OnUiThread(t, app, func() { shown = *showDisk })
		return shown == "tank/sda"
	}, 2*time.Second, 10*time.Millisecond)
}

func TestPoolDevices_SmartNeedsRoot(t *testing.T) {
	smart := disktest.NewSmart(map[string][]disk.SmartReport{"sda": {{Disk: "sda", Err: disk.ErrSmartNeedsRoot}}})
	app, devices, _, screen := setupSmartTest(t, smart)
	var setups atomic.Int32
	testutil.OnUiThread(t, app, func() { devices.SetOnSetup(func() { setups.Add(1) }) })

	require.Eventually(t, func() bool {
		var footer string
		testutil.OnUiThread(t, app, func() { footer = devices.footer.GetText() })
		return strings.Contains(footer, "SMART needs root: allow")
	}, 2*time.Second, 10*time.Millisecond)

	// a click on the hint offers to allow it
	testutil.OnUiThread(t, app, func() { app.ForceDraw() })
	var x, y int
	testutil.OnUiThread(t, app, func() {
		boxX, boxY, width, height := devices.GetLayout().GetRect()
		// right-aligned in the bottom border, behind the corner and a space
		x, y = boxX+width-4, boxY+height-1
	})
	screen.InjectMouse(x, y, tcell.Button1, tcell.ModNone)
	screen.InjectMouse(x, y, tcell.ButtonNone, tcell.ModNone)
	require.Eventually(t, func() bool { return setups.Load() == 1 }, 2*time.Second, 10*time.Millisecond)
}
