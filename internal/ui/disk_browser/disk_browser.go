package disk_browser

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
	"zfs-file-history/internal/disk"
	"zfs-file-history/internal/state"
	"zfs-file-history/internal/ui/dialog"
	"zfs-file-history/internal/ui/shortcut_helper"
	"zfs-file-history/internal/ui/table"
	"zfs-file-history/internal/ui/theme"
	"zfs-file-history/internal/ui/txwidgets"
	uiutil "zfs-file-history/internal/ui/util"
	"zfs-file-history/internal/util"
	"zfs-file-history/internal/zfs"

	"github.com/dustin/go-humanize"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

var (
	columnPool     = &table.Column{Id: 0, Key: "pool", Title: "Pool", Alignment: tview.AlignLeft}
	columnDevice   = &table.Column{Id: 1, Key: "device", Title: "Device", Alignment: tview.AlignLeft}
	columnDisk     = &table.Column{Id: 2, Key: "disk", Title: "Disk", Alignment: tview.AlignLeft}
	columnModel    = &table.Column{Id: 3, Key: "model", Title: "Model", Alignment: tview.AlignLeft}
	columnSerial   = &table.Column{Id: 4, Key: "serial", Title: "Serial", Alignment: tview.AlignLeft}
	columnFirmware = &table.Column{Id: 5, Key: "firmware", Title: "Firmware", Alignment: tview.AlignLeft}
	columnProtocol = &table.Column{Id: 6, Key: "protocol", Title: "Type", Alignment: tview.AlignLeft}
	columnCapacity = &table.Column{Id: 7, Key: "capacity", Title: "Size", Alignment: tview.AlignRight}
	columnTemp     = &table.Column{Id: 8, Key: "temperature", Title: "Temp", Alignment: tview.AlignRight}
	columnHealth   = &table.Column{Id: 9, Key: "health", Title: "Health", Alignment: tview.AlignLeft}
	columnSmart    = &table.Column{Id: 10, Key: "smart", Title: "SMART", Alignment: tview.AlignLeft}
	columnHours    = &table.Column{Id: 11, Key: "hours", Title: "Hours", Alignment: tview.AlignRight}
	columnRead     = &table.Column{Id: 12, Key: "read", Title: "Read", Alignment: tview.AlignLeft}

	tableColumns = []*table.Column{columnPool, columnDevice, columnDisk, columnModel, columnSerial, columnFirmware,
		columnProtocol, columnCapacity, columnTemp, columnHealth, columnSmart, columnHours, columnRead}
	// defaultColumns are the columns shown until the user chooses others (F2)
	defaultColumns = []*table.Column{columnPool, columnDevice, columnDisk, columnModel, columnCapacity, columnTemp,
		columnHealth, columnSmart, columnHours}
)

// stateKeyTable is the key of the column layout remembered in state.Current
const stateKeyTable = "diskBrowser"

const (
	// sampleInterval is how often the temperatures are sampled while the disks are shown, like in the devices pane
	// of the pools page (see disk.Sampler)
	sampleInterval = 10 * time.Second
	// smartInterval is how old the SMART values may be while the disks are shown: smartctl runs with sudo, which
	// logs every call. The activity monitor reads them every hour (for the alerts), the reports are shared.
	smartInterval = 10 * time.Minute
)

// DiskEntry is a disk of a device of a pool (a device on several disks, e.g. md, has an entry for each), with its
// SMART values and the temperature of its device.
type DiskEntry struct {
	Pool string
	// Device is the name of the device in "zpool status"
	Device  string
	Report  disk.SmartReport
	Reading disk.Reading
}

func (entry DiskEntry) TableRowId() string {
	return entry.Pool + "\x00" + entry.Device + "\x00" + entry.Report.Disk
}

// DeviceName returns the name of the device as shown, e.g. without "/dev/disk/by-id/".
func (entry DiskEntry) DeviceName() string {
	return zfs.DeviceDisplayName(entry.Device)
}

type Event interface{}

// SelectedDiskChangedEvent is emitted when another disk is selected, Disk is nil if none is.
type SelectedDiskChangedEvent struct {
	Disk *DiskEntry
}

// poolDevice is a device of a pool stored on disks, see DiskBrowserComponent.devices.
type poolDevice struct {
	pool   string
	device string
}

