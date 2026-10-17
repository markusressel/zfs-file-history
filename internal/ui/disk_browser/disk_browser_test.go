package disk_browser

import (
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"
	"zfs-file-history/internal/disk"
	"zfs-file-history/internal/disk/disktest"
	"zfs-file-history/internal/state"
	"zfs-file-history/internal/state/statetest"
	"zfs-file-history/internal/testutil"
	"zfs-file-history/internal/ui/table"
	uiutil "zfs-file-history/internal/ui/util"
	"zfs-file-history/internal/zfs"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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

func testSmart() *disktest.Smart {
	nvme := healthy("nvme0n1")
	nvme.Attributes = []disk.SmartAttribute{
		{Name: "Percentage used", Value: "93%", Raw: 93, Normalized: -1, Worst: -1, Threshold: -1, Important: true},
		{Name: "Available spare", Value: "100%", Raw: 100, Normalized: -1, Worst: -1, Threshold: -1},
	}
	return disktest.NewSmart(map[string][]disk.SmartReport{
		"/dev/disk/by-id/ata-DISK_1-part1": {ataReport()},
		"sdb":                              {{Disk: "sdb", Err: disk.ErrSmartNeedsRoot, Rotational: true}},
		"nvme0n1p1":                        {nvme},
	})
}

type diskBrowserTest struct {
	t       *testing.T
	app     *tview.Application
	screen  tcell.SimulationScreen
	pages   *tview.Pages
	browser *DiskBrowserComponent
	details *DiskDetailsComponent
	sampler *disktest.Sampler
	smart   *disktest.Smart
}

// newDiskBrowserTest shows the disks of testStatuses above their details, like on the disks page.
func newDiskBrowserTest(t *testing.T) *diskBrowserTest {
	dt := &diskBrowserTest{t: t, smart: testSmart(), sampler: disktest.NewSampler(map[string]disk.Reading{
		"/dev/disk/by-id/ata-DISK_1-part1": {IsDisk: true, Known: true, Temperature: disk.Temperature{Celsius: 40, Critical: 60}},
		"sdb":                              {IsDisk: true, Idle: true},
		"nvme0n1p1":                        {IsDisk: true, Known: true, Temperature: disk.Temperature{Celsius: 50}},
	})}
	dt.app, dt.screen = testutil.NewApp()
	dt.screen.SetSize(140, 40)
	testutil.RunApp(t, dt.app)

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
	testutil.Press(dt.t, dt.app, dt.screen, key, r)
}

// rows returns the disks shown, as "pool device disk".
func (dt *diskBrowserTest) rows() []string {
	var rows []string
	testutil.OnUiThread(dt.t, dt.app, func() {
		for _, entry := range dt.browser.tableContainer.GetEntries() {
			rows = append(rows, entry.PoolNames()+" "+entry.DeviceNames()+" "+entry.Report.Disk)
		}
	})
	return rows
}

func (dt *diskBrowserTest) detailsText() string {
	text := ""
	// the name of the disk is the title
	testutil.OnUiThread(dt.t, dt.app, func() {
		text = strings.TrimSpace(testutil.StripTags(dt.details.details.GetTitle())) + "\n" + dt.details.GetDetailsText(true)
	})
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
	return testutil.ScreenText(dt.t, dt.app, dt.screen)
}

func TestDiskBrowser_ListsDisksOfAllPools(t *testing.T) {
	dt := newDiskBrowserTest(t)

	// the hard disks first, sorted by pool; the file of tank is not on a disk
	assert.Equal(t, []string{"tank ata-DISK_1-part1 sda", "tank sdb sdb", "fast nvme0n1p1 nvme0n1"}, dt.rows())
	dt.waitFor("temperatures", func() bool { return strings.Contains(dt.screenText(), "40°C") })
	screen := dt.screenText()
	assert.Contains(t, screen, "WDC WD40EFRX")
	assert.Contains(t, screen, "8 realloc")
	assert.Contains(t, screen, ReadTemperatureIcon+" idle, not read: t")
	assert.Contains(t, screen, "3 disks · 1 with problems")
	assert.Contains(t, screen, "Hard disks  2")
	assert.Contains(t, screen, "SSDs  1")
	// the serial number instead of the long names of the devices, which the filter still finds
	assert.Contains(t, screen, "WD-1")
	assert.NotContains(t, screen, "Device(s)")
	testutil.OnUiThread(t, dt.app, func() { dt.browser.tableContainer.SetFilterText("ata-DISK*") })
	assert.Equal(t, []string{"tank ata-DISK_1-part1 sda"}, dt.rows())
	// read while drawn, the ones older than smartInterval
	assert.Equal(t, smartInterval, dt.smart.MaxAges()[0])
	assert.Equal(t, [][]string{nil}, dt.sampler.Forced()[:1], "idle hard disks are not forced")
}

func TestDiskBrowser_DetailsOfSelectedDisk(t *testing.T) {
	dt := newDiskBrowserTest(t)

	assert.Contains(t, dt.detailsText(), "tank  ata-DISK_1-part1")
	assert.Equal(t, []string{"Reallocated_Sector_Ct", "Power_On_Hours", "Airflow_Temperature_Cel", "Temperature_Celsius"}, dt.attributeNames())

	dt.press(tcell.KeyDown, 0)
	dt.waitFor("details of sdb", func() bool { return strings.Contains(dt.detailsText(), "only root may read them") })
	assert.Empty(t, dt.attributeNames())

	// over the row of the SSDs
	dt.press(tcell.KeyDown, 0)
	dt.waitFor("details of nvme0n1", func() bool {
		return strings.Contains(dt.detailsText(), "nvme0n1\n● healthy · SSD in fast")
	})
	// NVMe values have no id: in the order of smartctl
	assert.Equal(t, []string{"Percentage used", "Available spare"}, dt.attributeNames())
}

func TestDiskBrowser_SelectDevice(t *testing.T) {
	dt := newDiskBrowserTest(t)

	testutil.OnUiThread(t, dt.app, func() { dt.browser.SelectDevice("tank", "sdb") })
	assert.Contains(t, dt.detailsText(), "sdb\n● health unknown: SMART needs root · HDD in tank")
	// the disk of the pool with a problem, e.g. for its alert
	testutil.OnUiThread(t, dt.app, func() { dt.browser.SelectDevice("tank", "sdb") })
	testutil.OnUiThread(t, dt.app, func() { dt.browser.SelectDevice("tank", "") })
	assert.Contains(t, dt.detailsText(), "sda · WDC WD40EFRX\n● 8 reallocated sectors · SATA HDD in tank")
	// the first one if none has
	testutil.OnUiThread(t, dt.app, func() { dt.browser.SelectDevice("fast", "") })
	assert.Contains(t, dt.detailsText(), "nvme0n1\n● healthy · SSD in fast")
}

func TestDiskBrowser_SelectDeviceBeforeLoaded(t *testing.T) {
	app, _ := testutil.NewApp()
	testutil.RunApp(t, app)
	var browser *DiskBrowserComponent
	selected := ""
	testutil.OnUiThread(t, app, func() {
		browser = NewDiskBrowser(app, disktest.NewSampler(nil), testSmart())
		browser.Events.Subscribe(func(event Event) {
			if e, ok := event.(SelectedDiskChangedEvent); ok && e.Disk != nil {
				selected = e.Disk.Devices[0].Device
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
	dt.waitFor("SMART read again", func() bool { return len(dt.smart.MaxAges()) == 2 })
	assert.Equal(t, time.Duration(0), dt.smart.MaxAges()[1])

	dt.press(tcell.KeyRune, 't')
	dt.waitFor("temperatures read", func() bool {
		samples := dt.sampler.Forced()
		// all devices are forced, the sampler tells which are on disks
		return len(samples[len(samples)-1]) == 4
	})
}

func TestDiskBrowser_SetupFromFooter(t *testing.T) {
	dt := newDiskBrowserTest(t)
	var setups atomic.Int32
	testutil.OnUiThread(t, dt.app, func() { dt.browser.SetOnSetup(func() { setups.Add(1) }) })

	testutil.ClickText(t, dt.app, dt.screen, "SMART needs root")
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
		// the ones that cannot fail first, in the order of smartctl
		{attributeColumnRisk, false, []int{9, 194, 240, 5, 190}},
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
			{Devices: []PoolDevice{{Pool: "tank", Device: "sda"}}, Report: healthy("sda"), Reading: disk.Reading{IsDisk: true, Known: true, Temperature: disk.Temperature{Celsius: 30}}},
			{Devices: []PoolDevice{{Pool: "tank", Device: "sdb"}}, Report: warning, Reading: disk.Reading{IsDisk: true, Idle: true}},
			{Devices: []PoolDevice{{Pool: "fast", Device: "sdc"}}, Report: failing, Reading: disk.Reading{IsDisk: true, Known: true, Temperature: disk.Temperature{Celsius: 50}}},
			{Devices: []PoolDevice{{Pool: "tank", Device: "sdd"}}, Report: disk.SmartReport{Disk: "sdd", Err: disk.ErrSmartNeedsRoot}},
		}
	}
	devices := func(entries []*DiskEntry) []string {
		var result []string
		for _, entry := range entries {
			result = append(result, entry.Devices[0].Device)
		}
		return result
	}
	assert.Equal(t, []string{"sdc", "sda", "sdb", "sdd"}, devices(sortDiskEntries(entries(), columnPool, false)))
	assert.Equal(t, []string{"sdc", "sdb", "sda", "sdd"}, devices(sortDiskEntries(entries(), columnSmart, false)))
	assert.Equal(t, []string{"sdc", "sda", "sdb", "sdd"}, devices(sortDiskEntries(entries(), columnHealth, false)))
	// not known first, equal ones by disk
	assert.Equal(t, []string{"sdd", "sdb", "sda", "sdc"}, devices(sortDiskEntries(entries(), columnHours, false)))
	assert.Equal(t, []string{"sdb", "sdd", "sda", "sdc"}, devices(sortDiskEntries(entries(), columnTemp, false)))
}

// Regression: a disk to select that was not listed stayed pending, and was selected by surprise once its pool
// showed up later.
func TestDiskBrowser_SelectDeviceNotListed(t *testing.T) {
	dt := newDiskBrowserTest(t)
	dt.smart.SetReports("sdz", []disk.SmartReport{healthy("sdz")})

	testutil.OnUiThread(t, dt.app, func() { dt.browser.SelectDevice("other", "sdz") })
	assert.Contains(t, dt.detailsText(), "sda · WDC WD40EFRX\n● 8 reallocated sectors · SATA HDD in tank", "the first disk")

	statuses := append(testStatuses(), &zfs.PoolStatus{Name: "other", Devices: []*zfs.PoolDevice{
		{Name: "other", State: zfs.PoolStateOnline},
		{Name: "sdz", Depth: 1, State: zfs.PoolStateOnline},
	}})
	testutil.OnUiThread(t, dt.app, func() {
		dt.browser.SetStatuses(statuses)
		dt.browser.ReadSmart()
	})
	dt.waitFor("the new pool listed", func() bool { return len(dt.rows()) == 4 })
	assert.Contains(t, dt.detailsText(), "sda · WDC WD40EFRX\n● 8 reallocated sectors · SATA HDD in tank", "still the disk the user selected")
}

func TestAttentionRank(t *testing.T) {
	hot := &DiskEntry{Report: healthy("sda"), Reading: disk.Reading{IsDisk: true, Known: true, Temperature: disk.Temperature{Celsius: 62, Critical: 60}}}
	failing := &DiskEntry{Report: withProblems("sdb", failed)}
	warning := &DiskEntry{Report: withProblems("sdc", reallocated)}
	fine := &DiskEntry{Report: healthy("sdd")}
	assert.Equal(t, 0, attentionRank(hot))
	assert.Equal(t, 0, attentionRank(failing))
	assert.Equal(t, 1, attentionRank(warning))
	assert.Equal(t, 2, attentionRank(fine))
}

// Regression: reading the disks again (every 10 s while shown) selected the first disk while the header was selected,
// and the first attribute while the header of the attributes was.
func TestDiskBrowser_HeaderStaysSelected(t *testing.T) {
	dt := newDiskBrowserTest(t)
	headers := func() (disks bool, attributes bool) {
		testutil.OnUiThread(t, dt.app, func() {
			disks = dt.browser.tableContainer.GetSelectedEntry() == nil
			attributes = dt.details.tableContainer.GetSelectedEntry() == nil
		})
		return disks, attributes
	}

	testutil.OnUiThread(t, dt.app, func() {
		dt.details.tableContainer.SelectHeader()
		dt.browser.SetStatuses(testStatuses())
	})
	disks, attributes := headers()
	assert.False(t, disks)
	assert.True(t, attributes, "header of the attributes")

	testutil.OnUiThread(t, dt.app, func() {
		dt.browser.tableContainer.SelectHeader()
		dt.browser.SetStatuses(testStatuses())
	})
	disks, _ = headers()
	assert.True(t, disks, "header of the disks")
	assert.Empty(t, dt.attributeNames(), "no disk selected")
}

// Regression: when the SMART values were read ("38 seconds ago") was only updated when the disks were read again,
// every 10 seconds, instead of with the times of the tables (every second while they count seconds).
func TestDiskDetails_ReadTimeUpdated(t *testing.T) {
	dt := newDiskBrowserTest(t)
	require.False(t, uiutil.IsRelativeTimes())
	t.Cleanup(func() {
		if uiutil.IsRelativeTimes() {
			uiutil.ToggleRelativeTimes()
		}
	})
	assert.NotContains(t, dt.detailsText(), " ago")

	// changes the generation of the formatted times, like the time passing does
	uiutil.ToggleRelativeTimes()
	dt.screenText()
	assert.Contains(t, dt.detailsText(), " ago")
}

// A disk with a partition for each of two pools (e.g. bpool and rpool) is listed once, with both devices.
func TestDiskBrowser_DiskOfSeveralPools(t *testing.T) {
	dt := newDiskBrowserTest(t)
	dt.smart.SetReports("nvme0n1p2", []disk.SmartReport{healthy("nvme0n1")})
	statuses := append(testStatuses(), &zfs.PoolStatus{Name: "boot", Devices: []*zfs.PoolDevice{
		{Name: "boot", State: zfs.PoolStateOnline},
		{Name: "nvme0n1p2", Depth: 1, State: zfs.PoolStateOnline},
	}})
	testutil.OnUiThread(t, dt.app, func() {
		dt.browser.SetStatuses(statuses)
		dt.browser.ReadSmart()
	})
	dt.waitFor("both devices", func() bool { return slices.Contains(dt.rows(), "fast, boot nvme0n1p1, nvme0n1p2 nvme0n1") })
	assert.Len(t, dt.rows(), 3)

	testutil.OnUiThread(t, dt.app, func() { dt.browser.SelectDevice("boot", "nvme0n1p2") })
	details := dt.detailsText()
	assert.Contains(t, details, "nvme0n1\n● healthy · SSD in fast, boot")
	assert.Contains(t, details, "fast  nvme0n1p1")
	assert.Contains(t, details, "boot  nvme0n1p2")
}

func TestDiskBrowser_Groupings(t *testing.T) {
	dt := newDiskBrowserTest(t)
	grouping := func() string {
		key := ""
		testutil.OnUiThread(t, dt.app, func() { key = dt.browser.tableContainer.GetGrouping() })
		return key
	}
	assert.Equal(t, groupingType, grouping(), "by type at first")

	dt.press(tcell.KeyRune, 'b')
	dt.waitFor("by pool", func() bool { return grouping() == groupingPool })
	screen := dt.screenText()
	assert.Contains(t, screen, "fast  1")
	assert.Contains(t, screen, "tank  2")
	assert.Equal(t, []string{"fast nvme0n1p1 nvme0n1", "tank ata-DISK_1-part1 sda", "tank sdb sdb"}, dt.rows())

	dt.press(tcell.KeyRune, 'b')
	dt.waitFor("not grouped", func() bool { return grouping() == "" })
	assert.NotContains(t, dt.screenText(), "Hard disks")
}

func TestDiskEntry_DeviceNames(t *testing.T) {
	entry := DiskEntry{Devices: []PoolDevice{
		{Pool: "bpool", Device: "/dev/disk/by-id/nvme-Samsung_SSD_970_EVO_Plus_1TB_S4EWNX0R518399R-part2"},
		{Pool: "rpool", Device: "/dev/disk/by-id/nvme-Samsung_SSD_970_EVO_Plus_1TB_S4EWNX0R518399R-part3"},
	}}
	assert.Equal(t, "nvme-Samsung_SSD_970_EVO_Plus_1TB_S4EWNX0R518399R-part2, part3", entry.DeviceNames())
	// only up to a "-"
	entry.Devices = []PoolDevice{{Pool: "a", Device: "sdb1"}, {Pool: "b", Device: "sdb2"}}
	assert.Equal(t, "sdb1, sdb2", entry.DeviceNames())
}

// A click on the name of a pool in the details shows the pool, also where they are below each other.
func TestDiskDetails_ClickOnPoolShowsPool(t *testing.T) {
	dt := newDiskBrowserTest(t)
	shown := ""
	testutil.OnUiThread(t, dt.app, func() { dt.details.SetOnShowPool(func(pool string) { shown = pool }) })

	for _, width := range []int{140, 44} {
		dt.screen.SetSize(width, 40)
		x, y, found := testutil.FindText(t, dt.app, dt.screen, "Pools     tank")
		require.True(t, found, dt.screenText())
		testutil.Click(t, dt.app, dt.screen, x+len("Pools     "), y)
		testutil.OnUiThread(t, dt.app, func() { assert.Equal(t, "tank", shown, "width %d", width) })
		testutil.OnUiThread(t, dt.app, func() { shown = "" })

		// the label does nothing
		testutil.Click(t, dt.app, dt.screen, x, y)
		testutil.WaitForUpdates(t, dt.app)
		testutil.OnUiThread(t, dt.app, func() { assert.Empty(t, shown) })
	}
}

// Not while a dialog is in front.
func TestDiskDetails_PoolNotClickedBehindDialog(t *testing.T) {
	dt := newDiskBrowserTest(t)
	shown := ""
	testutil.OnUiThread(t, dt.app, func() {
		dt.details.SetOnShowPool(func(pool string) { shown = pool })
		dt.details.SetEnabledFunc(func() bool { return false })
	})
	x, y, found := testutil.FindText(t, dt.app, dt.screen, "Pools     tank")
	require.True(t, found)
	testutil.Click(t, dt.app, dt.screen, x+len("Pools     "), y)
	testutil.WaitForUpdates(t, dt.app)
	testutil.OnUiThread(t, dt.app, func() { assert.Empty(t, shown) })
}

// The attributes that can fail show how close they are to it; sorted by it (a click on the header), the closest come
// first.
func TestDiskDetails_RiskColumn(t *testing.T) {
	dt := newDiskBrowserTest(t)
	text := dt.screenText()
	assert.Contains(t, text, "Thresh Risk ")
	assert.Contains(t, text, "    40 ████████ 100%")
	assert.Contains(t, text, "   140            0%")

	// the first click selects the header, the next ones sort by the column and toggle the direction
	for range 3 {
		if strings.Contains(dt.screenText(), "Risk ↓") {
			break
		}
		testutil.ClickText(t, dt.app, dt.screen, "Risk")
	}
	require.Contains(t, dt.screenText(), "Risk ↓")
	assert.Equal(t, []string{"Airflow_Temperature_Cel", "Reallocated_Sector_Ct"}, dt.attributeNames()[:2])
}

// Regression: a layout of the attributes saved by 2.7.0 (before the column Risk) did not get the column.
func TestDiskDetails_RiskColumnAddedToSavedLayout(t *testing.T) {
	store := statetest.Use(t)
	store.SetTableLayout(stateKeyAttributesTable, state.TableLayout{
		Columns:    []string{"id", "name", "normalized", "worst", "threshold", "value", "failed"},
		SortColumn: "name",
	})
	app, _ := testutil.NewApp()
	testutil.RunApp(t, app)
	var columns []*table.Column
	testutil.OnUiThread(t, app, func() { columns = NewDiskDetails(app).tableContainer.GetColumnSpec() })
	assert.Equal(t, attributeColumns, columns, "after the threshold, as in the default layout")
}

// Regression: a click into the details focused them, which Tab skips: there is nothing to do in them.
func TestDiskDetails_ClickDoesNotFocus(t *testing.T) {
	dt := newDiskBrowserTest(t)
	var x, y int
	testutil.OnUiThread(t, dt.app, func() {
		dt.app.ForceDraw()
		x, y, _, _ = dt.details.details.GetInnerRect()
	})
	testutil.Click(dt.t, dt.app, dt.screen, x+1, y)
	// the table of the disks keeps the focus; nothing changes when the events were handled, so wait for them
	testutil.WaitForEvents(t, dt.app, dt.screen)
	focused := false
	testutil.OnUiThread(t, dt.app, func() { focused = dt.browser.HasFocus() && !dt.details.details.HasFocus() })
	assert.True(t, focused)
}

// Regression: the details showed what the disk is and its health side by side at any width, without wrapping, so on a
// narrow pane the health was cut off. They are below each other there, and what is still too wide is wrapped.
func TestDiskDetails_NarrowPane(t *testing.T) {
	dt := newDiskBrowserTest(t)
	dt.screen.SetSize(60, 40)
	text := dt.screenText()
	assert.Contains(t, text, "Temp      █████▏       40°C · critical at 60°C")
	assert.Contains(t, text, "Check     PASSED")

	dt.screen.SetSize(44, 40)
	text = dt.screenText()
	assert.Contains(t, text, "Temp      █████▏       40°C · critical")
	// wrapped below the value, in its color: tview keeps the style across the line break
	x, y, found := testutil.FindText(t, dt.app, dt.screen, "          at 60°C")
	require.True(t, found, text)
	criticalX, criticalY, found := testutil.FindText(t, dt.app, dt.screen, "critical")
	require.True(t, found, text)
	var wrapped, critical tcell.Style
	testutil.OnUiThread(t, dt.app, func() {
		_, wrapped, _ = dt.screen.Get(x+10, y)
		_, critical, _ = dt.screen.Get(criticalX, criticalY)
	})
	assert.Equal(t, critical, wrapped)

	// wide again: side by side
	dt.screen.SetSize(140, 40)
	assert.Contains(t, dt.screenText(), "Type      SATA HDD                    Temp      █████▏       40°C · critical at 60°C")
}

// Regression: the details took the height of their text, so on a low window they left no line for the attributes.
// They take at most detailsMaxPercent of it and scroll.
func TestDiskDetails_LowWindow(t *testing.T) {
	dt := newDiskBrowserTest(t)
	dt.screen.SetSize(140, 18)
	text := dt.screenText()
	assert.Contains(t, text, "Reallocated_Sector_Ct", "the first attribute")
	assert.Contains(t, text, "● 8 reallocated sectors", "the verdict")
}
