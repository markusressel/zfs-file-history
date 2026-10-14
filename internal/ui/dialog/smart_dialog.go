package dialog

import (
	"errors"
	"fmt"
	"strings"
	"zfs-file-history/internal/disk"
	"zfs-file-history/internal/ui/theme"
	"zfs-file-history/internal/ui/txwidgets"
	"zfs-file-history/internal/ui/util"
	"zfs-file-history/internal/zfs"

	"github.com/dustin/go-humanize"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

const (
	SmartDialogPage util.Page = "SmartDialog"
)

// smartDialogWidth is the width of the content of the dialog, see DialogSizeConstraints.ExtraContentWidth.
const smartDialogWidth = 90

// SmartDevice is a device of a pool and the SMART reports of its disks, see NewSmartDialog.
type SmartDevice struct {
	// Name is the name of the device in "zpool status"
	Name    string
	Reports []disk.SmartReport
}

// SmartDialog shows the SMART values of the disks of a pool: their health, their problems, and their attributes
// (the ones that tell of failures highlighted).
type SmartDialog struct {
	layout        *tview.Flex
	actionChannel chan DialogActionId
	textView      *tview.TextView
}

// NewSmartDialog shows the SMART values of the devices of the pool, scrolled to the one with the name focus (""
// for the first one). onRead is called on the UI thread when the user asks to read them again (s), nil: not offered.
// Must be called on the UI thread.
func NewSmartDialog(pool string, devices []SmartDevice, focus string, onRead func()) *SmartDialog {
	d := &SmartDialog{actionChannel: make(chan DialogActionId)}
	title := " Disk health: " + pool + " "

	d.textView = tview.NewTextView().SetDynamicColors(true).SetWrap(false).SetScrollable(true)
	text, lines := formatSmartDevices(devices)
	d.textView.SetText(text)
	if line, ok := lines[focus]; ok {
		d.textView.ScrollTo(line, 0)
	}

	hint := "Press 'esc' to close"
	if onRead != nil {
		hint = "'s' to read again, 'esc' to close"
	}
	content := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(d.textView, 0, 1, true).
		AddItem(util.CreateAttentionTextView(hint), 1, 0, false)
	content.SetBorderPadding(0, 0, 1, 1)

	_, height := util.TextSize(text)
	layout := createModal(title, content, DialogSizeConstraints{
		Title:             title,
		ExtraContentWidth: smartDialogWidth,
		StaticHeight:      min(height, 30) + 3,
	})
	layout.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		switch {
		case event.Key() == tcell.KeyEscape || (event.Key() == tcell.KeyRune && event.Rune() == 'q'):
			d.Close()
			return nil
		case event.Key() == tcell.KeyRune && event.Rune() == 's' && onRead != nil:
			d.Close()
			onRead()
			return nil
		}
		return event
	})
	d.layout = layout
	return d
}

// formatSmartDevices formats the SMART values of the devices, one section each. It returns the line of the section
// of each device as well.
func formatSmartDevices(devices []SmartDevice) (string, map[string]int) {
	var out strings.Builder
	lines := map[string]int{}
	line := 0
	write := func(text string) {
		out.WriteString(text + "\n")
		line++
	}
	for _, device := range devices {
		if len(device.Reports) == 0 {
			continue
		}
		if line > 0 {
			write("")
		}
		lines[device.Name] = line
		for _, report := range device.Reports {
			for _, text := range formatSmartReport(device.Name, report) {
				write(text)
			}
		}
	}
	if line == 0 {
		write(txwidgets.Span(theme.Colors.Pool.Hint, "No device of the pool is stored on a disk."))
	}
	return out.String(), lines
}