// DiskBrowserComponent lists the disks of all pools: what they are, their temperature and their health (SMART).
//
// Like the devices pane of the pools page, the temperatures are only sampled while the disks are drawn (the disks
// page is shown), every sampleInterval, and idle hard disks are only read when asked to (t), so they can spin down
// (see disk.Sampler). The SMART values are read while they are drawn as well, if older than smartInterval, or with s.
type DiskBrowserComponent struct {
	Events *util.Emitter[Event]

	application    *tview.Application
	tableContainer *table.RowSelectionTable[DiskEntry]
	view           *diskView
	sampler        disk.TemperatureSampler
	smart          disk.SmartSource
	loader         *uiutil.DataLoader[map[string]disk.Reading]
	smartLoader    *uiutil.DataLoader[map[string][]disk.SmartReport]

	// All fields below are only accessed on the UI thread.

	// statuses are the statuses of all pools, see SetStatuses
	statuses []*zfs.PoolStatus
	// readings are the last readings of the devices, by their name
	readings map[string]disk.Reading
	// sampledDevices are the devices of the last sample, sampledAt when it was started
	sampledDevices []string
	sampledAt      time.Time
	// sampleTimer draws again after sampleInterval, which samples again if the disks are still shown
	sampleTimer *time.Timer
	// smartReports are the SMART reports of the disks of the devices, by the name of the device; nil until read
	smartReports map[string][]disk.SmartReport
	// smartDevices are the devices SMART was last read for, smartReadAt when
	smartDevices []string
	smartReadAt  time.Time
	// toSelect is the disk to select once it is listed, see SelectDevice
	toSelect *poolDevice
	// dialogPages are the pages dialogs are shown on, see SetDialogPages
	dialogPages *tview.Pages
	// onSetup offers to allow running smartctl with sudo, nil: not offered, see SetOnSetup
	onSetup func()
	// enabled returns whether the footer can be clicked, e.g. not while a dialog is in front
	enabled func() bool
}

// diskView is the table of the disks as placed in the layout: drawing it samples the disks (see
// DiskBrowserComponent.sampleIfDue), so nothing is read while the disks are not shown.
type diskView struct {
	tview.Primitive
	browser *DiskBrowserComponent
}

func (view *diskView) Draw(screen tcell.Screen) {
	view.browser.sampleIfDue()
	view.browser.smartIfDue()
	view.Primitive.Draw(screen)
}

// NewDiskBrowser returns the list of the disks, reading their temperatures with the sampler and their SMART values
// with smart. Must be called on the UI thread.
func NewDiskBrowser(application *tview.Application, sampler disk.TemperatureSampler, smart disk.SmartSource) *DiskBrowserComponent {
	browser := &DiskBrowserComponent{
		Events:      util.NewEmitter[Event](),
		application: application,
		sampler:     sampler,
		smart:       smart,
		enabled:     func() bool { return true },
	}
	browser.tableContainer = table.NewTableContainer[DiskEntry](application, toTableCells, sortDiskEntries)
	browser.view = &diskView{Primitive: browser.tableContainer.GetLayout(), browser: browser}
	browser.setupTable()

	browser.loader = uiutil.NewDataLoader[map[string]disk.Reading](application).
		OnLoad(func(readings map[string]disk.Reading) {
			browser.readings = readings
			browser.render()
			browser.scheduleSample()
		})
	browser.smartLoader = uiutil.NewDataLoader[map[string][]disk.SmartReport](application).
		OnLoad(func(reports map[string][]disk.SmartReport) {
			browser.smartReports = reports
			browser.render()
		})
	return browser
}

