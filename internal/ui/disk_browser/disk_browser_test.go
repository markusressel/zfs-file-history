package disk_browser

import (
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"zfs-file-history/internal/disk"
	"zfs-file-history/internal/testutil"
	"zfs-file-history/internal/ui/table"
	"zfs-file-history/internal/zfs"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeSampler struct {
	mu       sync.Mutex
	readings map[string]disk.Reading
	forced   [][]string
}

func (sampler *fakeSampler) Sample(devices []string, forced []string) map[string]disk.Reading {
	sampler.mu.Lock()
	defer sampler.mu.Unlock()
	sampler.forced = append(sampler.forced, forced)
	result := map[string]disk.Reading{}
	for _, device := range devices {
		result[device] = sampler.readings[device]
	}
	return result
}

func (sampler *fakeSampler) samples() [][]string {
	sampler.mu.Lock()
	defer sampler.mu.Unlock()
	return slices.Clone(sampler.forced)
}

type fakeSmart struct {
	mu      sync.Mutex
	reports map[string][]disk.SmartReport
	maxAges []time.Duration
}

func (smart *fakeSmart) ReadDevices(devices []string, maxAge time.Duration) map[string][]disk.SmartReport {
	smart.mu.Lock()
	defer smart.mu.Unlock()
	smart.maxAges = append(smart.maxAges, maxAge)
	result := map[string][]disk.SmartReport{}
	for _, device := range devices {
		result[device] = smart.reports[device]
	}
	return result
}

func (smart *fakeSmart) Reset() {}

func (smart *fakeSmart) reads() []time.Duration {
	smart.mu.Lock()
	defer smart.mu.Unlock()
	return slices.Clone(smart.maxAges)
}

// testStatuses are two pools: tank on a mirror of two disks and a file, fast on an NVMe disk.
func testStatuses() []*zfs.PoolStatus {
	return []*zfs.PoolStatus{
		{Name: "tank", Devices: []*zfs.PoolDevice{
			{Name: "tank", State: zfs.PoolStateOnline},
			{Name: "mirror-0", Depth: 1, State: zfs.PoolStateOnline},
			{Name: "/dev/disk/by-id/ata-DISK_1-part1", Depth: 2, State: zfs.PoolStateOnline},
			{Name: "sdb", Depth: 2, State: zfs.PoolStateOnline},
			{Name: "/tmp/tank.img", Depth: 1, State: zfs.PoolStateOnline},
		}},
		{Name: "fast", Devices: []*zfs.PoolDevice{
			{Name: "fast", State: zfs.PoolStateOnline},
			{Name: "nvme0n1p1", Depth: 1, State: zfs.PoolStateOnline},
		}},
	}
}

func testSmart() *fakeSmart {
	nvme := healthy("nvme0n1")
	nvme.Attributes = []disk.SmartAttribute{
		{Name: "Percentage used", Value: "93%", Raw: 93, Normalized: -1, Worst: -1, Threshold: -1, Important: true},
		{Name: "Available spare", Value: "100%", Raw: 100, Normalized: -1, Worst: -1, Threshold: -1},
	}
	return &fakeSmart{reports: map[string][]disk.SmartReport{
		"/dev/disk/by-id/ata-DISK_1-part1": {ataReport()},
		"sdb":                              {{Disk: "sdb", Err: disk.ErrSmartNeedsRoot}},
		"nvme0n1p1":                        {nvme},
	}}
}

type diskBrowserTest struct {
	t       *testing.T
	app     *tview.Application
	screen  tcell.SimulationScreen
	pages   *tview.Pages
	browser *DiskBrowserComponent
	details *DiskDetailsComponent
	sampler *fakeSampler
	smart   *fakeSmart
}

// newDiskBrowserTest shows the disks of testStatuses above their details, like on the disks page.
func newDiskBrowserTest(t *testing.T) *diskBrowserTest {
	dt := &diskBrowserTest{t: t, smart: testSmart(), sampler: &fakeSampler{readings: map[string]disk.Reading{
		"/dev/disk/by-id/ata-DISK_1-part1": {IsDisk: true, Known: true, Temperature: disk.Temperature{Celsius: 40, Critical: 60}},
		"sdb":                              {IsDisk: true, Idle: true},
		"nvme0n1p1":                        {IsDisk: true, Known: true, Temperature: disk.Temperature{Celsius: 50}},
	}}}
	dt.app = tview.NewApplication()
	dt.screen = tcell.NewSimulationScreen("UTF-8")
	dt.app.SetScreen(dt.screen)
	dt.app.EnableMouse(true)
	dt.screen.SetSize(140, 40)
	go func() { _ = dt.app.Run() }()
	t.Cleanup(dt.app.Stop)

	testutil.OnUiThread(t, dt.app, func() {
		dt.browser = NewDiskBrowser(dt.app, dt.sampler, dt.smart)
		dt.details = NewDiskDetails(dt.app)
		dt.browser.Events.Subscribe(func(event Event) {
			if e, ok := event.(SelectedDiskChangedEvent); ok {
				dt.details.SetDisk(e.Disk)
			}
		})
		layout := tview.NewFlex().SetDirection(tview.FlexRow).
			AddItem(dt.browser.GetLayout(), 0, 1, true).
			AddItem(dt.details.GetLayout(), 0, 2, false)
		dt.pages = tview.NewPages().AddPage("disks", layout, true, true)
		dt.browser.SetDialogPages(dt.pages)
		dt.details.SetDialogPages(dt.pages)
		dt.app.SetRoot(dt.pages, true)
		dt.browser.SetStatuses(testStatuses())
		dt.browser.Focus()
		dt.app.ForceDraw()
	})
	dt.waitFor("disks listed", func() bool { return len(dt.rows()) == 3 })
	return dt
}

func (dt *diskBrowserTest) waitFor(message string, condition func() bool) {
	require.Eventually(dt.t, condition, 3*time.Second, 10*time.Millisecond, message)
}

func (dt *diskBrowserTest) press(key tcell.Key, r rune) {
	dt.screen.InjectKey(key, r, tcell.ModNone)
}

// rows returns the disks shown, as "pool device disk".
func (dt *diskBrowserTest) rows() []string {
	var rows []string
	testutil.OnUiThread(dt.t, dt.app, func() {
		for _, entry := range dt.browser.tableContainer.GetEntries() {
			rows = append(rows, entry.Pool+" "+entry.DeviceName()+" "+entry.Report.Disk)
		}
	})
	return rows
}

func (dt *diskBrowserTest) detailsText() string {
	text := ""
	testutil.OnUiThread(dt.t, dt.app, func() { text = dt.details.GetDetailsText(true) })
	return text
}

func (dt *diskBrowserTest) attributeNames() []string {
	var names []string
	testutil.OnUiThread(dt.t, dt.app, func() {
		for _, entry := range dt.details.tableContainer.GetEntries() {
			names = append(names, entry.Name)
		}
	})
	return names
}

func (dt *diskBrowserTest) screenText() string {
	var text strings.Builder
	testutil.OnUiThread(dt.t, dt.app, func() {
		dt.app.ForceDraw()
		cells, width, height := dt.screen.GetContents()
		for y := 0; y < height; y++ {
			for x := 0; x < width; x++ {
				if runes := cells[y*width+x].Runes; len(runes) > 0 {
					text.WriteRune(runes[0])
				}
			}
			text.WriteRune('\n')
		}
	})
	return text.String()
}

func TestDiskBrowser_ListsDisksOfAllPools(t *testing.T) {
	dt := newDiskBrowserTest(t)

	// sorted by pool; the file of tank is not on a disk
	assert.Equal(t, []string{"fast nvme0n1p1 nvme0n1", "tank ata-DISK_1-part1 sda", "tank sdb sdb"}, dt.rows())
	dt.waitFor("temperatures", func() bool { return strings.Contains(dt.screenText(), "40°C") })
	screen := dt.screenText()
	assert.Contains(t, screen, "WDC WD40EFRX")
	assert.Contains(t, screen, "8 realloc")
	assert.Contains(t, screen, ReadTemperatureIcon+" idle, not read: t")
	assert.Contains(t, screen, "3 disks · 1 with problems")
	// read while drawn, the ones older than smartInterval
	assert.Equal(t, smartInterval, dt.smart.reads()[0])
	assert.Equal(t, [][]string{nil}, dt.sampler.samples()[:1], "idle hard disks are not forced")
}

func TestDiskBrowser_DetailsOfSelectedDisk(t *testing.T) {
	dt := newDiskBrowserTest(t)

	assert.Contains(t, dt.detailsText(), "nvme0n1p1 → nvme0n1  in fast")
	// NVMe values have no id: in the order of smartctl
	assert.Equal(t, []string{"Percentage used", "Available spare"}, dt.attributeNames())

	dt.press(tcell.KeyDown, 0)
	dt.waitFor("details of sda", func() bool { return strings.Contains(dt.detailsText(), "ata-DISK_1-part1 → sda") })
	assert.Equal(t, []string{"Reallocated_Sector_Ct", "Power_On_Hours", "Airflow_Temperature_Cel", "Temperature_Celsius"}, dt.attributeNames())

	dt.press(tcell.KeyDown, 0)
	dt.waitFor("details of sdb", func() bool { return strings.Contains(dt.detailsText(), "only root may read them") })
	assert.Empty(t, dt.attributeNames())
}

func TestDiskBrowser_SelectDevice(t *testing.T) {
	dt := newDiskBrowserTest(t)

	testutil.OnUiThread(t, dt.app, func() { dt.browser.SelectDevice("tank", "sdb") })
	assert.Contains(t, dt.detailsText(), "sdb  in tank")
	// the first disk of the pool
	testutil.OnUiThread(t, dt.app, func() { dt.browser.SelectDevice("tank", "") })
	assert.Contains(t, dt.detailsText(), "ata-DISK_1-part1 → sda  in tank")
}

func TestDiskBrowser_SelectDeviceBeforeLoaded(t *testing.T) {
	app := tview.NewApplication()
	app.SetScreen(tcell.NewSimulationScreen("UTF-8"))
	go func() { _ = app.Run() }()
	t.Cleanup(app.Stop)
	var browser *DiskBrowserComponent
	selected := ""
	testutil.OnUiThread(t, app, func() {
		browser = NewDiskBrowser(app, &fakeSampler{}, testSmart())
		browser.Events.Subscribe(func(event Event) {
			if e, ok := event.(SelectedDiskChangedEvent); ok && e.Disk != nil {
				selected = e.Disk.Device
			}
		})
		app.SetRoot(browser.GetLayout(), true)
		browser.SelectDevice("tank", "sdb")
		browser.SetStatuses(testStatuses())
		app.ForceDraw()
	})
	require.Eventually(t, func() bool {
		var shown string
		testutil.OnUiThread(t, app, func() { shown = selected })
		return shown == "sdb"
	}, 3*time.Second, 10*time.Millisecond)
}

func TestDiskBrowser_ReadAgain(t *testing.T) {
	dt := newDiskBrowserTest(t)

	dt.press(tcell.KeyRune, 's')
	dt.waitFor("SMART read again", func() bool { return len(dt.smart.reads()) == 2 })
	assert.Equal(t, time.Duration(0), dt.smart.reads()[1])

	dt.press(tcell.KeyRune, 't')
	dt.waitFor("temperatures read", func() bool {
		samples := dt.sampler.samples()
		// all devices are forced, the sampler tells which are on disks
		return len(samples[len(samples)-1]) == 4
	})
}

func TestDiskBrowser_SetupFromFooter(t *testing.T) {
	dt := newDiskBrowserTest(t)
	var setups atomic.Int32
	testutil.OnUiThread(t, dt.app, func() { dt.browser.SetOnSetup(func() { setups.Add(1) }) })

	x, y := -1, -1
	testutil.OnUiThread(t, dt.app, func() {
		dt.app.ForceDraw()
		cells, width, height := dt.screen.GetContents()
		for row := 0; row < height && y < 0; row++ {
			var line strings.Builder
			for column := 0; column < width; column++ {
				if runes := cells[row*width+column].Runes; len(runes) > 0 {
					line.WriteRune(runes[0])
				}
			}
			if index := strings.Index(line.String(), "SMART needs root"); index >= 0 {
				x, y = len([]rune(line.String()[:index])), row
			}
		}
	})
	require.GreaterOrEqual(t, y, 0)
	dt.screen.InjectMouse(x, y, tcell.Button1, tcell.ModNone)
	dt.screen.InjectMouse(x, y, tcell.ButtonNone, tcell.ModNone)
	dt.waitFor("setup offered", func() bool { return setups.Load() == 1 })
}

func TestDiskBrowser_ColumnDialogs(t *testing.T) {
	dt := newDiskBrowserTest(t)
	// dialogShown returns whether a dialog is in front of the disks
	dialogShown := func() bool {
		shown := false
		testutil.OnUiThread(t, dt.app, func() { front, _ := dt.pages.GetFrontPage(); shown = front != "disks" })
		return shown
	}

	dt.press(tcell.KeyF2, 0)
	dt.waitFor("column dialog of the disks", dialogShown)
	assert.Contains(t, dt.screenText(), "Configure Disk Columns")
	dt.press(tcell.KeyEscape, 0)
	dt.waitFor("closed", func() bool { return !dialogShown() })

	testutil.OnUiThread(t, dt.app, func() { dt.details.Focus() })
	dt.press(tcell.KeyF2, 0)
	dt.waitFor("column dialog of the attributes", dialogShown)
	assert.Contains(t, dt.screenText(), "Configure Attribute Columns")
}

func TestSortAttributes(t *testing.T) {
	attributes := func() []*AttributeEntry {
		var entries []*AttributeEntry
		for i, attribute := range ataReport().Attributes {
			entries = append(entries, &AttributeEntry{SmartAttribute: attribute, Index: i})
		}
		// a value without a number
		entries = append(entries, &AttributeEntry{SmartAttribute: disk.SmartAttribute{Id: 240, Name: "Head_Flying_Hours", Value: "n/a", Raw: -1, Normalized: 100}, Index: len(entries)})
		return entries
	}
	ids := func(entries []*AttributeEntry) []int {
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
		{attributeColumnId, false, []int{5, 9, 190, 194, 240}},
		{attributeColumnId, true, []int{240, 194, 190, 9, 5}},
		{attributeColumnName, false, []int{190, 240, 9, 5, 194}},
		// by their number, not their text ("35 (Min/Max 20/45)" < "62" < "8" < "8760" as text), the ones without last
		{attributeColumnValue, false, []int{5, 194, 190, 9, 240}},
		{attributeColumnNorm, false, []int{190, 9, 240, 194, 5}},
		{attributeColumnFailed, false, []int{190, 5, 9, 194, 240}},
	}
	for _, test := range tests {
		t.Run(test.column.Key, func(t *testing.T) {
			assert.Equal(t, test.ids, ids(sortAttributes(attributes(), test.column, test.inverted)))
		})
	}
}

func TestSortDiskEntries(t *testing.T) {
	failing := healthy("sdc")
	failing.Passed = false
	failing.Problems = []disk.SmartProblem{failed}
	warning := withProblems("sdb", reallocated)
	warning.PowerOnHours = 50
	entries := func() []*DiskEntry {
		return []*DiskEntry{
			{Pool: "tank", Device: "sda", Report: healthy("sda"), Reading: disk.Reading{IsDisk: true, Known: true, Temperature: disk.Temperature{Celsius: 30}}},
			{Pool: "tank", Device: "sdb", Report: warning, Reading: disk.Reading{IsDisk: true, Idle: true}},
			{Pool: "fast", Device: "sdc", Report: failing, Reading: disk.Reading{IsDisk: true, Known: true, Temperature: disk.Temperature{Celsius: 50}}},
			{Pool: "tank", Device: "sdd", Report: disk.SmartReport{Disk: "sdd", Err: disk.ErrSmartNeedsRoot}},
		}
	}
	devices := func(entries []*DiskEntry) []string {
		var result []string
		for _, entry := range entries {
			result = append(result, entry.Device)
		}
		return result
	}
	assert.Equal(t, []string{"sdc", "sda", "sdb", "sdd"}, devices(sortDiskEntries(entries(), columnPool, false)))
	assert.Equal(t, []string{"sdc", "sdb", "sda", "sdd"}, devices(sortDiskEntries(entries(), columnSmart, false)))
	assert.Equal(t, []string{"sdc", "sda", "sdb", "sdd"}, devices(sortDiskEntries(entries(), columnHealth, false)))
	// not known first
	assert.Equal(t, []string{"sdd", "sdb", "sdc", "sda"}, devices(sortDiskEntries(entries(), columnHours, false)))
	assert.Equal(t, []string{"sdb", "sdd", "sda", "sdc"}, devices(sortDiskEntries(entries(), columnTemp, false)))
}
