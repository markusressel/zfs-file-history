package pool_status

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
	"zfs-file-history/internal/disk"
	"zfs-file-history/internal/ui/clickable"
	"zfs-file-history/internal/ui/disk_browser"
	"zfs-file-history/internal/ui/shortcut_helper"
	"zfs-file-history/internal/ui/theme"
	"zfs-file-history/internal/ui/txwidgets"
	uiutil "zfs-file-history/internal/ui/util"
	"zfs-file-history/internal/zfs"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// the lines of the tree of devices, like the ones of the tree view of the dataset browser
const (
	treeBranch     = "├─ "
	treeLastBranch = "└─ "
	treeLine       = "│  "
	treeSpace      = "   "
)

// temperatureWidth is the width of the temperature column, e.g. " 45°C".
const temperatureWidth = 5

// sampleInterval is how often the disks are sampled while the devices are shown, see disk.Sampler: whether hard
// disks are busy is told by comparing their I/O statistics between two samples.
const sampleInterval = 10 * time.Second

// smartInterval is how old the SMART values may be while the devices are shown: smartctl runs with sudo, which logs
// every call. The activity monitor reads them every hour (for the alerts), the reports are shared.
const smartInterval = 10 * time.Minute

// PoolDevicesComponent shows the devices of a pool as a tree, like the config of "zpool status": their state, their
// read, write and checksum errors, the temperature of their disks and their health (SMART).
//
// The temperatures are only sampled while the devices are drawn (the pools page is shown), every sampleInterval.
// Idle hard disks are not read, so they can spin down (see disk.Sampler): a click on their temperature, or t, reads
// them. The SMART values are read while they are drawn as well, if older than smartInterval, or with s. A click on
// them, or Enter, shows them on the disks page.
type PoolDevicesComponent struct {
	application *tview.Application
	textView    *DevicesView
	footer      *uiutil.BorderFooter
	sampler     disk.TemperatureSampler
	loader      *uiutil.DataLoader[map[string]disk.Reading]
	smart       disk.SmartSource
	smartLoader *uiutil.DataLoader[map[string][]disk.SmartReport]

	// All fields below are only accessed on the UI thread.

	status *zfs.PoolStatus
	// readings are the last readings of the devices, by their name
	readings map[string]disk.Reading
	// sampledDevices are the devices of the last sample, sampledAt when it was started
	sampledDevices []string
	sampledAt      time.Time
	// sampleTimer draws again after sampleInterval, which samples again if the devices are still shown
	sampleTimer *time.Timer
	// smartReports are the SMART reports of the disks of the devices, by the name of the device
	smartReports map[string][]disk.SmartReport
	// smartDevices are the devices SMART was last read for, smartReadAt when
	smartDevices []string
	smartReadAt  time.Time
	// fields are the temperatures and SMART values in the text, which can be clicked
	fields  []clickableField
	regions clickable.Regions
	// enabled returns whether the fields can be clicked, e.g. not while a dialog is in front
	enabled func() bool
	// onShowDisk shows the disks of the device on the disks page, nil: not offered, see SetOnShowDisk
	onShowDisk func(pool string, device string)
	// onSetup offers to allow running smartctl with sudo, nil: not offered, see SetOnSetup
	onSetup func()
}

// DevicesView is the text view of the devices, which also draws the parts that can be clicked, see
// PoolDevicesComponent.
type DevicesView struct {
	*tview.TextView
	devices *PoolDevicesComponent
}

