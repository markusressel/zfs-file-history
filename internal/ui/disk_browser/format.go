package disk_browser

import (
	"cmp"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"zfs-file-history/internal/disk"
	"zfs-file-history/internal/ui/table"
	"zfs-file-history/internal/ui/theme"
	"zfs-file-history/internal/ui/txwidgets"
	uiutil "zfs-file-history/internal/ui/util"

	"github.com/dustin/go-humanize"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// ReadTemperatureIcon is shown instead of the temperature of an idle hard disk that was not read, see
// disk.Reading.Idle. A click (or t) reads it.
const ReadTemperatureIcon = "⟳"

const (
	// coolTemperature is where the gradient of the temperatures starts, a disk at room temperature
	coolTemperature = 25.0
	// defaultWarningTemperature is where it ends for disks that do not report a temperature at which they are too
	// hot (most hard disks are specified up to 60 °C)
	defaultWarningTemperature = 60.0
)

// FormatTemperature returns the text and color of the temperature of a device: colored by how close it is to the
// temperature at which its disk is too hot, gray if it may be outdated (an idle hard disk), the icon to read an idle
// hard disk that was not read yet.
func FormatTemperature(reading disk.Reading) (string, tcell.Color) {
	switch {
	case !reading.IsDisk:
		return "", tcell.ColorDefault
	case reading.NoSensor:
		return "–", theme.Colors.Pool.TemperatureUnknown
	case !reading.Known && reading.Idle:
		return ReadTemperatureIcon, theme.Colors.Pool.ReadTemperature
	case !reading.Known:
		return "", tcell.ColorDefault
	}
	text := fmt.Sprintf("%.0f°C", reading.Temperature.Celsius)
	if reading.Idle {
		return text, theme.Colors.Pool.TemperatureIdle
	}
	return text, uiutil.GradientColor(theme.Colors.Pool.Temperature, temperaturePosition(reading.Temperature), tcell.ColorWhite)
}

// temperaturePosition returns the position of the temperature in theme.Colors.Pool.Temperature: 0 for a cool disk,
// 1 at its critical temperature (where it blinks and raises an alert), or else its warning temperature. Not the
// warning temperature first: some disks report a recommended one far below (e.g. Seagate IronWolf Pro: 40 °C, critical
// 60 °C), which would show normal temperatures as too hot.
func temperaturePosition(temperature disk.Temperature) float64 {
	warning := temperature.Critical
	if warning <= coolTemperature {
		warning = temperature.Warning
	}
	if warning <= coolTemperature {
		warning = defaultWarningTemperature
	}
	return min(max((temperature.Celsius-coolTemperature)/(warning-coolTemperature), 0), 1)
}

// SmartSummary returns the text and color of the SMART values of the disks of a device: the most severe problem
// (e.g. "8 realloc"), "OK", why they could not be read, or "asleep" for a disk that slept whenever it was asked. ""
// if there is nothing to show (no disks, not read yet, or smartctl is not installed).
func SmartSummary(reports []disk.SmartReport) (string, tcell.Color) {
	if problem, ok := disk.WorstProblem(reports); ok {
		return problem.Short, severityColor(problem.Severity)
	}
	text, color := "", tcell.ColorDefault
	for _, report := range reports {
		switch {
		case errors.Is(report.Err, disk.ErrSmartctlMissing):
		case errors.Is(report.Err, disk.ErrSmartNeedsRoot):
			return "?", theme.Colors.Pool.Hint
		case report.Err != nil:
			return "error", theme.Colors.Pool.Hint
		case !report.HasValues() && report.Standby:
			text, color = "asleep", theme.Colors.Pool.Hint
		case report.HasValues() && text == "":
			text, color = "OK", theme.Colors.Pool.Online
		}
	}
	return text, color
}

// severityColor returns the color of a problem of the given severity.
func severityColor(severity disk.SmartSeverity) tcell.Color {
	if severity == disk.SmartError {
		return theme.Colors.Pool.Faulted
	}
	return theme.Colors.Pool.Degraded
}

// formatHealth returns the text and color of the result of the health check of the disk, "" if it was not read.
func formatHealth(report disk.SmartReport) (string, tcell.Color) {
	switch {
	case report.Err != nil || !report.HasValues():
		return "", tcell.ColorDefault
	case !report.HealthKnown:
		return "unknown", theme.Colors.Pool.Hint
	case report.Passed:
		return "PASSED", theme.Colors.Pool.Online
	default:
		return "FAILED", theme.Colors.Pool.Faulted
	}
}

// detailsBarWidth is the width of the bars of the temperature and the wear in the details of a disk
const detailsBarWidth = 12

// detailsTitle returns the title of the details of a disk: its name and model.
func detailsTitle(entry *DiskEntry) string {
	if entry.Report.Model == "" {
		return entry.Report.Disk
	}
	return entry.Report.Disk + " · " + entry.Report.Model
}

// detailsColumnGap separates the columns of the details while they are side by side, see formatDetails.
const detailsColumnGap = 6

// formatDetails returns the lines of the details of a disk (see uiutil.InfoText): whether it is healthy, then what it
// is next to its health, or above it if both do not fit side by side into width columns (e.g. a small terminal, where
// the health was cut off). Lines still wider are wrapped; a width of 0 or less does neither. Its name and model are
// the title of the pane, the attributes are in a table of their own.
func formatDetails(entry *DiskEntry, width int) []string {
	lines, _ := formatDetailsWithPoolLinks(entry, width)
	return lines
}

// poolLink is where the name of a pool the disk is used by is in the lines of formatDetails (line and column, without
// scrolling), which shows the pool on the pools page when clicked.
type poolLink struct {
	pool         string
	line, column int
	width        int
}

// formatDetailsWithPoolLinks returns the lines of formatDetails, and where the names of the pools are in them.
func formatDetailsWithPoolLinks(entry *DiskEntry, width int) ([]string, []poolLink) {
	var info uiutil.InfoText
	info.Verdict(detailsVerdict(entry)...)

	var what, health uiutil.InfoText
	// poolsLine is the line of info of the first pool (the others follow), poolsColumn where its value starts in it
	var poolsLine, poolsColumn int
	if width <= 0 || sideBySideWidth(entry) <= width {
		info.Line("")
		poolsLine = info.LineCount() + addDiskSection(&what, entry)
		addHealthSection(&health, entry)
		// the lines of both are lines of info of their own, not fields
		poolsColumn = what.ValueColumn()
		info.SideBySide(detailsColumnGap, &what, &health)
	} else {
		// one column, the labels of both sections aligned
		poolsLine = addDiskSection(&info, entry)
		addHealthSection(&info, entry)
	}

	lines, positions := info.Wrap(width)
	var links []poolLink
	for i, device := range entry.Devices {
		// a wrapped line above moves it down, a narrow pane below its label
		position := positions[poolsLine+i]
		link := poolLink{pool: device.Pool, line: position.Line, column: position.Column + poolsColumn,
			width: tview.TaggedStringWidth(tview.Escape(device.Pool))}
		// the name is not wrapped: it comes first, and is short
		if width > 0 {
			link.width = min(link.width, width-link.column)
		}
		links = append(links, link)
	}
	return lines, links
}

// sideBySideWidth returns the width of what the disk is next to its health, see formatDetails.
func sideBySideWidth(entry *DiskEntry) int {
	var what, health uiutil.InfoText
	addDiskSection(&what, entry)
	addHealthSection(&health, entry)
	return what.Width() + detailsColumnGap + health.Width()
}

// addDiskSection adds what the disk is: its type, where it is used, serial number, firmware and size. It returns the
// line of the first pool, the one of each device of the entry follows.
func addDiskSection(what *uiutil.InfoText, entry *DiskEntry) (poolsLine int) {
	report := entry.Report
	what.Section("Disk")
	what.Field("Type", uiutil.InfoValue(typeOf(report)))
	var pools []string
	for _, device := range entry.Devices {
		pools = append(pools, uiutil.InfoValue(device.Pool)+"  "+uiutil.InfoMuted(device.DisplayName()))
	}
	poolsLine = what.LineCount()
	if len(pools) > 0 {
		what.Field("Pools", strings.Join(pools, "\n"))
	}
	if report.Serial != "" {
		what.Field("Serial", uiutil.InfoValue(report.Serial))
	}
	if report.Firmware != "" {
		what.Field("Firmware", uiutil.InfoValue(report.Firmware))
	}
	if report.Capacity > 0 {
		what.Field("Size", uiutil.InfoValue(humanize.Bytes(uint64(report.Capacity))))
	}
	return poolsLine
}

// addHealthSection adds the health of the disk: its temperature and what its SMART values say, or why they are not
// known.
func addHealthSection(health *uiutil.InfoText, entry *DiskEntry) {
	report := entry.Report
	health.Section("Health")
	if temperature := formatDetailsTemperature(entry.Reading); temperature != "" {
		health.Field("Temp", temperature)
	}
	switch {
	case errors.Is(report.Err, disk.ErrSmartctlMissing):
		health.Field("SMART", uiutil.InfoMuted("not read: smartctl is not installed (smartmontools)"))
	case errors.Is(report.Err, disk.ErrSmartNeedsRoot):
		health.Field("SMART", txwidgets.Span(theme.Colors.Dialog.Error, "only root may read them: allow \"Disk health\" in the command menu (:)"))
	case report.Err != nil:
		health.Field("Error", txwidgets.Span(theme.Colors.Dialog.Error, "%s", report.Err.Error()))
	case !report.HasValues():
		health.Field("SMART", uiutil.InfoMuted("asleep, not woken up to read it"))
	default:
		addHealthFields(health, report)
	}
}

// addHealthFields adds the health of the disk, read from its SMART values: the result of its health check, its wear,
// age and problems.
func addHealthFields(health *uiutil.InfoText, report disk.SmartReport) {
	text, color := formatHealth(report)
	health.Field("Check", txwidgets.Span(color, "%s", text))
	if wear := report.Counters.WearPercent; wear >= 0 {
		health.Field("Wear", uiutil.InfoBar(float64(wear)/100, detailsBarWidth)+" "+uiutil.InfoValue(fmt.Sprintf("%d%%", wear))+
			uiutil.InfoMuted(" of its rated endurance"))
	}
	if report.PowerOnHours >= 0 {
		health.Field("Age", uiutil.InfoValue(fmt.Sprintf("%.1f years", float64(report.PowerOnHours)/(24*365)))+
			uiutil.InfoMuted(" · "+humanize.Comma(report.PowerOnHours)+" h"))
	}
	if written := report.Counters.BytesWritten; written >= 0 {
		health.Field("Written", uiutil.InfoValue(humanize.Bytes(uint64(written))))
	}
	for i, problem := range report.Problems {
		label := ""
		if i == 0 {
			label = "Problems"
		}
		health.Field(label, txwidgets.Span(severityColor(problem.Severity), "%s", problem.Text))
	}
	read := uiutil.FormatTime(report.ReadAt)
	if report.Standby {
		// not woken up to read it again
		read += ", asleep now"
	}
	health.Field("Read", uiutil.InfoMuted(read))
}

// formatDetailsTemperature returns the temperature of the disk in its details: a bar up to the temperature at which it
// is too hot (critical), "" if it has none.
func formatDetailsTemperature(reading disk.Reading) string {
	switch {
	case !reading.IsDisk:
		return ""
	case reading.NoSensor:
		return uiutil.InfoMuted("no sensor")
	case !reading.Known && reading.Idle:
		return uiutil.InfoMuted("idle, not read: t")
	case !reading.Known:
		return ""
	}
	text, color := FormatTemperature(reading)
	value := uiutil.InfoBar(temperaturePosition(reading.Temperature), detailsBarWidth) + " " + txwidgets.Span(color, "%s", text)
	// the bar ends where the disk is too hot: not a target, a limit
	switch critical := reading.Temperature.Critical; {
	case critical <= 0:
	case reading.Temperature.Celsius >= critical:
		value += txwidgets.Span(uiutil.InfoError.Color(), " · %.0f°C above critical (%.0f°C)", reading.Temperature.Celsius-critical, critical)
	default:
		value += uiutil.InfoMuted(fmt.Sprintf(" · critical at %.0f°C", critical))
	}
	if reading.Idle {
		value += uiutil.InfoMuted(", idle")
	}
	return value
}

// detailsVerdict returns the parts of the verdict of a disk: its problems (or that it is healthy, or why that is not
// known), then where it is.
func detailsVerdict(entry *DiskEntry) []uiutil.InfoPart {
	report := entry.Report
	var parts []uiutil.InfoPart
	if entry.Reading.IsCritical() {
		parts = append(parts, uiutil.InfoPart{Text: "too hot", State: uiutil.InfoError})
	}
	switch {
	case errors.Is(report.Err, disk.ErrSmartNeedsRoot):
		parts = append(parts, uiutil.InfoPart{Text: "health unknown: SMART needs root", State: uiutil.InfoUnknown})
	case report.Err != nil:
		parts = append(parts, uiutil.InfoPart{Text: "health unknown", State: uiutil.InfoUnknown})
	case !report.HasValues():
		parts = append(parts, uiutil.InfoPart{Text: "health unknown: asleep", State: uiutil.InfoUnknown})
	case report.HealthKnown && !report.Passed && len(report.Problems) == 0:
		// the problems say so otherwise, see disk.SmartReport.Problems
		parts = append(parts, uiutil.InfoPart{Text: "health check failed", State: uiutil.InfoError})
	case len(report.Problems) == 0:
		parts = append(parts, uiutil.InfoPart{Text: "healthy", State: uiutil.InfoOk})
	}
	for _, problem := range report.Problems {
		state := uiutil.InfoWarning
		if problem.Severity == disk.SmartError {
			state = uiutil.InfoError
		}
		parts = append(parts, uiutil.InfoPart{Text: problem.Text, State: state})
	}
	if pools := entry.PoolNames(); pools != "" {
		parts = append(parts, uiutil.InfoPart{Text: typeOf(report) + " in " + pools})
	}
	return parts
}

// isHardDisk returns whether the disk spins. NVMe drives never do: the kernel reports unknown ones as rotational.
func isHardDisk(report disk.SmartReport) bool {
	return report.Rotational && report.Protocol != "NVMe"
}

// typeOf returns the type of the disk, e.g. "SATA HDD" or "NVMe SSD": hard disk or SSD from the kernel, known also if
// the SMART values could not be read.
func typeOf(report disk.SmartReport) string {
	kind := "SSD"
	if isHardDisk(report) {
		kind = "HDD"
	}
	switch report.Protocol {
	case "ATA":
		return "SATA " + kind
	case "SCSI":
		return "SAS " + kind
	case "":
		return kind
	default:
		return report.Protocol + " " + kind
	}
}

// counterOf returns the value of the column of the counters of the disk for sorting, -1 if it is not known.
func counterOf(column *table.Column, report disk.SmartReport) int64 {
	if !report.HasValues() {
		return -1
	}
	counters := report.Counters
	switch column {
	case columnBad:
		return counters.BadSectors
	case columnUncorrectable:
		return counters.Uncorrectable
	case columnCrc:
		return counters.CrcErrors
	case columnWear:
		return counters.WearPercent
	case columnWritten:
		return counters.BytesWritten
	case columnPowerCycles:
		return counters.PowerCycles
	case columnUnsafe:
		return counters.UnsafeShutdowns
	case columnLoadCycles:
		return counters.LoadCycles
	}
	return -1
}

// formatCounter returns the text and color of the value of the column of the counters of the disk: "" if not known,
// errors other than 0 in the color of warnings (bad sectors as soon as there are any: they predict failures), the
// wear from the level at which it is a problem, 0 dimmed so the others stand out.
func formatCounter(column *table.Column, report disk.SmartReport) (string, tcell.Color) {
	value := counterOf(column, report)
	switch {
	case value < 0:
		return "", tcell.ColorDefault
	case column == columnWritten:
		return humanize.Bytes(uint64(value)), tcell.ColorLightGray
	case column == columnWear:
		color := tcell.ColorLightGray
		if value >= disk.WornWarningPercent {
			color = theme.Colors.Pool.Degraded
		}
		return fmt.Sprintf("%d%%", value), color
	case value == 0:
		return "0", theme.Colors.Layout.Table.ZeroSize
	case column == columnBad || column == columnUncorrectable || column == columnCrc:
		return humanize.Comma(value), theme.Colors.Pool.Degraded
	default:
		return humanize.Comma(value), tcell.ColorLightGray
	}
}

// lettersDiskName matches the kernel names of disks numbered by letters: "sda" to "sdz", then "sdaa".
var lettersDiskName = regexp.MustCompile(`^(sd|vd|hd|xvd)([a-z]+)$`)

// compareDiskNames orders the kernel names of disks like lsblk: "sdb" before "sdaa", "nvme2n1" before "nvme10n1".
func compareDiskNames(a string, b string) int {
	if matchA, matchB := lettersDiskName.FindStringSubmatch(a), lettersDiskName.FindStringSubmatch(b); matchA != nil && matchB != nil && matchA[1] == matchB[1] {
		return cmp.Or(cmp.Compare(len(matchA[2]), len(matchB[2])), cmp.Compare(matchA[2], matchB[2]))
	}
	// numbers by their value
	for a != "" && b != "" {
		numberA, restA := leadingNumber(a)
		numberB, restB := leadingNumber(b)
		switch {
		case numberA >= 0 && numberB >= 0:
			if result := cmp.Compare(numberA, numberB); result != 0 {
				return result
			}
			a, b = restA, restB
		case a[0] != b[0]:
			return cmp.Compare(a[0], b[0])
		default:
			a, b = a[1:], b[1:]
		}
	}
	return cmp.Compare(len(a), len(b))
}

// leadingNumber returns the number the text starts with and the rest, -1 if it starts with none.
func leadingNumber(text string) (int, string) {
	end := 0
	for end < len(text) && text[end] >= '0' && text[end] <= '9' {
		end++
	}
	number, err := strconv.Atoi(text[:end])
	if end == 0 || err != nil {
		return -1, text
	}
	return number, text[end:]
}
