package disk_browser

import (
	"errors"
	"fmt"
	"strings"
	"zfs-file-history/internal/disk"
	"zfs-file-history/internal/ui/theme"
	"zfs-file-history/internal/ui/txwidgets"
	uiutil "zfs-file-history/internal/ui/util"

	"github.com/dustin/go-humanize"
	"github.com/gdamore/tcell/v2"
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

// formatDetails returns the lines of the details of a disk: what it is, its health and its problems. The attributes
// are in a table of their own.
func formatDetails(entry *DiskEntry) []string {
	report := entry.Report
	name := entry.DeviceName()
	if report.Disk != name {
		name += " → " + report.Disk
	}
	lines := []string{"[::b]" + txwidgets.Span(theme.Colors.Dialog.Highlight, "%s", name) + "[::-]" +
		txwidgets.Span(theme.Colors.Pool.Hint, "  in %s", entry.Pool)}

	field := func(label string, value string) {
		if label != "" {
			label += ":"
		}
		lines = append(lines, txwidgets.Span(theme.Colors.Layout.Table.Accent, "  %-9s", label)+" "+value)
	}
	// the model and so on in a line of their own, the name of the device can be long
	var details []string
	if report.Serial != "" {
		details = append(details, "serial "+report.Serial)
	}
	if report.Capacity > 0 {
		details = append(details, humanize.Bytes(uint64(report.Capacity)))
	}
	if report.Firmware != "" {
		details = append(details, "firmware "+report.Firmware)
	}
	if report.Protocol != "" {
		details = append(details, report.Protocol)
	}
	if report.Model != "" || len(details) > 0 {
		value := txwidgets.Span(tcell.ColorWhite, "%s", report.Model)
		if len(details) > 0 {
			if report.Model != "" {
				value += "  "
			}
			value += txwidgets.Span(theme.Colors.Pool.Hint, "%s", strings.Join(details, " · "))
		}
		field("Disk", value)
	}
	switch {
	case errors.Is(report.Err, disk.ErrSmartctlMissing):
		field("SMART", txwidgets.Span(theme.Colors.Pool.Hint, "not read: smartctl is not installed (smartmontools)"))
		return lines
	case errors.Is(report.Err, disk.ErrSmartNeedsRoot):
		field("SMART", txwidgets.Span(theme.Colors.Dialog.Error, "only root may read them: allow \"Disk health\" in the command menu (:)"))
		return lines
	case report.Err != nil:
		field("Error", txwidgets.Span(theme.Colors.Dialog.Error, "%s", report.Err.Error()))
		return lines
	case !report.HasValues():
		field("SMART", txwidgets.Span(theme.Colors.Pool.Hint, "asleep, not woken up to read it"))
		return lines
	}

	health, color := formatHealth(report)
	field("Health", txwidgets.Span(color, "%s", health))
	if report.PowerOnHours >= 0 {
		field("Power on", txwidgets.Span(tcell.ColorWhite, "%d h (%.1f years)", report.PowerOnHours, float64(report.PowerOnHours)/(24*365)))
	}
	read := uiutil.FormatTime(report.ReadAt)
	if report.Standby {
		// not woken up to read it again
		read += ", asleep now"
	}
	field("Read", txwidgets.Span(theme.Colors.Pool.Hint, "%s", read))
	for i, problem := range report.Problems {
		label := ""
		if i == 0 {
			label = "Problems"
		}
		field(label, txwidgets.Span(severityColor(problem.Severity), "%s", problem.Text))
	}
	return lines
}
