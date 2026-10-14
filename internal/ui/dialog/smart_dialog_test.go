package dialog

import (
	"errors"
	"strings"
	"testing"
	"time"
	"zfs-file-history/internal/disk"
	"zfs-file-history/internal/testutil"
	"zfs-file-history/internal/ui/table"
	"zfs-file-history/internal/ui/theme"
	"zfs-file-history/internal/ui/util"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var smartReadAt = time.Date(2026, 10, 14, 12, 0, 0, 0, time.Local)

func healthySmartReport(name string) disk.SmartReport {
	return disk.SmartReport{Disk: name, ReadAt: smartReadAt, CheckedAt: smartReadAt, HealthKnown: true, Passed: true, PowerOnHours: 100}
}

var smartReallocated = disk.SmartProblem{Severity: disk.SmartWarning, Short: "8 realloc", Text: "8 reallocated sectors"}
var smartFailed = disk.SmartProblem{Severity: disk.SmartError, Short: "FAILED", Text: "the health check of the disk failed"}

// ataSmartReport is a report of an ATA disk with a problem.
func ataSmartReport() disk.SmartReport {
	return disk.SmartReport{
		Disk: "sda", ReadAt: smartReadAt, CheckedAt: smartReadAt, Model: "WDC WD40EFRX", Serial: "WD-1", Firmware: "82.00A82", Capacity: 4000787030016, Protocol: "ATA",
		HealthKnown: true, Passed: true, PowerOnHours: 8760,
		Problems: []disk.SmartProblem{smartReallocated},
		Attributes: []disk.SmartAttribute{
			{Id: 5, Name: "Reallocated_Sector_Ct", Value: "8", Raw: 8, Normalized: 199, Worst: 199, Threshold: 140, Important: true},
			{Id: 9, Name: "Power_On_Hours", Value: "8760", Raw: 8760, Normalized: 52, Worst: 52, Threshold: 0},
			{Id: 194, Name: "Temperature_Celsius", Value: "35 (Min/Max 20/45)", Raw: 35, Normalized: 115, Worst: 100, Threshold: 0},
			{Id: 190, Name: "Airflow_Temperature_Cel", Value: "62", Raw: 62, Normalized: 38, Worst: 30, Threshold: 40, Failed: "now"},
		},
	}
}

func TestFormatSmartReport(t *testing.T) {
	lines := formatSmartReport("/dev/disk/by-id/ata-DISK_1-part1", ataSmartReport())
	assert.Equal(t, []string{
		"ata-DISK_1-part1 → sda",
		"  Disk:     WDC WD40EFRX  serial WD-1 · 4.0 TB · firmware 82.00A82 · ATA",
		"  Health:   PASSED",
		"  Power on: 8760 h (1.0 years)",
		"  Read:     " + util.FormatTime(smartReadAt),
		"  Problems: 8 reallocated sectors",
	}, strings.Split(testutil.StripTags(strings.Join(lines, "\n")), "\n"), "the attributes are in their own table")

	tests := []struct {
		name   string
		report disk.SmartReport
		lines  []string
	}{
		{"asleep", disk.SmartReport{Disk: "sdb", Standby: true, CheckedAt: smartReadAt},
			[]string{"sdb", "  State:    asleep, not woken up to read it"}},
		{"needs root", disk.SmartReport{Disk: "sdb", Err: disk.ErrSmartNeedsRoot},
			[]string{"sdb", "  Error:    only root may read them: allow \"Disk health\" in the command menu (:)"}},
		{"other error", disk.SmartReport{Disk: "sdb", Err: errors.New("smartctl failed: no")},
			[]string{"sdb", "  Error:    smartctl failed: no"}},
		{"NVMe", disk.SmartReport{Disk: "sdb", ReadAt: smartReadAt, HealthKnown: true, PowerOnHours: -1},
			[]string{"sdb", "  Health:   FAILED", "  Read:     " + util.FormatTime(smartReadAt)}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assert.Equal(t, test.lines, strings.Split(testutil.StripTags(strings.Join(formatSmartReport("sdb", test.report), "\n")), "\n"))
		})
	}
}