func (browser *DiskBrowserComponent) setupTable() {
	t := browser.tableContainer
	t.SetTitle("Disks")
	t.SetFilterFunc(func(entry *DiskEntry, filterText string) bool {
		return table.MatchesGlob(entry.Pool, filterText) || table.MatchesGlob(entry.DeviceName(), filterText) ||
			table.MatchesGlob(entry.Report.Disk, filterText) || table.MatchesGlob(entry.Report.Model, filterText) ||
			table.MatchesGlob(entry.Report.Serial, filterText)
	})
	t.SetFilterChangedCallback(browser.updateFooter)
	t.SetColumnSpec(tableColumns, columnPool, false)
	t.SetActiveColumns(defaultColumns)
	t.BindColumnLayout(state.Current, stateKeyTable, tableColumns)
	t.SetPlaceholder("Reading the disks of the pools…", theme.Colors.Pool.Hint)
	t.SetFooterEnabledFunc(func() bool { return browser.enabled() })
	t.SetSelectionChangedCallback(func(entry *DiskEntry) {
		browser.Events.Emit(SelectedDiskChangedEvent{Disk: entry})
	})
	t.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		switch {
		case event.Key() == tcell.KeyF2:
			browser.openColumnSelectionDialog()
			return nil
		case event.Key() == tcell.KeyRune && event.Rune() == 't':
			browser.ReadTemperatures()
			return nil
		case event.Key() == tcell.KeyRune && event.Rune() == 's':
			browser.ReadSmart()
			return nil
		}
		return event
	})
}

// SetDialogPages sets the pages its dialogs are shown on: the pages of the application, so they are modal for the
// whole screen. Must be called on the UI thread.
func (browser *DiskBrowserComponent) SetDialogPages(pages *tview.Pages) {
	browser.dialogPages = pages
}

// SetOnSetup sets the function that offers to allow running smartctl with sudo, offered while the SMART values
// cannot be read without. Must be called on the UI thread.
func (browser *DiskBrowserComponent) SetOnSetup(onSetup func()) {
	browser.onSetup = onSetup
	browser.updateFooter()
}

// SetEnabledFunc sets a function that returns whether the footer can be clicked, e.g. not while a dialog is in front
// (tview hands mouse events to all pages, see rivo/tview#926). Must be called on the UI thread.
func (browser *DiskBrowserComponent) SetEnabledFunc(enabled func() bool) {
	browser.enabled = enabled
}

// SetStatuses sets the statuses of all pools, whose disks are listed. Must be called on the UI thread.
func (browser *DiskBrowserComponent) SetStatuses(statuses []*zfs.PoolStatus) {
	browser.statuses = statuses
	browser.render()
}

// devices returns the devices of all pools that are not groups (e.g. mirrors), in the order of the pools.
func (browser *DiskBrowserComponent) devices() []poolDevice {
	var devices []poolDevice
	for _, status := range browser.statuses {
		for _, name := range zfs.LeafDevices(status.Devices) {
			devices = append(devices, poolDevice{pool: status.Name, device: name})
		}
	}
	return devices
}

// deviceNames returns the names of the devices of all pools, see devices.
func (browser *DiskBrowserComponent) deviceNames() []string {
	var names []string
	for _, device := range browser.devices() {
		names = append(names, device.device)
	}
	return names
}

// render shows the disks of the devices with their readings and SMART values, keeping the selected one. Runs on the
// UI thread.
func (browser *DiskBrowserComponent) render() {
	var entries []*DiskEntry
	if browser.smartReports != nil {
		entries = []*DiskEntry{}
		for _, device := range browser.devices() {
			for _, report := range browser.smartReports[device.device] {
				entries = append(entries, &DiskEntry{Pool: device.pool, Device: device.device, Report: report,
					Reading: browser.readings[device.device]})
			}
		}
		browser.tableContainer.SetPlaceholder("No pool is stored on a disk.", theme.Colors.Pool.Hint)
	}

	selected := ""
	if entry := browser.tableContainer.GetSelectedEntry(); entry != nil {
		selected = entry.TableRowId()
	}
	browser.tableContainer.SetData(entries)
	visible := browser.tableContainer.GetEntries()
	var toSelect *DiskEntry
	if browser.toSelect != nil {
		for _, entry := range visible {
			if entry.Pool == browser.toSelect.pool && (browser.toSelect.device == "" || entry.Device == browser.toSelect.device) {
				toSelect = entry
				browser.toSelect = nil
				break
			}
		}
	}
	if toSelect == nil {
		for _, entry := range visible {
			if entry.TableRowId() == selected {
				toSelect = entry
			}
		}
	}
	if toSelect == nil && len(visible) > 0 {
		toSelect = visible[0]
	}
	// SetData does not notify the selection callback, so always select explicitly, to keep the details in sync
	if toSelect != nil {
		browser.tableContainer.Select(toSelect)
	} else {
		browser.Events.Emit(SelectedDiskChangedEvent{Disk: nil})
	}
	browser.updateFooter()
}