// formatSmartReport returns the lines of the report of a disk of the device.
func formatSmartReport(device string, report disk.SmartReport) []string {
	name := zfs.DeviceDisplayName(device)
	if report.Disk != name {
		name += " → " + report.Disk
	}
	header := Highlight(name)
	if report.Model != "" {
		header += txwidgets.Span(tcell.ColorWhite, "  %s", report.Model)
	}
	if report.Serial != "" {
		header += txwidgets.Span(theme.Colors.Pool.Hint, "  %s", report.Serial)
	}
	if report.Capacity > 0 {
		header += txwidgets.Span(theme.Colors.Pool.Hint, "  %s", humanize.Bytes(uint64(report.Capacity)))
	}
	if report.Firmware != "" {
		header += txwidgets.Span(theme.Colors.Pool.Hint, "  firmware %s", report.Firmware)
	}
	if report.Protocol != "" {
		header += txwidgets.Span(theme.Colors.Pool.Hint, "  %s", report.Protocol)
	}
	lines := []string{"[::b]" + header + "[::-]"}

	field := func(label string, value string) {
		lines = append(lines, txwidgets.Span(theme.Colors.Layout.Table.Accent, "  %-9s", label+":")+" "+value)
	}
	if report.Err != nil {
		text := report.Err.Error()
		if errors.Is(report.Err, disk.ErrSmartNeedsRoot) {
			text = "only root may read them: allow \"Disk health\" in the command menu (:)"
		}
		field("Error", txwidgets.Span(theme.Colors.Dialog.Error, "%s", text))
		return lines
	}
	if !report.HasValues() {
		field("State", txwidgets.Span(theme.Colors.Pool.Hint, "asleep, not woken up to read it"))
		return lines
	}

	switch {
	case !report.HealthKnown:
		field("Health", txwidgets.Span(theme.Colors.Pool.Hint, "unknown"))
	case report.Passed:
		field("Health", txwidgets.Span(theme.Colors.Pool.Online, "PASSED"))
	default:
		field("Health", txwidgets.Span(theme.Colors.Pool.Faulted, "FAILED"))
	}
	if report.PowerOnHours >= 0 {
		field("Power on", txwidgets.Span(tcell.ColorWhite, "%d h (%.1f years)", report.PowerOnHours, float64(report.PowerOnHours)/(24*365)))
	}
	read := util.FormatTime(report.ReadAt)
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
		lines = append(lines, txwidgets.Span(theme.Colors.Layout.Table.Accent, "  %-9s", labelWithColon(label))+" "+
			txwidgets.Span(smartSeverityColor(problem.Severity), "%s", problem.Text))
	}

	if len(report.Attributes) > 0 {
		lines = append(lines, formatSmartAttributes(report.Attributes)...)
	}
	return lines
}

// labelWithColon returns the label of a field with a colon, "" for none (a further line of the field).
func labelWithColon(label string) string {
	if label == "" {
		return ""
	}
	return label + ":"
}

// formatSmartAttributes returns the lines of a table of the attributes: the ones that tell of failures highlighted,
// failing ones in the color of errors.
func formatSmartAttributes(attributes []disk.SmartAttribute) []string {
	nameWidth := len("ATTRIBUTE")
	ata := false
	for _, attribute := range attributes {
		nameWidth = max(nameWidth, len(attribute.Name))
		ata = ata || attribute.Id > 0
	}
	var lines []string
	header := fmt.Sprintf("  %-*s  %s", nameWidth, "ATTRIBUTE", "VALUE")
	if ata {
		header = fmt.Sprintf("  %3s  %-*s  %5s %5s %6s  %s", "ID", nameWidth, "ATTRIBUTE", "NORM", "WORST", "THRESH", "RAW")
	}
	lines = append(lines, txwidgets.Span(theme.Colors.Layout.Table.HeaderForeground, "%s", header))
	for _, attribute := range attributes {
		color := tcell.ColorLightGray
		switch {
		case attribute.Failed == "now":
			color = theme.Colors.Pool.Faulted
		case attribute.Important:
			color = tcell.ColorWhite
		}
		var text string
		if attribute.Id > 0 {
			text = fmt.Sprintf("  %3d  %-*s  %5d %5d %6d  %s", attribute.Id, nameWidth, attribute.Name,
				attribute.Normalized, attribute.Worst, attribute.Threshold, attribute.Value)
		} else {
			text = fmt.Sprintf("  %-*s  %s", nameWidth, attribute.Name, attribute.Value)
		}
		if attribute.Failed != "" {
			text += "  (failed " + attribute.Failed + ")"
		}
		if attribute.Important {
			text = "[::b]" + txwidgets.Span(color, "%s", text) + "[::-]"
		} else {
			text = txwidgets.Span(color, "%s", text)
		}
		lines = append(lines, text)
	}
	return lines
}

// smartSeverityColor returns the color of a problem of the given severity.
func smartSeverityColor(severity disk.SmartSeverity) tcell.Color {
	if severity == disk.SmartError {
		return theme.Colors.Pool.Faulted
	}
	return theme.Colors.Pool.Degraded
}

func (d *SmartDialog) GetName() string {
	return string(SmartDialogPage)
}

func (d *SmartDialog) GetLayout() *tview.Flex {
	return d.layout
}

func (d *SmartDialog) GetActionChannel() <-chan DialogActionId {
	return d.actionChannel
}

// GetText returns the text shown, without style tags if stripTags. Must be called on the UI thread.
func (d *SmartDialog) GetText(stripTags bool) string {
	return d.textView.GetText(stripTags)
}

func (d *SmartDialog) Close() {
	go func() {
		d.actionChannel <- DialogCloseActionId
	}()
}
