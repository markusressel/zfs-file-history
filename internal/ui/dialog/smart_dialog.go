package dialog

import (
	"cmp"
	"errors"
	"fmt"
	"slices"
	"strings"
	"zfs-file-history/internal/disk"
	"zfs-file-history/internal/ui/shortcut_helper"
	"zfs-file-history/internal/ui/table"
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

	// smartDialogWidth is the width of the content of the dialog, see DialogSizeConstraints.ExtraContentWidth.
	smartDialogWidth = 90
	// smartShortcutLines is the height of the shortcut map
	smartShortcutLines = 1
	// smartMaxDiskRows is the most rows of disks shown before the table scrolls, so the attributes keep their space
	smartMaxDiskRows = 8
)

var (
	smartDiskColumnDevice = &table.Column{Id: 0, Key: "device", Title: "Device", Alignment: tview.AlignLeft}
	smartDiskColumnDisk   = &table.Column{Id: 1, Key: "disk", Title: "Disk", Alignment: tview.AlignLeft}
	smartDiskColumnModel  = &table.Column{Id: 2, Key: "model", Title: "Model", Alignment: tview.AlignLeft}
	smartDiskColumnHealth = &table.Column{Id: 3, Key: "health", Title: "Health", Alignment: tview.AlignLeft}
	smartDiskColumnSmart  = &table.Column{Id: 4, Key: "smart", Title: "SMART", Alignment: tview.AlignLeft}
	smartDiskColumnHours  = &table.Column{Id: 5, Key: "hours", Title: "Hours", Alignment: tview.AlignRight}
	smartDiskColumns      = []*table.Column{smartDiskColumnDevice, smartDiskColumnDisk, smartDiskColumnModel,
		smartDiskColumnHealth, smartDiskColumnSmart, smartDiskColumnHours}

	smartAttributeColumnId        = &table.Column{Id: 0, Key: "id", Title: "ID", Alignment: tview.AlignRight}
	smartAttributeColumnName      = &table.Column{Id: 1, Key: "name", Title: "Attribute", Alignment: tview.AlignLeft}
	smartAttributeColumnNorm      = &table.Column{Id: 2, Key: "normalized", Title: "Norm", Alignment: tview.AlignRight}
	smartAttributeColumnWorst     = &table.Column{Id: 3, Key: "worst", Title: "Worst", Alignment: tview.AlignRight}
	smartAttributeColumnThreshold = &table.Column{Id: 4, Key: "threshold", Title: "Thresh", Alignment: tview.AlignRight}
	smartAttributeColumnRaw       = &table.Column{Id: 5, Key: "raw", Title: "Raw", Alignment: tview.AlignRight}
	smartAttributeColumnFailed    = &table.Column{Id: 6, Key: "failed", Title: "Failed", Alignment: tview.AlignLeft}
	// smartAttributeColumnValue is the value of NVMe and SCSI values, like smartAttributeColumnRaw
	smartAttributeColumnValue = &table.Column{Id: 7, Key: "value", Title: "Value", Alignment: tview.AlignRight}
	// smartAtaAttributeColumns are the columns of ATA attributes, which have an id and normalized values
	smartAtaAttributeColumns = []*table.Column{smartAttributeColumnId, smartAttributeColumnName, smartAttributeColumnNorm,
		smartAttributeColumnWorst, smartAttributeColumnThreshold, smartAttributeColumnRaw, smartAttributeColumnFailed}
	// smartOtherAttributeColumns are the columns of NVMe and SCSI values, which only have a value
	smartOtherAttributeColumns = []*table.Column{smartAttributeColumnName, smartAttributeColumnValue}
)

// SmartDevice is a device of a pool and the SMART reports of its disks, see NewSmartDialog.
type SmartDevice struct {
	// Name is the name of the device in "zpool status"
	Name    string
	Reports []disk.SmartReport
}

// smartDisk is a row of the table of disks: a disk of a device (a device on several disks, e.g. md, has a row for
// each).
type smartDisk struct {
	device string
	report disk.SmartReport
}