// SelectDevice selects the first disk of the device of the pool (device "": the first disk of the pool), or once
// it is listed. Must be called on the UI thread.
func (browser *DiskBrowserComponent) SelectDevice(pool string, device string) {
	browser.toSelect = &poolDevice{pool: pool, device: device}
	browser.tableContainer.SetFilterText("")
	browser.render()
}

// updateFooter shows the number of disks and the ones with problems in the bottom border, and the hints about the
// values that were not read: the temperatures of idle hard disks (a click reads them) and of disks without the
// kernel module drivetemp, and SMART values that need root (a click offers to allow it). Runs on the UI thread.
func (browser *DiskBrowserComponent) updateFooter() {
	all := browser.tableContainer.GetAllEntries()
	idle, needsDrivetemp, smartNeedsRoot := false, false, false
	withProblems := 0
	for _, entry := range all {
		idle = idle || entry.Reading.Idle && !entry.Reading.Known
		needsDrivetemp = needsDrivetemp || entry.Reading.NeedsDrivetemp
		smartNeedsRoot = smartNeedsRoot || errors.Is(entry.Report.Err, disk.ErrSmartNeedsRoot)
		if len(entry.Report.Problems) > 0 {
			withProblems++
		}
	}

	var parts []uiutil.FooterPart
	add := func(part uiutil.FooterPart) {
		if len(parts) > 0 {
			parts = append(parts, uiutil.FooterPart{Text: " · "})
		}
		parts = append(parts, part)
	}
	if needsDrivetemp {
		add(uiutil.FooterPart{Text: txwidgets.Span(theme.Colors.Pool.Hint, "SATA temperatures need: modprobe drivetemp")})
	}
	if idle {
		add(uiutil.FooterPart{
			Text:    txwidgets.Span(theme.Colors.Pool.ReadTemperature, "%s idle, not read: t", ReadTemperatureIcon),
			OnClick: browser.ReadTemperatures,
		})
	}
	if smartNeedsRoot && browser.onSetup != nil {
		add(uiutil.FooterPart{Text: txwidgets.Span(theme.Colors.Pool.ReadTemperature, "SMART needs root: allow"), OnClick: browser.onSetup})
	}
	if browser.smartReports != nil {
		count := fmt.Sprintf("%d disks", len(all))
		if browser.tableContainer.IsFilterActive() {
			count = fmt.Sprintf("%d of %d disks", len(browser.tableContainer.GetEntries()), len(all))
		}
		if withProblems > 0 {
			count += txwidgets.Span(theme.Colors.Pool.Degraded, " · %d with problems", withProblems)
		}
		add(uiutil.FooterPart{Text: count})
	}
	browser.tableContainer.SetFooterParts(parts...)
}

// ReadSmart reads the SMART values of all disks again, also if they were read recently. Disks that sleep are not
// woken up. Must be called on the UI thread.
func (browser *DiskBrowserComponent) ReadSmart() {
	browser.readSmart(browser.deviceNames(), 0)
}

// smartIfDue reads the SMART values of the devices if they changed, or the last read is older than smartInterval.
// Called while the disks are drawn, like sampleIfDue. Runs on the UI thread.
func (browser *DiskBrowserComponent) smartIfDue() {
	if browser.smart == nil || browser.statuses == nil || browser.smartLoader.IsLoading() {
		return
	}
	devices := browser.deviceNames()
	if browser.smartReports != nil && slices.Equal(devices, browser.smartDevices) && time.Since(browser.smartReadAt) < smartInterval {
		return
	}
	browser.readSmart(devices, smartInterval)
}

// readSmart reads the SMART values of the devices in the background, the ones older than maxAge. Runs on the UI
// thread.
func (browser *DiskBrowserComponent) readSmart(devices []string, maxAge time.Duration) {
	if browser.smart == nil {
		return
	}
	browser.smartDevices = devices
	browser.smartReadAt = time.Now()
	smart := browser.smart
	browser.smartLoader.LoadQuietly(func(ctx context.Context) (map[string][]disk.SmartReport, error) {
		return smart.ReadDevices(devices, maxAge), nil
	})
}