// NewPoolDevices returns the devices component, reading the temperatures with the sampler and the SMART values with
// smart. Must be called on the UI thread.
func NewPoolDevices(application *tview.Application, sampler disk.TemperatureSampler, smart disk.SmartSource) *PoolDevicesComponent {
	poolDevices := &PoolDevicesComponent{
		application: application,
		sampler:     sampler,
		smart:       smart,
		enabled:     func() bool { return true },
	}
	textView := tview.NewTextView().
		SetDynamicColors(true).
		SetWrap(false).
		SetScrollable(true)
	textView.SetBorder(true)
	uiutil.SetupWindow(textView, "Devices")
	poolDevices.textView = &DevicesView{TextView: textView, devices: poolDevices}
	poolDevices.footer = uiutil.NewBorderFooter(textView.Box)

	poolDevices.loader = uiutil.NewDataLoader[map[string]disk.Reading](application).
		OnLoad(func(readings map[string]disk.Reading) {
			poolDevices.readings = readings
			poolDevices.render()
			poolDevices.scheduleSample()
		})

	poolDevices.smartLoader = uiutil.NewDataLoader[map[string][]disk.SmartReport](application).
		OnLoad(func(reports map[string][]disk.SmartReport) {
			poolDevices.smartReports = reports
			poolDevices.render()
		})

	textView.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		switch {
		case event.Key() == tcell.KeyRune && event.Rune() == 't' && poolDevices.hasDisks():
			poolDevices.ReadTemperatures()
			return nil
		case event.Key() == tcell.KeyRune && event.Rune() == 's' && poolDevices.hasSmart():
			poolDevices.ReadSmart()
			return nil
		case event.Key() == tcell.KeyEnter && poolDevices.hasSmart():
			poolDevices.ShowSmart("")
			return nil
		}
		return event
	})
	return poolDevices
}

// SetOnShowDisk sets the function that shows the disks of a device of the pool (device "": the first one) with
// their SMART values, on the disks page. Must be called on the UI thread.
func (poolDevices *PoolDevicesComponent) SetOnShowDisk(onShowDisk func(pool string, device string)) {
	poolDevices.onShowDisk = onShowDisk
}

// SetOnSetup sets the function that offers to allow running smartctl with sudo, offered while the SMART values
// cannot be read without. Must be called on the UI thread.
func (poolDevices *PoolDevicesComponent) SetOnSetup(onSetup func()) {
	poolDevices.onSetup = onSetup
	poolDevices.render()
}

// SetEnabledFunc sets a function that returns whether the temperatures can be clicked, e.g. not while a dialog is
// in front (tview hands mouse events to all pages, see rivo/tview#926). Must be called on the UI thread.
func (poolDevices *PoolDevicesComponent) SetEnabledFunc(enabled func() bool) {
	poolDevices.enabled = enabled
}

// SetStatus shows the devices of the status, or none if it is nil. Must be called on the UI thread.
func (poolDevices *PoolDevicesComponent) SetStatus(status *zfs.PoolStatus) {
	poolDevices.status = status
	poolDevices.render()
}

// render shows the devices of the status with the readings. Runs on the UI thread.
func (poolDevices *PoolDevicesComponent) render() {
	status := poolDevices.status
	if status == nil {
		uiutil.SetupWindow(poolDevices.textView, "Devices")
		poolDevices.textView.SetText("")
		poolDevices.fields = nil
		poolDevices.footer.SetParts()
		return
	}
	uiutil.SetupWindow(poolDevices.textView, devicesTitle(status.Devices))
	// keep the scroll position while the status is refreshed
	row, column := poolDevices.textView.GetScrollOffset()
	text, fields := formatDevices(status.Devices, poolDevices.readings, poolDevices.smartReports)
	poolDevices.textView.SetText(text)
	poolDevices.textView.ScrollTo(row, column)
	poolDevices.fields = fields
	poolDevices.footer.SetParts(poolDevices.footerParts()...)
}

// footerParts returns the hints about the values that were not read: the temperatures of idle hard disks (a click
// reads them) and of disks without the kernel module drivetemp, and SMART values that need root (a click offers to
// allow it).
func (poolDevices *PoolDevicesComponent) footerParts() []uiutil.FooterPart {
	idle, needsDrivetemp, smartNeedsRoot := false, false, false
	for _, name := range zfs.LeafDevices(poolDevices.status.Devices) {
		reading := poolDevices.readings[name]
		idle = idle || reading.Idle && !reading.Known
		needsDrivetemp = needsDrivetemp || reading.NeedsDrivetemp
		for _, report := range poolDevices.smartReports[name] {
			smartNeedsRoot = smartNeedsRoot || errors.Is(report.Err, disk.ErrSmartNeedsRoot)
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
			Text:    txwidgets.Span(theme.Colors.Pool.ReadTemperature, "%s idle, not read: t", disk_browser.ReadTemperatureIcon),
			OnClick: poolDevices.ReadTemperatures,
		})
	}
	if smartNeedsRoot && poolDevices.onSetup != nil {
		add(uiutil.FooterPart{Text: txwidgets.Span(theme.Colors.Pool.ReadTemperature, "SMART needs root: allow"), OnClick: poolDevices.onSetup})
	}
	return parts
}