func (entry smartDisk) TableRowId() string {
	return entry.device + "\x00" + entry.report.Disk
}

// smartAttribute is a row of the table of attributes. index is its position in the output of smartctl, the order
// of values without an id (NVMe, SCSI).
type smartAttribute struct {
	disk.SmartAttribute
	index int
}

func (entry smartAttribute) TableRowId() string {
	return fmt.Sprintf("%d\x00%s", entry.Id, entry.Name)
}

// SmartDialog shows the SMART values of the disks of a pool: a table of the disks, the details of the selected one
// (health, problems), and a table of its attributes (the ones that tell of failures highlighted). Both tables can be
// sorted and filtered.
type SmartDialog struct {
	application   *tview.Application
	layout        *tview.Flex
	actionChannel chan DialogActionId

	// content holds the tables and the details, see updateDetails
	content    *tview.Flex
	disks      *table.RowSelectionTable[smartDisk]
	details    *tview.TextView
	attributes *table.RowSelectionTable[smartAttribute]
	// attributesOf is the disk whose attributes are shown, see updateAttributes. Only accessed on the UI thread.
	attributesOf string
	// attributesAta is whether the columns of ATA attributes are shown. Only accessed on the UI thread.
	attributesAta bool
}

// NewSmartDialog shows the SMART values of the devices of the pool, with the first disk of the device with the name
// focus selected ("" for the first one). onRead is called on the UI thread when the user asks to read them again
// (s), nil: not offered. Must be called on the UI thread.
func NewSmartDialog(application *tview.Application, pool string, devices []SmartDevice, focus string, onRead func()) *SmartDialog {
	d := &SmartDialog{application: application, actionChannel: make(chan DialogActionId)}

	var entries []*smartDisk
	for _, device := range devices {
		for _, report := range device.Reports {
			entries = append(entries, &smartDisk{device: device.Name, report: report})
		}
	}

	d.disks = table.NewTableContainer[smartDisk](application, toSmartDiskCells, sortSmartDisks)
	d.disks.SetColumnSpec(smartDiskColumns, smartDiskColumnDevice, false)
	d.disks.SetActiveColumns(smartDiskColumns)
	d.disks.SetTitle("Disks")
	d.disks.SetFilterFunc(func(entry *smartDisk, filterText string) bool {
		return table.MatchesGlob(zfs.DeviceDisplayName(entry.device), filterText) ||
			table.MatchesGlob(entry.report.Disk, filterText) || table.MatchesGlob(entry.report.Model, filterText) ||
			table.MatchesGlob(entry.report.Serial, filterText)
	})
	d.disks.SetPlaceholder("No device of the pool is stored on a disk.", theme.Colors.Pool.Hint)
	d.disks.SetSelectionChangedCallback(func(*smartDisk) { d.updateDetails() })

	d.details = tview.NewTextView().SetDynamicColors(true).SetWrap(false)
	d.details.SetBorderPadding(0, 0, 1, 1)

	d.attributes = table.NewTableContainer[smartAttribute](application, toSmartAttributeCells, sortSmartAttributes)
	d.attributes.SetColumnSpec(smartOtherAttributeColumns, smartAttributeColumnName, false)
	d.attributes.SetActiveColumns(smartOtherAttributeColumns)
	d.attributes.SetTitle("Attributes")
	d.attributes.SetFilterFunc(func(entry *smartAttribute, filterText string) bool {
		return table.MatchesGlob(entry.Name, filterText) || table.MatchesGlob(entry.Value, filterText) ||
			table.MatchesGlob(fmt.Sprint(entry.Id), filterText)
	})

	shortcuts := shortcut_helper.NewShortcutMap(application)
	shortcuts.SetEntries(smartDialogShortcuts(onRead != nil))

	diskRows := min(max(len(entries), 1), smartMaxDiskRows)
	maxDetails, maxAttributes := 0, 1
	for _, entry := range entries {
		maxDetails = max(maxDetails, len(formatSmartReport(entry.device, entry.report)))
		maxAttributes = max(maxAttributes, len(entry.report.Attributes))
	}
	d.content = tview.NewFlex().SetDirection(tview.FlexRow).
		// the header, the rows and the border
		AddItem(d.disks.GetLayout(), diskRows+3, 0, true).
		AddItem(d.details, 0, 0, false).
		AddItem(d.attributes.GetLayout(), 0, 1, false).
		AddItem(shortcuts.GetLayout(), smartShortcutLines, 0, false)

	title := " Disk health: " + pool + " "
	d.layout = createModal(title, d.content, DialogSizeConstraints{
		Title:             title,
		ExtraContentWidth: smartDialogWidth,
		// the disks, their details, the attributes (each with a header and a border) and the shortcuts; clamped to
		// the terminal height
		StaticHeight: diskRows + 3 + maxDetails + maxAttributes + 3 + smartShortcutLines,
	})
	d.layout.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		// while typing a filter, the keys belong to it (Esc ends it)
		if d.disks.IsEditingFilter() || d.attributes.IsEditingFilter() {
			return event
		}
		switch {
		case event.Key() == tcell.KeyTab || event.Key() == tcell.KeyBacktab:
			d.switchTable()
			return nil
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

	d.disks.SetData(entries)
	d.disks.SelectFirstIfExists()
	if focus != "" {
		for _, entry := range d.disks.GetEntries() {
			if entry.device == focus {
				d.disks.Select(entry)
				break
			}
		}
	}
	d.updateDetails()
	return d
}

func smartDialogShortcuts(canRead bool) []shortcut_helper.ShortcutEntry {
	entries := []shortcut_helper.ShortcutEntry{
		{KeyCombo: []string{shortcut_helper.KeyTab}, Name: "Disks/Attributes", Group: shortcut_helper.GroupNavigation},
		util.TableComponentShortcutFilter,
	}
	if canRead {
		entries = append(entries, shortcut_helper.ShortcutEntry{KeyCombo: []string{"s"}, Name: "Read again"})
	}
	return append(entries, shortcut_helper.ShortcutEntry{KeyCombo: []string{shortcut_helper.KeyEsc}, Name: "Close", Group: shortcut_helper.GroupGlobal})
}

// switchTable moves the focus between the table of the disks and the one of the attributes. Runs on the UI thread.
func (d *SmartDialog) switchTable() {
	if d.disks.HasFocus() {
		d.application.SetFocus(d.attributes.GetLayout())
	} else {
		d.application.SetFocus(d.disks.GetLayout())
	}
}

// updateDetails shows the details and the attributes of the selected disk. Runs on the UI thread.
func (d *SmartDialog) updateDetails() {
	entry := d.disks.GetSelectedEntry()
	if entry == nil {
		d.details.SetText("")
		d.content.ResizeItem(d.details, 0, 0)
		d.updateAttributes(nil)
		return
	}
	lines := formatSmartReport(entry.device, entry.report)
	d.details.SetText(strings.Join(lines, "\n"))
	d.content.ResizeItem(d.details, len(lines), 0)
	d.updateAttributes(entry)
}

// updateAttributes shows the attributes of the disk, keeping the sort order and the filter. The columns depend on
// the kind of disk: only ATA attributes have normalized values. Runs on the UI thread.
func (d *SmartDialog) updateAttributes(entry *smartDisk) {
	id := ""
	if entry != nil {
		id = entry.TableRowId()
	}
	if id == d.attributesOf && id != "" {
		return
	}
	d.attributesOf = id

	var attributes []*smartAttribute
	ata := false
	placeholder := "No disk selected."
	if entry != nil {
		for i, attribute := range entry.report.Attributes {
			attributes = append(attributes, &smartAttribute{SmartAttribute: attribute, index: i})
			ata = ata || attribute.Id > 0
		}
		placeholder = "No values read."
	}
	// the sort order is kept between disks of the same kind
	if ata != d.attributesAta {
		d.attributesAta = ata
		if ata {
			d.attributes.SetColumnSpec(smartAtaAttributeColumns, smartAttributeColumnId, false)
			d.attributes.SetActiveColumns(smartAtaAttributeColumns)
		} else {
			d.attributes.SetColumnSpec(smartOtherAttributeColumns, smartAttributeColumnName, false)
			d.attributes.SetActiveColumns(smartOtherAttributeColumns)
		}
	}
	d.attributes.SetPlaceholder(placeholder, theme.Colors.Pool.Hint)
	d.attributes.SetData(attributes)
	d.attributes.SelectFirstIfExists()
}

// toSmartDiskCells shows a disk, its health and most severe problem colored like in the devices pane.
func toSmartDiskCells(row int, columns []*table.Column, entry *smartDisk) []*tview.TableCell {
	report := entry.report
	var cells []*tview.TableCell
	for _, column := range columns {
		text, color := "", tcell.ColorWhite
		switch column {
		case smartDiskColumnDevice:
			text = zfs.DeviceDisplayName(entry.device)
		case smartDiskColumnDisk:
			text = report.Disk
		case smartDiskColumnModel:
			text, color = report.Model, tcell.ColorLightGray
		case smartDiskColumnHealth:
			text, color = smartHealth(report)
		case smartDiskColumnSmart:
			text, color = SmartSummary([]disk.SmartReport{report})
		case smartDiskColumnHours:
			if report.HasValues() && report.PowerOnHours >= 0 {
				text, color = fmt.Sprint(report.PowerOnHours), tcell.ColorLightGray
			}
		}
		cells = append(cells, tview.NewTableCell(tview.Escape(text)).SetTextColor(color).SetAlign(column.Alignment))
	}
	return cells
}

// smartHealth returns the text and color of the result of the health check of the disk, "" if it was not read.
func smartHealth(report disk.SmartReport) (string, tcell.Color) {
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

// smartHealthRank orders the results of the health check, failed first.
func smartHealthRank(report disk.SmartReport) int {
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

// smartProblemRank orders disks by their most severe problem: errors first, then warnings, then the others.
func smartProblemRank(report disk.SmartReport) int {
	problem, ok := disk.WorstProblem([]disk.SmartReport{report})
	switch {
	case !ok:
		return 2
	case problem.Severity == disk.SmartError:
		return 0
	default:
		return 1
	}
}

func sortSmartDisks(entries []*smartDisk, column *table.Column, inverted bool) []*smartDisk {
	slices.SortStableFunc(entries, func(a, b *smartDisk) int {
		result := 0
		switch column {
		case smartDiskColumnDisk:
			result = cmp.Compare(a.report.Disk, b.report.Disk)
		case smartDiskColumnModel:
			result = cmp.Compare(a.report.Model, b.report.Model)
		case smartDiskColumnHealth:
			result = cmp.Compare(smartHealthRank(a.report), smartHealthRank(b.report))
		case smartDiskColumnSmart:
			result = cmp.Compare(smartProblemRank(a.report), smartProblemRank(b.report))
		case smartDiskColumnHours:
			result = cmp.Compare(a.report.PowerOnHours, b.report.PowerOnHours)
		}
		if result == 0 {
			result = cmp.Or(cmp.Compare(zfs.DeviceDisplayName(a.device), zfs.DeviceDisplayName(b.device)),
				cmp.Compare(a.report.Disk, b.report.Disk))
		}
		if inverted {
			result = -result
		}
		return result
	})
	return entries
}

// toSmartAttributeCells shows an attribute: the ones that tell of failures in white and bold, failing ones in the
// color of errors.
func toSmartAttributeCells(row int, columns []*table.Column, entry *smartAttribute) []*tview.TableCell {
	color := tcell.ColorLightGray
	attributes := tcell.AttrNone
	switch {
	case entry.Failed == "now":
		color = theme.Colors.Pool.Faulted
	case entry.Important:
		color = tcell.ColorWhite
	}
	if entry.Important {
		attributes = tcell.AttrBold
	}
	number := func(value int) string {
		if value < 0 {
			return ""
		}
		return fmt.Sprint(value)
	}
	var cells []*tview.TableCell
	for _, column := range columns {
		var text string
		switch column {
		case smartAttributeColumnId:
			if entry.Id > 0 {
				text = fmt.Sprint(entry.Id)
			}
		case smartAttributeColumnName:
			text = entry.Name
		case smartAttributeColumnNorm:
			text = number(entry.Normalized)
		case smartAttributeColumnWorst:
			text = number(entry.Worst)
		case smartAttributeColumnThreshold:
			text = number(entry.Threshold)
		case smartAttributeColumnRaw, smartAttributeColumnValue:
			text = entry.Value
		case smartAttributeColumnFailed:
			text = entry.Failed
		}
		cells = append(cells, tview.NewTableCell(tview.Escape(text)).SetTextColor(color).SetAttributes(attributes).
			SetAlign(column.Alignment))
	}
	return cells
}

func sortSmartAttributes(entries []*smartAttribute, column *table.Column, inverted bool) []*smartAttribute {
	slices.SortStableFunc(entries, func(a, b *smartAttribute) int {
		result := 0
		switch column {
		case smartAttributeColumnId:
			result = cmp.Compare(a.Id, b.Id)
		case smartAttributeColumnName:
			result = cmp.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name))
		case smartAttributeColumnNorm:
			result = cmp.Compare(a.Normalized, b.Normalized)
		case smartAttributeColumnWorst:
			result = cmp.Compare(a.Worst, b.Worst)
		case smartAttributeColumnThreshold:
			result = cmp.Compare(a.Threshold, b.Threshold)
		case smartAttributeColumnRaw, smartAttributeColumnValue:
			// numbers by their value, the others (e.g. text) by their text after them
			switch {
			case a.Raw >= 0 && b.Raw >= 0:
				result = cmp.Compare(a.Raw, b.Raw)
			case a.Raw >= 0:
				result = -1
			case b.Raw >= 0:
				result = 1
			default:
				result = cmp.Compare(a.Value, b.Value)
			}
		case smartAttributeColumnFailed:
			// failing ones first
			result = cmp.Compare(smartFailedRank(a.Failed), smartFailedRank(b.Failed))
		}
		if result == 0 {
			result = cmp.Compare(a.index, b.index)
		}
		if inverted {
			result = -result
		}
		return result
	})
	return entries
}

// smartFailedRank orders the attributes by whether they fail: now, in the past, never.
func smartFailedRank(failed string) int {
	switch failed {
	case "now":
		return 0
	case "past":
		return 1
	default:
		return 2
	}
}

// formatSmartReport returns the lines of the details of a disk of the device: what it is, its health and problems.
// The attributes are in their own table.
func formatSmartReport(device string, report disk.SmartReport) []string {
	name := zfs.DeviceDisplayName(device)
	if report.Disk != name {
		name += " → " + report.Disk
	}
	lines := []string{"[::b]" + Highlight(name) + "[::-]"}

	field := func(label string, value string) {
		lines = append(lines, txwidgets.Span(theme.Colors.Layout.Table.Accent, "  %-9s", labelWithColon(label))+" "+value)
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

	health, color := smartHealth(report)
	field("Health", txwidgets.Span(color, "%s", health))
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
		field(label, txwidgets.Span(smartSeverityColor(problem.Severity), "%s", problem.Text))
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

// SmartSummary returns the text and color of the SMART values of the disks of a device: the most severe problem
// (e.g. "8 realloc"), "OK", why they could not be read, or "asleep" for a disk that slept whenever it was asked. ""
// if there is nothing to show (no disks, not read yet, or smartctl is not installed).
func SmartSummary(reports []disk.SmartReport) (string, tcell.Color) {
	if problem, ok := disk.WorstProblem(reports); ok {
		return problem.Short, smartSeverityColor(problem.Severity)
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

// GetText returns the details of the selected disk, without style tags if stripTags. Must be called on the UI
// thread.
func (d *SmartDialog) GetText(stripTags bool) string {
	return d.details.GetText(stripTags)
}

func (d *SmartDialog) Close() {
	emitDialogActions(d.actionChannel, DialogCloseActionId)
}