// ReadTemperatures reads the temperatures of all disks, also of idle hard disks. Must be called on the UI thread.
func (browser *DiskBrowserComponent) ReadTemperatures() {
	devices := browser.deviceNames()
	browser.sample(devices, devices)
}

// sampleIfDue samples the devices if they changed, or the last sample is older than sampleInterval. Called while
// the disks are drawn, so nothing is sampled while they are not shown. Runs on the UI thread.
func (browser *DiskBrowserComponent) sampleIfDue() {
	if browser.sampler == nil || browser.statuses == nil || browser.loader.IsLoading() {
		return
	}
	devices := browser.deviceNames()
	// a timer may fire a little early
	if slices.Equal(devices, browser.sampledDevices) && time.Since(browser.sampledAt) < sampleInterval-time.Second {
		return
	}
	browser.sample(devices, nil)
}

// sample samples the devices in the background, forced ones are read in any case. Runs on the UI thread.
func (browser *DiskBrowserComponent) sample(devices []string, forced []string) {
	if browser.sampler == nil {
		return
	}
	browser.sampledDevices = devices
	browser.sampledAt = time.Now()
	sampler := browser.sampler
	browser.loader.LoadQuietly(func(ctx context.Context) (map[string]disk.Reading, error) {
		return sampler.Sample(devices, forced), nil
	})
}

// scheduleSample draws again after sampleInterval, which samples the devices again if they are still shown (see
// sampleIfDue). Runs on the UI thread.
func (browser *DiskBrowserComponent) scheduleSample() {
	if browser.sampleTimer != nil {
		browser.sampleTimer.Stop()
	}
	application := browser.application
	browser.sampleTimer = time.AfterFunc(sampleInterval, func() {
		application.QueueUpdateDraw(func() {})
	})
}

// openColumnSelectionDialog lets the user select and order the displayed columns. Runs on the UI thread.
func (browser *DiskBrowserComponent) openColumnSelectionDialog() {
	if browser.dialogPages == nil {
		return
	}
	d := dialog.NewTableColumnSelectionDialog(browser.application, "Configure Disk Columns", tableColumns, browser.tableContainer)
	dialog.ShowDialogOnPages(browser.application, browser.dialogPages, d, nil)
}

// GetShortcutMap returns the shortcuts of the list of the disks.
func (browser *DiskBrowserComponent) GetShortcutMap() []shortcut_helper.ShortcutEntry {
	return []shortcut_helper.ShortcutEntry{
		{
			KeyCombo: []string{"s"}, Name: "Read SMART", Group: shortcut_helper.GroupAction,
			Description: "Read the SMART values of the disks again (sleeping disks are not woken up)",
			Run:         browser.ReadSmart,
		},
		{
			KeyCombo: []string{"t"}, Name: "Read temperatures", Group: shortcut_helper.GroupAction,
			Description: "Read the temperatures of the disks, also of idle hard disks (reading may keep them from spinning down)",
			Run:         browser.ReadTemperatures,
		},
		uiutil.TableComponentShortcutColumns.WithRun(browser.openColumnSelectionDialog).OnlyInMenu(),
		uiutil.TableComponentShortcutFilter.WithRun(browser.tableContainer.StartFilter),
	}
}

// GetCommands returns the sort commands of the list, see shortcut_helper.CommandProvider.
func (browser *DiskBrowserComponent) GetCommands() []shortcut_helper.ShortcutEntry {
	return browser.tableContainer.SortCommands()
}

func (browser *DiskBrowserComponent) GetLayout() tview.Primitive {
	return browser.view
}

func (browser *DiskBrowserComponent) HasFocus() bool {
	return browser.tableContainer.HasFocus()
}

func (browser *DiskBrowserComponent) Focus() {
	browser.application.SetFocus(browser.view)
}

// ContentSize returns the size of the table with all disks, including the border. Must be called on the UI thread.
func (browser *DiskBrowserComponent) ContentSize() (width int, height int) {
	return browser.tableContainer.ContentSize()
}