// hasSmart returns whether SMART values of any device were read, or tried to. Runs on the UI thread.
func (poolDevices *PoolDevicesComponent) hasSmart() bool {
	for _, reports := range poolDevices.smartReports {
		if len(reports) > 0 {
			return true
		}
	}
	return false
}

// ReadSmart reads the SMART values of all devices again, also if they were read recently. Disks that sleep are not
// woken up. Must be called on the UI thread.
func (poolDevices *PoolDevicesComponent) ReadSmart() {
	if poolDevices.status != nil {
		poolDevices.readSmart(zfs.LeafDevices(poolDevices.status.Devices), 0)
	}
}

// smartIfDue reads the SMART values of the devices if they changed, or the last read is older than smartInterval.
// Called while the devices are drawn, like sampleIfDue. Runs on the UI thread.
func (poolDevices *PoolDevicesComponent) smartIfDue() {
	if poolDevices.smart == nil || poolDevices.status == nil || poolDevices.smartLoader.IsLoading() {
		return
	}
	devices := zfs.LeafDevices(poolDevices.status.Devices)
	if slices.Equal(devices, poolDevices.smartDevices) && time.Since(poolDevices.smartReadAt) < smartInterval {
		return
	}
	poolDevices.readSmart(devices, smartInterval)
}

// readSmart reads the SMART values of the devices in the background, the ones older than maxAge. Runs on the UI
// thread.
func (poolDevices *PoolDevicesComponent) readSmart(devices []string, maxAge time.Duration) {
	if poolDevices.smart == nil {
		return
	}
	poolDevices.smartDevices = devices
	poolDevices.smartReadAt = time.Now()
	smart := poolDevices.smart
	poolDevices.smartLoader.LoadQuietly(func(ctx context.Context) (map[string][]disk.SmartReport, error) {
		return smart.ReadDevices(devices, maxAge), nil
	})
}

// ShowSmart shows the disks of the device with the given name ("" for the first) with their SMART values, see
// SetOnShowDisk. Must be called on the UI thread.
func (poolDevices *PoolDevicesComponent) ShowSmart(device string) {
	if poolDevices.onShowDisk == nil || poolDevices.status == nil {
		return
	}
	poolDevices.onShowDisk(poolDevices.status.Name, device)
}

// hasDisks returns whether any device is stored on a disk, whose temperature can be read. Runs on the UI thread.
func (poolDevices *PoolDevicesComponent) hasDisks() bool {
	for _, reading := range poolDevices.readings {
		if reading.IsDisk && !reading.NoSensor {
			return true
		}
	}
	return false
}

// ReadTemperatures reads the temperatures of all devices, also of idle hard disks. Must be called on the UI thread.
func (poolDevices *PoolDevicesComponent) ReadTemperatures() {
	if poolDevices.status == nil {
		return
	}
	devices := zfs.LeafDevices(poolDevices.status.Devices)
	poolDevices.sample(devices, devices)
}

// readTemperature reads the temperature of the device, also of an idle hard disk. Runs on the UI thread.
func (poolDevices *PoolDevicesComponent) readTemperature(device string) {
	if poolDevices.status == nil {
		return
	}
	poolDevices.sample(zfs.LeafDevices(poolDevices.status.Devices), []string{device})
}

// sampleIfDue samples the devices if they changed, or the last sample is older than sampleInterval. Called while
// the devices are drawn, so nothing is sampled while they are not shown. Runs on the UI thread.
func (poolDevices *PoolDevicesComponent) sampleIfDue() {
	if poolDevices.status == nil || poolDevices.loader.IsLoading() {
		return
	}
	devices := zfs.LeafDevices(poolDevices.status.Devices)
	// a timer may fire a little early
	if slices.Equal(devices, poolDevices.sampledDevices) && time.Since(poolDevices.sampledAt) < sampleInterval-time.Second {
		return
	}
	poolDevices.sample(devices, nil)
}

// sample samples the devices in the background, forced ones are read in any case. Runs on the UI thread.
func (poolDevices *PoolDevicesComponent) sample(devices []string, forced []string) {
	poolDevices.sampledDevices = devices
	poolDevices.sampledAt = time.Now()
	sampler := poolDevices.sampler
	poolDevices.loader.LoadQuietly(func(ctx context.Context) (map[string]disk.Reading, error) {
		return sampler.Sample(devices, forced), nil
	})
}

