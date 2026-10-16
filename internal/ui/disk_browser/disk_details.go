package disk_browser

import (
	"cmp"
	"fmt"
	"math"
	"slices"
	"strings"
	"zfs-file-history/internal/disk"
	"zfs-file-history/internal/state"
	"zfs-file-history/internal/ui/clickable"
	"zfs-file-history/internal/ui/dialog"
	"zfs-file-history/internal/ui/scrollbar"
	"zfs-file-history/internal/ui/shortcut_helper"
	"zfs-file-history/internal/ui/table"
	"zfs-file-history/internal/ui/theme"
	uiutil "zfs-file-history/internal/ui/util"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

var (
	attributeColumnId        = &table.Column{Id: 0, Key: "id", Title: "ID", Alignment: tview.AlignRight}
	attributeColumnName      = &table.Column{Id: 1, Key: "name", Title: "Attribute", Alignment: tview.AlignLeft}
	attributeColumnNorm      = &table.Column{Id: 2, Key: "normalized", Title: "Norm", Alignment: tview.AlignRight}
	attributeColumnWorst     = &table.Column{Id: 3, Key: "worst", Title: "Worst", Alignment: tview.AlignRight}
	attributeColumnThreshold = &table.Column{Id: 4, Key: "threshold", Title: "Thresh", Alignment: tview.AlignRight}
	attributeColumnValue     = &table.Column{Id: 5, Key: "value", Title: "Value", Alignment: tview.AlignRight}
	attributeColumnFailed    = &table.Column{Id: 6, Key: "failed", Title: "Failed", Alignment: tview.AlignLeft}
	// attributeColumnRisk shows how close an attribute is to failing, see disk.SmartAttribute.Risk
	attributeColumnRisk = &table.Column{Id: 7, Key: "risk", Title: "Risk", Alignment: tview.AlignLeft}

	// attributeColumns are the columns of all kinds of disks: only ATA attributes have an id and normalized values,
	// the cells are empty for the values of NVMe and SCSI disks
	attributeColumns = []*table.Column{attributeColumnId, attributeColumnName, attributeColumnNorm, attributeColumnWorst,
		attributeColumnThreshold, attributeColumnRisk, attributeColumnValue, attributeColumnFailed}
)

// stateKeyAttributesTable is the key of the column layout of the attributes remembered in state.Current
const stateKeyAttributesTable = "diskAttributes"

// AttributeEntry is a row of the table of attributes. Index is its position in the output of smartctl, the order of
// values without an id (NVMe, SCSI).
type AttributeEntry struct {
	disk.SmartAttribute
	Index int
}

func (entry AttributeEntry) TableRowId() string {
	return fmt.Sprintf("%d\x00%s", entry.Id, entry.Name)
}

// DiskDetailsComponent shows the details of the selected disk (what it is, its health and problems), above a table
// of its SMART attributes (the ones that tell of failures highlighted).
type DiskDetailsComponent struct {
	application    *tview.Application
	layout         *detailsLayout
	details        *scrollbar.TextView
	detailsView    *detailsView
	tableContainer *table.RowSelectionTable[AttributeEntry]

	// All fields below are only accessed on the UI thread.

	// shownDisk is the disk whose attributes are shown (its TableRowId), see SetDisk
	shownDisk string
	// shownEntry is the disk whose details are shown, nil: none
	shownEntry *DiskEntry
	// renderedTimeFormat is the uiutil.TimeFormatGeneration the details were rendered with
	renderedTimeFormat uint64
	// layoutWidth and layoutHeight are the size of the layout as last drawn, 0 before; renderedWidth and
	// renderedHeight the one the details were rendered for, see renderDetails
	layoutWidth, layoutHeight     int
	renderedWidth, renderedHeight int
	// dialogPages are the pages dialogs are shown on, see SetDialogPages
	dialogPages *tview.Pages
	// poolLinks are where the names of the pools are in the details, which can be clicked (see detailsView)
	poolLinks []poolLink
	// regions are the parts of the details that can be clicked: the names of the pools
	regions clickable.Regions
	// onShowPool shows a pool on the pools page (nil: the names of the pools cannot be clicked), see SetOnShowPool
	onShowPool func(pool string)
	// enabled returns whether the names of the pools can be clicked, e.g. not while a dialog is in front
	enabled func() bool
}

// detailsView is the text of the details as placed in the layout, which also draws the names of the pools that can be
// clicked.
type detailsView struct {
	*scrollbar.TextView
	component *DiskDetailsComponent
}

// Draw draws the details, and adds the names of the pools, which can be clicked, where they are shown.
func (view *detailsView) Draw(screen tcell.Screen) {
	view.TextView.Draw(screen)

	component := view.component
	component.regions.Reset()
	defer component.regions.DrawHover(screen)
	if component.onShowPool == nil {
		return
	}
	onShowPool := component.onShowPool
	links := make([]clickable.TextLink, 0, len(component.poolLinks))
	for _, link := range component.poolLinks {
		links = append(links, clickable.TextLink{Line: link.line, Column: link.column, Width: link.width, Key: link.pool,
			OnClick: func() { onShowPool(link.pool) }})
	}
	component.regions.AddTextLinks(view, links, theme.Colors.Pool.HoverBackground)
}

// MouseHandler shows a pool when its name is clicked (see clickable.Regions), and otherwise handles the mouse like
// the text view (e.g. scrolling), without focusing it: there is nothing to do in it, like Tab skips it.
func (view *detailsView) MouseHandler() uiutil.MouseHandler {
	component := view.component
	return component.regions.WrapMouseHandler(view, func() bool { return component.enabled() },
		uiutil.WithoutFocus(view.TextView.MouseHandler()))
}

// detailsLayout is the layout of the details above the attributes: drawing it renders the details again for its
// size (side by side or one below the other, see formatDetails), or if formatted times changed (when the SMART values
// were read, "38 seconds ago"), like the tables do (see uiutil.TimeFormatGeneration).
type detailsLayout struct {
	*tview.Flex
	component *DiskDetailsComponent
}

func (layout *detailsLayout) Draw(screen tcell.Screen) {
	component := layout.component
	_, _, component.layoutWidth, component.layoutHeight = layout.GetRect()
	if component.shownEntry != nil && (component.renderedTimeFormat != uiutil.TimeFormatGeneration() ||
		component.renderedWidth != component.layoutWidth || component.renderedHeight != component.layoutHeight) {
		component.renderDetails()
	}
	layout.Flex.Draw(screen)
}

// detailsFrameWidth is the width the border and the padding of the details take.
const detailsFrameWidth = 4

// detailsMaxPercent is the most of the height of the layout the details take, so some attributes are always shown;
// the rest of the details scrolls.
const detailsMaxPercent = 60

// NewDiskDetails returns the details of a disk. Must be called on the UI thread.
func NewDiskDetails(application *tview.Application) *DiskDetailsComponent {
	component := &DiskDetailsComponent{application: application, enabled: func() bool { return true }}

	// the attributes below take the space they need, the details may not fit
	component.details = scrollbar.NewTextView(application)
	component.details.SetDynamicColors(true).SetWrap(false)
	// a pane of its own like the path overview, which Tab skips: there is nothing to do in it
	component.details.SetBorder(true)
	component.details.SetBorderPadding(0, 0, 1, 1)
	uiutil.SetupWindow(component.details, "Disk")
	component.detailsView = &detailsView{TextView: component.details, component: component}

	t := table.NewTableContainer[AttributeEntry](application, toAttributeCells, sortAttributes)
	t.SetTitle("Attributes")
	t.SetFilterFunc(func(entry *AttributeEntry, filterText string) bool {
		return table.MatchesGlob(entry.Name, filterText) || table.MatchesGlob(entry.Value, filterText) ||
			(entry.Id > 0 && table.MatchesGlob(fmt.Sprint(entry.Id), filterText))
	})
	t.SetFilterChangedCallback(component.updateFooter)
	t.SetColumnSpec(attributeColumns, attributeColumnId, false)
	t.SetActiveColumns(attributeColumns)
	t.BindColumnLayout(state.Current, stateKeyAttributesTable, attributeColumns)
	t.SetPlaceholder("No disk selected.", theme.Colors.Pool.Hint)
	t.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyF2 {
			component.openColumnSelectionDialog()
			return nil
		}
		return event
	})
	component.tableContainer = t

	component.layout = &detailsLayout{Flex: tview.NewFlex().SetDirection(tview.FlexRow), component: component}
	component.layout.AddItem(component.detailsView, 0, 0, false).
		AddItem(t.GetLayout(), 0, 1, true)
	return component
}