// toTableCells shows a disk: its temperature colored by how hot it is (blinking at its critical one), its health and
// most severe problem colored like in the devices pane.
func toTableCells(row int, columns []*table.Column, entry *DiskEntry) []*tview.TableCell {
	report := entry.Report
	var cells []*tview.TableCell
	for _, column := range columns {
		text, color := "", tcell.ColorLightGray
		attributes := tcell.AttrNone
		switch column {
		case columnPool:
			text = entry.Pool
		case columnDevice:
			text, color = entry.DeviceName(), tcell.ColorWhite
		case columnDisk:
			text = report.Disk
		case columnModel:
			text, color = report.Model, tcell.ColorWhite
		case columnSerial:
			text = report.Serial
		case columnFirmware:
			text = report.Firmware
		case columnProtocol:
			text = report.Protocol
		case columnCapacity:
			if report.Capacity > 0 {
				text = uiutil.StableLengthHumanizedBytes(uint64(report.Capacity))
			}
		case columnTemp:
			text, color = FormatTemperature(entry.Reading)
			if entry.Reading.IsCritical() {
				attributes = tcell.AttrBlink
			}
		case columnHealth:
			text, color = formatHealth(report)
		case columnSmart:
			text, color = SmartSummary([]disk.SmartReport{report})
		case columnHours:
			if report.HasValues() && report.PowerOnHours >= 0 {
				text = humanize.Comma(report.PowerOnHours)
			}
		case columnRead:
			if !report.ReadAt.IsZero() {
				text = uiutil.FormatTime(report.ReadAt)
			}
		}
		cells = append(cells, tview.NewTableCell(tview.Escape(text)).SetTextColor(color).SetAttributes(attributes).
			SetAlign(column.Alignment))
	}
	return cells
}

// healthRank orders the results of the health check: failed first, not read last.
func healthRank(report disk.SmartReport) int {
	switch {
	case report.Err != nil || !report.HasValues():
		return 3
	case !report.HealthKnown:
		return 2
	case report.Passed:
		return 1
	default:
		return 0
	}
}

// problemRank orders disks by their most severe problem: errors first, then warnings, then the others.
func problemRank(report disk.SmartReport) int {
	problem, ok := report.Worst()
	switch {
	case !ok:
		return 2
	case problem.Severity == disk.SmartError:
		return 0
	default:
		return 1
	}
}

// temperatureOf returns the temperature of the disk for sorting, -1 if it is not known.
func temperatureOf(reading disk.Reading) float64 {
	if !reading.Known {
		return -1
	}
	return reading.Temperature.Celsius
}

// powerOnHoursOf returns the power on hours of the disk for sorting, -1 if they are not known.
func powerOnHoursOf(report disk.SmartReport) int64 {
	if !report.HasValues() {
		return -1
	}
	return report.PowerOnHours
}

// sortDiskEntries sorts the disks by the column, and by pool, device and disk if equal.
func sortDiskEntries(entries []*DiskEntry, column *table.Column, inverted bool) []*DiskEntry {
	slices.SortStableFunc(entries, func(a, b *DiskEntry) int {
		result := 0
		switch column {
		case columnDevice:
			result = strings.Compare(a.DeviceName(), b.DeviceName())
		case columnDisk:
			result = strings.Compare(a.Report.Disk, b.Report.Disk)
		case columnModel:
			result = strings.Compare(a.Report.Model, b.Report.Model)
		case columnSerial:
			result = strings.Compare(a.Report.Serial, b.Report.Serial)
		case columnFirmware:
			result = strings.Compare(a.Report.Firmware, b.Report.Firmware)
		case columnProtocol:
			result = strings.Compare(a.Report.Protocol, b.Report.Protocol)
		case columnCapacity:
			result = cmp.Compare(a.Report.Capacity, b.Report.Capacity)
		case columnTemp:
			result = cmp.Compare(temperatureOf(a.Reading), temperatureOf(b.Reading))
		case columnHealth:
			result = cmp.Compare(healthRank(a.Report), healthRank(b.Report))
		case columnSmart:
			result = cmp.Compare(problemRank(a.Report), problemRank(b.Report))
		case columnHours:
			result = cmp.Compare(powerOnHoursOf(a.Report), powerOnHoursOf(b.Report))
		case columnRead:
			result = a.Report.ReadAt.Compare(b.Report.ReadAt)
		}
		if result == 0 {
			result = cmp.Or(strings.Compare(a.Pool, b.Pool), strings.Compare(a.DeviceName(), b.DeviceName()),
				strings.Compare(a.Report.Disk, b.Report.Disk))
		}
		if inverted {
			result = -result
		}
		return result
	})
	return entries
}