// scheduleSample draws again after sampleInterval, which samples the devices again if they are still shown (see
// sampleIfDue). Runs on the UI thread.
func (poolDevices *PoolDevicesComponent) scheduleSample() {
	if poolDevices.sampleTimer != nil {
		poolDevices.sampleTimer.Stop()
	}
	application := poolDevices.application
	poolDevices.sampleTimer = time.AfterFunc(sampleInterval, func() {
		application.QueueUpdateDraw(func() {})
	})
}

// Draw samples the devices if due (see sampleIfDue), draws them, and adds the temperatures that can be clicked.
func (view *DevicesView) Draw(screen tcell.Screen) {
	devices := view.devices
	devices.sampleIfDue()
	devices.smartIfDue()
	view.TextView.Draw(screen)

	devices.regions.Reset()
	defer devices.regions.DrawHover(screen)
	x, y, width, height := view.GetInnerRect()
	row, column := view.GetScrollOffset()
	for _, field := range devices.fields {
		fieldY := y + field.line - row
		if fieldY < y || fieldY >= y+height {
			continue
		}
		start := max(x+field.column-column, x)
		end := min(x+field.column-column+field.width, x+width)
		name := field.device
		onClick := func() { devices.readTemperature(name) }
		if field.smart {
			onClick = func() { devices.ShowSmart(name) }
		}
		devices.regions.Add(clickable.Region{
			X: start, Y: fieldY, Width: end - start, Key: field,
			HoverBackground: theme.Colors.Pool.HoverBackground,
			OnClick:         onClick,
		})
	}
}

// MouseHandler reads a clicked temperature (see clickable.Regions), or the ones of the footer, and otherwise
// scrolls and focuses like a text view.
func (view *DevicesView) MouseHandler() func(action tview.MouseAction, event *tcell.EventMouse, setFocus func(p tview.Primitive)) (consumed bool, capture tview.Primitive) {
	textViewHandler := view.TextView.MouseHandler()
	return func(action tview.MouseAction, event *tcell.EventMouse, setFocus func(p tview.Primitive)) (consumed bool, capture tview.Primitive) {
		devices := view.devices
		enabled := devices.enabled()
		if devices.footer.HandleMouse(action, event, enabled) {
			return true, nil
		}
		consumed, captures := devices.regions.HandleMouse(action, event, enabled && view.InRect(event.Position()))
		if !consumed {
			consumed, capture = textViewHandler(action, event, setFocus)
		}
		if capture == nil && captures {
			// to learn when the mouse leaves
			capture = view
		}
		return consumed, capture
	}
}

// GetShortcutMap returns the shortcuts of the devices: reading the temperatures and the SMART values, if there are
// disks.
func (poolDevices *PoolDevicesComponent) GetShortcutMap() []shortcut_helper.ShortcutEntry {
	var shortcuts []shortcut_helper.ShortcutEntry
	if poolDevices.hasSmart() {
		shortcuts = append(shortcuts,
			shortcut_helper.ShortcutEntry{
				KeyCombo: []string{shortcut_helper.KeyEnter}, Name: "Disk health", Group: shortcut_helper.GroupAction,
				Description: "Show the SMART values of the disks of the pool on the disks page (also a click on them)",
				Run:         func() { poolDevices.ShowSmart("") },
			},
			shortcut_helper.ShortcutEntry{
				KeyCombo: []string{"s"}, Name: "Read SMART", Group: shortcut_helper.GroupAction,
				Description: "Read the SMART values of the disks again (sleeping disks are not woken up)",
				Run:         poolDevices.ReadSmart,
			})
	}
	if poolDevices.hasDisks() {
		shortcuts = append(shortcuts, shortcut_helper.ShortcutEntry{
			KeyCombo: []string{"t"}, Name: "Read temperatures", Group: shortcut_helper.GroupAction,
			Description: "Read the temperatures of the disks, also of idle hard disks (reading may keep them from spinning down)",
			Run:         poolDevices.ReadTemperatures,
		})
	}
	return shortcuts
}