// SetDialogPages sets the pages its dialogs are shown on: the pages of the application, so they are modal for the
// whole screen. Must be called on the UI thread.
func (component *DiskDetailsComponent) SetDialogPages(pages *tview.Pages) {
	component.dialogPages = pages
}

// SetOnShowPool sets the function that shows a pool on the pools page, called when the name of a pool the disk is used
// by is clicked. Must be called on the UI thread.
func (component *DiskDetailsComponent) SetOnShowPool(onShowPool func(pool string)) {
	component.onShowPool = onShowPool
}

// SetEnabledFunc sets a function that returns whether the names of the pools can be clicked, e.g. not while a dialog
// is in front (tview hands mouse events to all pages, see rivo/tview#926). Must be called on the UI thread.
func (component *DiskDetailsComponent) SetEnabledFunc(enabled func() bool) {
	component.enabled = enabled
}

// SetDisk shows the details and the attributes of the disk, nil: none. The selected attribute (or the header), the
// sort order and the filter are kept while the same disk is shown (e.g. read again). Must be called on the UI thread.
func (component *DiskDetailsComponent) SetDisk(entry *DiskEntry) {
	component.shownEntry = entry
	if entry == nil {
		component.details.SetText("")
		component.poolLinks = nil
		uiutil.SetupWindow(component.details, "Disk")
		component.layout.ResizeItem(component.detailsView, 0, 0)
		component.shownDisk = ""
		component.tableContainer.SetPlaceholder("No disk selected.", theme.Colors.Pool.Hint)
		component.tableContainer.SetData([]*AttributeEntry{})
		component.updateFooter()
		return
	}
	component.renderDetails()

	var attributes []*AttributeEntry
	for i, attribute := range entry.Report.Attributes {
		attributes = append(attributes, &AttributeEntry{SmartAttribute: attribute, Index: i})
	}
	component.tableContainer.SetPlaceholder("No values read.", theme.Colors.Pool.Hint)
	selected := component.tableContainer.GetSelectedEntry()
	sameDisk := component.shownDisk == entry.TableRowId()
	// the header stays selected while the same disk is read again, e.g. to sort by another column
	headerSelected := sameDisk && selected == nil && !component.tableContainer.IsEmpty()
	component.shownDisk = entry.TableRowId()
	component.tableContainer.SetData(attributes)
	component.tableContainer.SelectFirstIfExists()
	if headerSelected && !component.tableContainer.IsEmpty() {
		component.tableContainer.SelectHeader()
	}
	if sameDisk && selected != nil {
		if attribute := component.tableContainer.FindByRowId(selected.TableRowId()); attribute != nil {
			component.tableContainer.Select(attribute)
		}
	}
	component.updateFooter()
}