func TestSmartSummary(t *testing.T) {
	withProblems := func(name string, problems ...disk.SmartProblem) disk.SmartReport {
		report := healthySmartReport(name)
		report.Problems = problems
		return report
	}
	asleepAfterRead := healthySmartReport("sda")
	asleepAfterRead.Standby = true
	tests := []struct {
		name    string
		reports []disk.SmartReport
		text    string
		color   tcell.Color
	}{
		{"not on a disk", nil, "", tcell.ColorDefault},
		{"healthy", []disk.SmartReport{healthySmartReport("sda")}, "OK", theme.Colors.Pool.Online},
		{"warning", []disk.SmartReport{withProblems("sda", smartReallocated)}, "8 realloc", theme.Colors.Pool.Degraded},
		{"the worst of several disks", []disk.SmartReport{withProblems("sda", smartReallocated), withProblems("sdb", smartFailed)}, "FAILED", theme.Colors.Pool.Faulted},
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

func TestSortSmartAttributes(t *testing.T) {
	attributes := func() []*smartAttribute {
		var entries []*smartAttribute
		for i, attribute := range ataSmartReport().Attributes {
			entries = append(entries, &smartAttribute{SmartAttribute: attribute, index: i})
		}
		// a value without a number
		entries = append(entries, &smartAttribute{SmartAttribute: disk.SmartAttribute{Id: 240, Name: "Head_Flying_Hours", Value: "n/a", Raw: -1}, index: len(entries)})
		return entries
	}
	ids := func(entries []*smartAttribute) []int {
		var result []int
		for _, entry := range entries {
			result = append(result, entry.Id)
		}
		return result
	}
	tests := []struct {
		column   *table.Column
		inverted bool
		ids      []int
	}{
		{smartAttributeColumnId, false, []int{5, 9, 190, 194, 240}},
		{smartAttributeColumnId, true, []int{240, 194, 190, 9, 5}},
		{smartAttributeColumnName, false, []int{190, 240, 9, 5, 194}},
		// by their number, not their text ("35 (Min/Max 20/45)" < "62" < "8" < "8760" as text), the ones without last
		{smartAttributeColumnRaw, false, []int{5, 194, 190, 9, 240}},
		{smartAttributeColumnNorm, false, []int{240, 190, 9, 194, 5}},
		{smartAttributeColumnFailed, false, []int{190, 5, 9, 194, 240}},
	}
	for _, test := range tests {
		t.Run(test.column.Key, func(t *testing.T) {
			assert.Equal(t, test.ids, ids(sortSmartAttributes(attributes(), test.column, test.inverted)))
		})
	}
}

func TestSortSmartDisks(t *testing.T) {
	failing := healthySmartReport("sdc")
	failing.Passed = false
	failing.Problems = []disk.SmartProblem{smartFailed}
	warning := healthySmartReport("sdb")
	warning.PowerOnHours = 50
	warning.Problems = []disk.SmartProblem{smartReallocated}
	entries := func() []*smartDisk {
		return []*smartDisk{
			{device: "sda", report: healthySmartReport("sda")},
			{device: "sdb", report: warning},
			{device: "sdc", report: failing},
			{device: "sdd", report: disk.SmartReport{Disk: "sdd", Err: disk.ErrSmartNeedsRoot}},
		}
	}
	devices := func(entries []*smartDisk) []string {
		var result []string
		for _, entry := range entries {
			result = append(result, entry.device)
		}
		return result
	}
	assert.Equal(t, []string{"sdc", "sdb", "sda", "sdd"}, devices(sortSmartDisks(entries(), smartDiskColumnSmart, false)))
	assert.Equal(t, []string{"sdc", "sda", "sdb", "sdd"}, devices(sortSmartDisks(entries(), smartDiskColumnHealth, false)))
	assert.Equal(t, []string{"sdd", "sdb", "sda", "sdc"}, devices(sortSmartDisks(entries(), smartDiskColumnHours, false)))
}

type smartDialogTest struct {
	t      *testing.T
	app    *tview.Application
	screen tcell.SimulationScreen
	pages  *tview.Pages
	dialog *SmartDialog
}

// newSmartDialogTest shows the dialog for the devices, with the given one selected.
func newSmartDialogTest(t *testing.T, devices []SmartDevice, focus string) *smartDialogTest {
	st := &smartDialogTest{t: t}
	st.app = tview.NewApplication()
	st.screen = tcell.NewSimulationScreen("UTF-8")
	st.app.SetScreen(st.screen)
	st.pages = tview.NewPages().AddPage("background", tview.NewBox(), true, true)
	st.app.SetRoot(st.pages, true)
	go func() { _ = st.app.Run() }()
	t.Cleanup(st.app.Stop)

	testutil.OnUiThread(t, st.app, func() {
		st.dialog = NewSmartDialog(st.app, "tank", devices, focus, nil)
		ShowDialogOnPages(st.app, st.pages, st.dialog, nil)
		st.app.ForceDraw()
	})
	return st
}

func (st *smartDialogTest) press(key tcell.Key, r rune) {
	st.screen.InjectKey(key, r, tcell.ModNone)
}

// attributeNames returns the names of the attributes shown, in their order.
func (st *smartDialogTest) attributeNames() []string {
	var names []string
	testutil.OnUiThread(st.t, st.app, func() {
		for _, entry := range st.dialog.attributes.GetEntries() {
			names = append(names, entry.Name)
		}
	})
	return names
}

func (st *smartDialogTest) details() string {
	text := ""
	testutil.OnUiThread(st.t, st.app, func() { text = st.dialog.GetText(true) })
	return text
}

func smartTestDevices() []SmartDevice {
	nvme := healthySmartReport("nvme0n1")
	nvme.Attributes = []disk.SmartAttribute{
		{Name: "Percentage used", Value: "93%", Raw: 93, Normalized: -1, Worst: -1, Threshold: -1, Important: true},
		{Name: "Available spare", Value: "100%", Raw: 100, Normalized: -1, Worst: -1, Threshold: -1},
	}
	return []SmartDevice{
		{Name: "tank.img"},
		{Name: "/dev/disk/by-id/ata-DISK_1-part1", Reports: []disk.SmartReport{ataSmartReport()}},
		{Name: "nvme0n1p1", Reports: []disk.SmartReport{nvme}},
	}
}

func TestSmartDialog_SelectsFocusedDevice(t *testing.T) {
	st := newSmartDialogTest(t, smartTestDevices(), "nvme0n1p1")

	assert.Contains(t, st.details(), "nvme0n1p1 → nvme0n1")
	// NVMe values have no id, sorted by their name
	assert.Equal(t, []string{"Available spare", "Percentage used"}, st.attributeNames())
	testutil.OnUiThread(t, st.app, func() {
		assert.Len(t, st.dialog.disks.GetAllEntries(), 2, "a file is not stored on a disk")
		assert.False(t, st.dialog.attributesAta, "no columns of normalized values")
	})
}

func TestSmartDialog_ShowsAttributesOfSelectedDisk(t *testing.T) {
	st := newSmartDialogTest(t, smartTestDevices(), "")

	// the disks sorted by their device: ata-DISK_1-part1 first
	assert.Contains(t, st.details(), "ata-DISK_1-part1 → sda")
	assert.Equal(t, []string{"Reallocated_Sector_Ct", "Power_On_Hours", "Airflow_Temperature_Cel", "Temperature_Celsius"}, st.attributeNames())

	st.press(tcell.KeyDown, 0)
	require.Eventually(t, func() bool { return strings.Contains(st.details(), "nvme0n1") }, 3*time.Second, 10*time.Millisecond)
	assert.Equal(t, []string{"Available spare", "Percentage used"}, st.attributeNames())
}

func TestSmartDialog_TabSortsAttributes(t *testing.T) {
	st := newSmartDialogTest(t, smartTestDevices(), "")

	st.press(tcell.KeyTab, 0)
	require.Eventually(t, func() bool {
		focused := false
		testutil.OnUiThread(t, st.app, func() { focused = st.dialog.attributes.HasFocus() })
		return focused
	}, 3*time.Second, 10*time.Millisecond)
	// up to the header, then → sorts by the next column (the name)
	st.press(tcell.KeyUp, 0)
	st.press(tcell.KeyRight, 0)
	require.Eventually(t, func() bool {
		names := st.attributeNames()
		return len(names) == 4 && names[0] == "Airflow_Temperature_Cel"
	}, 3*time.Second, 10*time.Millisecond)
	assert.Equal(t, []string{"Airflow_Temperature_Cel", "Power_On_Hours", "Reallocated_Sector_Ct", "Temperature_Celsius"}, st.attributeNames())
}

func TestSmartDialog_NoDisks(t *testing.T) {
	st := newSmartDialogTest(t, []SmartDevice{{Name: "tank.img"}}, "")
	testutil.OnUiThread(t, st.app, func() {
		assert.Equal(t, "No device of the pool is stored on a disk.", st.dialog.disks.GetPlaceholder())
	})
	assert.Empty(t, st.details())
}