// devicesTitle returns the title of the devices, with the number of devices with errors, e.g. "Devices · 2 with
// errors".
func devicesTitle(devices []*zfs.PoolDevice) string {
	withErrors := 0
	for _, device := range devices {
		if device.HasErrors() {
			withErrors++
		}
	}
	if withErrors == 0 {
		return "Devices"
	}
	return fmt.Sprintf("Devices · %d with errors", withErrors)
}

// clickableField is where the temperature or the SMART values of a device are in the text of formatDevices.
type clickableField struct {
	device string
	// smart is set for the SMART values, else it is the temperature
	smart bool
	// line, column and width are the position in the text, in cells
	line   int
	column int
	width  int
}

// formatDevices formats the devices as a table: the tree of their names, their state, their errors, the
// temperature of their disks (if any device is stored on a disk, see readings), their health (if smartctl could
// run, see smart) and their note. It returns the fields that can be clicked as well.
func formatDevices(devices []*zfs.PoolDevice, readings map[string]disk.Reading, smart map[string][]disk.SmartReport) (string, []clickableField) {
	prefixes := devicePrefixes(devices)
	showTemperatures := false
	for _, reading := range readings {
		showTemperatures = showTemperatures || reading.IsDisk
	}
	showSmart := false
	smartWidth := len("SMART")
	for _, reports := range smart {
		text, _ := disk_browser.SmartSummary(reports)
		showSmart = showSmart || text != ""
		smartWidth = max(smartWidth, utf8.RuneCountInString(text))
	}
	isLeaf := map[string]bool{}
	for _, name := range zfs.LeafDevices(devices) {
		isLeaf[name] = true
	}
	var fields []clickableField

	nameWidth := len("NAME")
	stateWidth := len("STATE")
	counterWidth := len("CKSUM")
	for i, device := range devices {
		nameWidth = max(nameWidth, utf8.RuneCountInString(prefixes[i])+len(device.Name))
		stateWidth = max(stateWidth, len(device.State))
		for _, counter := range []uint64{device.Read, device.Write, device.Checksum} {
			counterWidth = max(counterWidth, len(strconv.FormatUint(counter, 10)))
		}
	}

	var out strings.Builder
	header := fmt.Sprintf(" %-*s  %-*s  %*s %*s %*s", nameWidth, "NAME", stateWidth, "STATE",
		counterWidth, "READ", counterWidth, "WRITE", counterWidth, "CKSUM")
	// behind the counters
	temperatureColumn := utf8.RuneCountInString(header) + 2
	smartColumn := temperatureColumn
	if showTemperatures {
		header += fmt.Sprintf("  %*s", temperatureWidth, "TEMP")
		smartColumn += temperatureWidth + 2
	}
	if showSmart {
		header += "  SMART"
	}
	out.WriteString("[::b]" + txwidgets.Span(theme.Colors.Layout.Table.HeaderForeground, "%s", header) + "[::-]\n")

	for i, device := range devices {
		out.WriteString(" " + txwidgets.Span(theme.Colors.Layout.Table.TreeLines, "%s", prefixes[i]))
		if device.IsGroup() {
			out.WriteString(txwidgets.Span(theme.Colors.Pool.Group, "%s", device.Name) + "\n")
			continue
		}

		padding := nameWidth - utf8.RuneCountInString(prefixes[i]) - len(device.Name)
		out.WriteString(txwidgets.Span(tcell.ColorWhite, "%s", device.Name) + strings.Repeat(" ", padding) + "  ")

		// the columns behind the counters; empty ones (and the padding of left-aligned ones) are only filled with
		// spaces if something follows
		trailing, gap := "", ""
		addColumn := func(cell string, cellWidth int, width int) {
			if cell == "" {
				gap += strings.Repeat(" ", 2+width)
				return
			}
			trailing += gap + "  " + cell
			gap = strings.Repeat(" ", width-cellWidth)
		}
		if showTemperatures {
			cell := ""
			if reading, ok := readings[device.Name]; ok && isLeaf[device.Name] {
				if text, color := disk_browser.FormatTemperature(reading); text != "" {
					cell = txwidgets.Span(color, "%*s", temperatureWidth, text)
					if reading.IsCritical() {
						cell = "[::l]" + cell + "[::-]"
					}
				}
				if reading.IsDisk && !reading.NoSensor {
					fields = append(fields, clickableField{device: device.Name, line: i + 1, column: temperatureColumn, width: temperatureWidth})
				}
			}
			addColumn(cell, temperatureWidth, temperatureWidth)
		}
		if showSmart {
			cell, cellWidth := "", 0
			if isLeaf[device.Name] {
				if text, color := disk_browser.SmartSummary(smart[device.Name]); text != "" {
					cell, cellWidth = txwidgets.Span(color, "%s", text), utf8.RuneCountInString(text)
					fields = append(fields, clickableField{device: device.Name, smart: true, line: i + 1, column: smartColumn, width: smartWidth})
				}
			}
			addColumn(cell, cellWidth, smartWidth)
		}
		if device.Note != "" {
			trailing += gap + "  " + txwidgets.Span(noteColor(device), "%s", device.Note)
		}

		// like "zpool status": spares have no error counters, what follows is aligned with the others
		counters := ""
		if !isSpare(device) || device.HasErrors() {
			for _, counter := range []uint64{device.Read, device.Write, device.Checksum} {
				counters += " " + txwidgets.Span(counterColor(counter), "%*d", counterWidth, counter)
			}
		} else if trailing != "" {
			counters = strings.Repeat(" ", 3*(counterWidth+1))
		}

		state := device.State
		if counters != "" {
			state = fmt.Sprintf("%-*s ", stateWidth, state)
		}
		out.WriteString(txwidgets.Span(StateColor(device.State, tcell.ColorWhite), "%s", state) + counters + trailing + "\n")
	}
	return out.String(), fields
}