// renderDetails shows the details of the shown disk, fitted into the size of the layout as last drawn (any width
// before it was drawn). Runs on the UI thread.
func (component *DiskDetailsComponent) renderDetails() {
	component.renderedTimeFormat = uiutil.TimeFormatGeneration()
	component.renderedWidth, component.renderedHeight = component.layoutWidth, component.layoutHeight
	width := 0
	if component.layoutWidth > 0 {
		width = max(component.layoutWidth-detailsFrameWidth, 1)
	}
	lines, links := formatDetailsWithPoolLinks(component.shownEntry, width)
	// and the border
	height := len(lines) + 2
	if component.layoutHeight > 0 {
		if capped := uiutil.FitContent(component.layoutHeight, height, detailsMaxPercent); capped < height {
			height = capped
			// the details scroll, the scrollbar takes a column (see scrollbar.TextView)
			if width > 1 {
				lines, links = formatDetailsWithPoolLinks(component.shownEntry, width-1)
			}
		}
	}
	uiutil.SetupWindow(component.details, detailsTitle(component.shownEntry))
	component.details.SetText(strings.Join(lines, "\n"))
	component.poolLinks = links
	component.layout.ResizeItem(component.detailsView, height, 0)
}

// updateFooter shows the number of attributes in the bottom border of the table. Runs on the UI thread.
func (component *DiskDetailsComponent) updateFooter() {
	all := len(component.tableContainer.GetAllEntries())
	switch {
	case all == 0:
		component.tableContainer.SetFooter("")
	case component.tableContainer.IsFilterActive():
		component.tableContainer.SetFooter(fmt.Sprintf("%d of %d values", len(component.tableContainer.GetEntries()), all))
	default:
		component.tableContainer.SetFooter(fmt.Sprintf("%d values", all))
	}
}

// GetDetailsText returns the details of the shown disk, without style tags if stripTags. Must be called on the UI
// thread.
func (component *DiskDetailsComponent) GetDetailsText(stripTags bool) string {
	return component.details.GetText(stripTags)
}

// openColumnSelectionDialog lets the user select and order the displayed columns. Runs on the UI thread.
func (component *DiskDetailsComponent) openColumnSelectionDialog() {
	if component.dialogPages == nil {
		return
	}
	d := dialog.NewTableColumnSelectionDialog(component.application, "Configure Attribute Columns", attributeColumns, component.tableContainer)
	dialog.ShowDialogOnPages(component.application, component.dialogPages, d, nil)
}