// isSpare returns whether the device is a hot spare, by its state.
func isSpare(device *zfs.PoolDevice) bool {
	return device.State == zfs.PoolStateAvail || device.State == zfs.PoolStateInUse
}

// counterColor returns the color of an error counter: whether there were errors.
func counterColor(counter uint64) tcell.Color {
	if counter > 0 {
		return theme.Colors.Pool.Errors
	}
	return theme.Colors.Pool.NoErrors
}

// noteColor returns the color of the note of a device, e.g. "(resilvering)": the color of its state, unless it is
// online, so the notes of healthy devices do not look like a problem.
func noteColor(device *zfs.PoolDevice) tcell.Color {
	if device.State == zfs.PoolStateOnline {
		return theme.Colors.Pool.Degraded
	}
	return StateColor(device.State, tcell.ColorWhite)
}

// devicePrefixes returns the tree lines in front of the name of each device, e.g. "│  ├─ ". The pool and the
// groups (depth 0) are the roots.
func devicePrefixes(devices []*zfs.PoolDevice) []string {
	prefixes := make([]string, len(devices))
	// hasNextSibling[depth] is whether the last device seen on that depth has a sibling below it
	var hasNextSibling []bool
	for i, device := range devices {
		for len(hasNextSibling) < device.Depth {
			hasNextSibling = append(hasNextSibling, false)
		}
		hasNextSibling = append(hasNextSibling[:device.Depth], !isLastSibling(devices, i))
		if device.Depth == 0 {
			continue
		}

		var prefix strings.Builder
		for depth := 1; depth < device.Depth; depth++ {
			if hasNextSibling[depth] {
				prefix.WriteString(treeLine)
			} else {
				prefix.WriteString(treeSpace)
			}
		}
		if hasNextSibling[device.Depth] {
			prefix.WriteString(treeBranch)
		} else {
			prefix.WriteString(treeLastBranch)
		}
		prefixes[i] = prefix.String()
	}
	return prefixes
}

// isLastSibling returns whether no sibling follows the device at index: the next device that is not a descendant
// is on a higher level (or there is none).
func isLastSibling(devices []*zfs.PoolDevice, index int) bool {
	depth := devices[index].Depth
	for _, device := range devices[index+1:] {
		if device.Depth <= depth {
			return device.Depth < depth
		}
	}
	return true
}

func (poolDevices *PoolDevicesComponent) HasFocus() bool {
	return poolDevices.textView.HasFocus()
}

func (poolDevices *PoolDevicesComponent) Focus() {
	poolDevices.application.SetFocus(poolDevices.textView)
}

func (poolDevices *PoolDevicesComponent) GetLayout() *DevicesView {
	return poolDevices.textView
}

// ContentSize returns the size needed to show all of the text, including the border. Must be called on the UI
// thread.
func (poolDevices *PoolDevicesComponent) ContentSize() (width int, height int) {
	return uiutil.TextViewContentSize(poolDevices.textView.TextView)
}