// GetShortcutMap returns the shortcuts of the attributes.
func (component *DiskDetailsComponent) GetShortcutMap() []shortcut_helper.ShortcutEntry {
	return []shortcut_helper.ShortcutEntry{
		uiutil.TableComponentShortcutColumns.WithRun(component.openColumnSelectionDialog).OnlyInMenu(),
		uiutil.TableComponentShortcutFilter.WithRun(component.tableContainer.StartFilter),
	}
}

// GetCommands returns the sort commands of the attributes, see shortcut_helper.CommandProvider.
func (component *DiskDetailsComponent) GetCommands() []shortcut_helper.ShortcutEntry {
	return component.tableContainer.SortCommands()
}

func (component *DiskDetailsComponent) GetLayout() tview.Primitive {
	return component.layout
}

func (component *DiskDetailsComponent) HasFocus() bool {
	return component.tableContainer.HasFocus()
}

func (component *DiskDetailsComponent) Focus() {
	component.application.SetFocus(component.tableContainer.GetLayout())
}

// toAttributeCells shows an attribute: the ones that tell of failures in white and bold, failing ones in the color
// of errors.
func toAttributeCells(row int, columns []*table.Column, entry *AttributeEntry) []*tview.TableCell {
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
		case attributeColumnId:
			if entry.Id > 0 {
				text = fmt.Sprint(entry.Id)
			}
		case attributeColumnName:
			text = entry.Name
		case attributeColumnNorm:
			text = number(entry.Normalized)
		case attributeColumnWorst:
			text = number(entry.Worst)
		case attributeColumnThreshold:
			text = number(entry.Threshold)
		case attributeColumnValue:
			text = entry.Value
		case attributeColumnFailed:
			text = entry.Failed
		case attributeColumnRisk:
			// with style tags of its own, not escaped
			cells = append(cells, tview.NewTableCell(formatRisk(entry.SmartAttribute)).SetAlign(column.Alignment))
			continue
		}
		cells = append(cells, tview.NewTableCell(tview.Escape(text)).SetTextColor(color).SetAttributes(attributes).
			SetAlign(column.Alignment))
	}
	return cells
}

// sortAttributes sorts the attributes by the column, and in the order of smartctl if equal (e.g. by id: the values
// of NVMe disks, which have none).
func sortAttributes(entries []*AttributeEntry, column *table.Column, inverted bool) []*AttributeEntry {
	slices.SortStableFunc(entries, func(a, b *AttributeEntry) int {
		result := 0
		switch column {
		case attributeColumnId:
			result = cmp.Compare(a.Id, b.Id)
		case attributeColumnName:
			result = cmp.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name))
		case attributeColumnNorm:
			result = cmp.Compare(a.Normalized, b.Normalized)
		case attributeColumnWorst:
			result = cmp.Compare(a.Worst, b.Worst)
		case attributeColumnThreshold:
			result = cmp.Compare(a.Threshold, b.Threshold)
		case attributeColumnValue:
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
		case attributeColumnRisk:
			result = cmp.Compare(riskRank(a.SmartAttribute), riskRank(b.SmartAttribute))
		case attributeColumnFailed:
			// failing ones first
			result = cmp.Compare(failedRank(a.Failed), failedRank(b.Failed))
		}
		if result == 0 {
			result = cmp.Compare(a.Index, b.Index)
		}
		if inverted {
			result = -result
		}
		return result
	})
	return entries
}

// riskBarWidth is the width of the bar of the risk column, see formatRisk.
const riskBarWidth = 8

// formatRisk returns how close the attribute is to failing: a bar (empty while it is far from it, full where it fails,
// like the wear in the details) and the percentage, rounded down, so 100% is only shown when it fails. "" if it cannot
// fail.
func formatRisk(attribute disk.SmartAttribute) string {
	if !attribute.RiskKnown {
		return ""
	}
	return uiutil.InfoBar(attribute.Risk, riskBarWidth) + uiutil.InfoValue(fmt.Sprintf(" %3.0f%%", math.Floor(attribute.Risk*100)))
}

// riskRank orders the attributes by how close they are to failing, the ones that cannot fail first (the least).
func riskRank(attribute disk.SmartAttribute) float64 {
	if !attribute.RiskKnown {
		return -1
	}
	return attribute.Risk
}

// failedRank orders the attributes by whether they fail: now, in the past, never.
func failedRank(failed string) int {
	switch failed {
	case "now":
		return 0
	case "past":
		return 1
	default:
		return 2
	}
}
