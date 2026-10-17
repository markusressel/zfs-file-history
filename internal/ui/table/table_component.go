package table

import (
	"fmt"
	"maps"
	"slices"
	"sync"
	"time"
	"zfs-file-history/internal/ui/clickable"
	"zfs-file-history/internal/ui/scrollbar"
	"zfs-file-history/internal/ui/theme"
	"zfs-file-history/internal/ui/txwidgets"
	uiutil "zfs-file-history/internal/ui/util"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// RowSelectionTableEntry is an interface that must be implemented by entries used in a RowSelectionTable.
type RowSelectionTableEntry interface {
	// TableRowId returns a unique identifier for the entry.
	// This identifier is used to keep track of the selected entries in the table.
	TableRowId() string
}

const (
	SpaceRune = ' '
)

type ColumnId int

type Column struct {
	Id ColumnId
	// Key identifies the column in the saved table layout (see BindColumnLayout), so it must never change.
	Key       string
	Title     string
	Alignment int
	// Note says when the column is shown, if not always (see SetColumnShownFunc), in the column selection
	Note string
	// AddedLater marks a column that saved layouts of versions before TableLayout.Known did not know: it is added to
	// them if it is in the default layout, see BindColumnLayout
	AddedLater bool
}

// RowSelectionTable is a table component for the special case where
// only a single table row can be highlighted at a time instead of a table cell.
//
// RowSelectionTable is a generic component and can be used with any type T.
// For each entry of type T in the table, a row is created via the toTableCells function.
//
// RowSelectionTable supports custom sorting of entries by providing the sortTableEntries function.
// The column can be sorted by individual columns or multiple columns as you please.
//
// RowSelectionTable supports the selection of multiple entries using the "Space" key.
// This feature is disabled by default, but can be enabled by setting the "multiSelectEnabled" property to true.
// The entries of type T can implement the RowSelectionTableEntry interface to provide a unique identifier for each entry,
// which allows the selection to be retained even when the memory address of the entry changes.
type RowSelectionTable[T RowSelectionTableEntry] struct {
	application *tview.Application

	layout *tview.Flex
	footer *uiutil.BorderFooter
	// footerEnabled returns whether the parts of the footer and the cell regions can be clicked, nil: always, see
	// SetFooterEnabledFunc
	footerEnabled func() bool
	// cellRegions returns the parts of a cell that react to the mouse, nil: none, see SetCellRegionsFunc;
	// regions are the ones drawn. Only accessed on the UI thread.
	cellRegions func(entry *T, column *Column) []CellRegion
	regions     clickable.Regions
	table       *tview.Table
	// view is the table as placed in the layout, see tableView
	view *tableView
	// renderedTimeFormat is the uiutil.TimeFormatGeneration the cells were rendered with
	renderedTimeFormat uint64
	// drawnHeight is the height of the table as last drawn, see keepSelectionVisible. Only accessed on the UI thread.
	drawnHeight int
	scrollbar   *scrollbar.ScrollbarComponent

	// allEntries are all entries set with SetData, entries are the ones that match the filter (displayed)
	allEntries   []*T
	entries      []*T
	entriesMutex sync.Mutex
	// rowEntries are the entries of the rows of the table, nil for the header row and the rows that start a group,
	// groupRows the rows that start a group (see SetGroupings). Only accessed on the UI thread.
	rowEntries []*T
	groupRows  map[int]groupRow
	// groupings are the ways the entries can be grouped, grouping the one they are grouped by (nil: none)
	groupings []Grouping[T]
	grouping  *Grouping[T]

	// title is the title set with SetTitle
	title string

	// filterMatches enables filtering, see SetFilterFunc
	filterMatches func(entry *T, filterText string) bool
	// extraFilter hides entries independent of the typed filter, see SetExtraFilter
	extraFilter           func(entry *T) bool
	filterText            string
	isEditingFilter       bool
	filterEditor          lineEditor
	filterChangedCallback func()

	isUpdatingData bool

	lastSelectedEntry      *T
	multiSelectEnabled     bool
	multiSelectionEntryMap map[string]*T

	sortByColumn     *Column
	sortTableEntries func(entries []*T, column *Column, inverted bool) []*T
	toTableCells     func(row int, columns []*Column, entry *T) (cells []*tview.TableCell)

	inputCapture             func(event *tcell.EventKey) *tcell.EventKey
	selectionChangedCallback func(selectedEntry *T)

	columnSpec []*Column
	// columnShown returns whether an active column is drawn, nil: all are, see SetColumnShownFunc. Only accessed on
	// the UI thread.
	columnShown  func(column *Column) bool
	sortInverted bool
	// columnOffset is the number of columns hidden on the left (horizontal scrolling), see scrollColumns.
	// Only accessed on the UI thread.
	columnOffset int

	defaultSortColumn   *Column
	defaultSortInverted bool

	// columnLayoutChangedCallback is called when the user changes the columns or the sort order,
	// see SetColumnLayoutChangedCallback
	columnLayoutChangedCallback func(layout ColumnLayout)
	// savedLayout is the binding to the saved layout, see BindColumnLayout
	savedLayout *savedLayoutBinding

	isScrollbarVisible bool

	// placeholder is shown instead of the rows while there are none, see SetPlaceholder
	placeholder      string
	placeholderColor tcell.Color

	// headerWasSelected is whether the header row was selected (and focused) when the mouse button was pressed, see
	// handleHeaderClick
	headerWasSelected bool

	lastSyncHeight int
	resizeTimer    *time.Timer

	// contentWidth is the width of all columns, see ContentSize. Only accessed on the UI thread.
	contentWidth int
}

func NewTableContainer[T RowSelectionTableEntry](
	application *tview.Application,
	toTableCells func(row int, columns []*Column, entry *T) (cells []*tview.TableCell),
	sortTableEntries func(entries []*T, column *Column, inverted bool) []*T,
) *RowSelectionTable[T] {
	tableContainer := &RowSelectionTable[T]{
		application:  application,
		entriesMutex: sync.Mutex{},

		multiSelectEnabled:     false,
		multiSelectionEntryMap: map[string]*T{},

		toTableCells:     toTableCells,
		sortTableEntries: sortTableEntries,

		inputCapture: func(event *tcell.EventKey) *tcell.EventKey {
			return event
		},
		selectionChangedCallback: func(selectedEntry *T) {},
	}
	tableContainer.createLayout()
	tableContainer.setupResizeMonitor()
	return tableContainer
}

func (c *RowSelectionTable[T]) setupResizeMonitor() {
	c.table.SetDrawFunc(func(screen tcell.Screen, x, y, width, height int) (int, int, int, int) {
		if height > 0 && height != c.lastSyncHeight {
			c.lastSyncHeight = height
			if c.resizeTimer != nil {
				c.resizeTimer.Stop()
			}
			c.resizeTimer = time.AfterFunc(50*time.Millisecond, func() {
				c.application.QueueUpdateDraw(func() {
					c.syncScrollbar()
				})
			})
		}
		return x, y, width, height
	})
}

func (c *RowSelectionTable[T]) SetMultiSelect(multiSelect bool) {
	c.multiSelectEnabled = multiSelect
	c.ClearMultiSelection()
}

func (c *RowSelectionTable[T]) createLayout() {
	table := tview.NewTable()

	table.SetBorders(false)
	uiutil.IgnoreClicksBelowRows(table)
	ignoreClicksBelowRows := table.GetMouseCapture()
	table.SetMouseCapture(func(action tview.MouseAction, event *tcell.EventMouse) (tview.MouseAction, *tcell.EventMouse) {
		action, event = ignoreClicksBelowRows(action, event)
		if event == nil {
			return action, event
		}
		if action == tview.MouseLeftClick && c.isBesideTheColumns(event.Position()) {
			// the empty space to the right of the rows, like the one below them: nothing to select
			return tview.MouseConsumed, nil
		}
		return c.handleHeaderClick(action, event)
	})

	// fixed header row
	table.SetFixed(1, 0)

	table.SetSelectedStyle(
		tcell.StyleDefault.
			Foreground(theme.Colors.Layout.Table.SelectedForeground).
			Background(theme.Colors.Layout.Table.SelectedBackground),
	)

	table.SetSelectable(true, false)
	table.SetBlurFunc(func() {
		c.stopEditingFilter()
	})
	table.SetSelectionChangedFunc(func(row, column int) {
		if c.isUpdatingData {
			return
		}

		selectedEntry := c.GetSelectedEntry()

		if c.lastSelectedEntry != nil && selectedEntry == nil {
			// workaround to the table not scrolling to the top when PgUp is pressed and a page jump will
			// select the table header row.
			c.scrollToTop()
		}
		c.selectionChangedCallback(selectedEntry)

		c.lastSelectedEntry = selectedEntry
		c.syncScrollbar()
	})

	table.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		// handled before the owner's input capture, so typing a filter never triggers its shortcuts
		if c.filterMatches != nil {
			var handled bool
			event, handled = c.handleFilterInput(event)
			if handled {
				return event
			}
		}

		event = c.inputCapture(event)
		if event == nil {
			return event
		}
		key := event.Key()

		if key == tcell.KeyRune && event.Rune() == 'b' && len(c.groupings) > 0 {
			c.CycleGrouping()
			return nil
		}

		// current selection is on HEADER row
		if c.GetSelectedEntry() == nil {
			switch key {
			case tcell.KeyRight:
				c.nextSortOrder()
				return nil
			case tcell.KeyLeft:
				c.previousSortOrder()
				return nil
			case tcell.KeyEnter:
				c.toggleSortDirection()
				return nil
			default:
			}
		}

		// current selection is on a DATA row: ← and → (h and l) scroll horizontally
		if c.GetSelectedEntry() != nil && event.Modifiers()&tcell.ModShift == 0 {
			switch {
			case key == tcell.KeyRight || (key == tcell.KeyRune && event.Rune() == 'l'):
				c.scrollColumns(1)
				return nil
			case key == tcell.KeyLeft || (key == tcell.KeyRune && event.Rune() == 'h'):
				c.scrollColumns(-1)
				return nil
			}
		}

		if c.HasMultiSelection() {
			switch key {
			case tcell.KeyEscape:
				c.ClearMultiSelection()
			}
		}

		if c.multiSelectEnabled {
			if event.Modifiers()&tcell.ModShift != 0 {
				switch key {
				case tcell.KeyUp:
					c.addToMultiSelection(c.GetSelectedEntry())
					c.Up()
					c.addToMultiSelection(c.GetSelectedEntry())
					return nil
				case tcell.KeyDown:
					c.addToMultiSelection(c.GetSelectedEntry())
					c.Down()
					c.addToMultiSelection(c.GetSelectedEntry())
					return nil
				default:
					return nil
				}
			}

			// current selection is on DATA row
			if event.Rune() == SpaceRune {
				currentEntry := c.GetSelectedEntry()
				c.toggleMultiSelection(currentEntry)
			}
		}

		return event
	})

	c.table = table

	c.scrollbar = scrollbar.NewScrollbarComponent(c.application, scrollbar.ScrollBarVertical, 0, 0, 0, 0)
	c.view = &tableView{Table: c.table, beforeDraw: c.beforeDraw, afterDraw: c.afterDraw, regions: &c.regions,
		regionsEnabled: func() bool { return c.footerEnabled == nil || c.footerEnabled() }}

	c.isScrollbarVisible = true
	c.layout = tview.NewFlex().
		SetDirection(tview.FlexColumn).
		AddItem(c.view, 0, 1, true).
		AddItem(c.scrollbar.GetLayout(), 1, 0, false)

	c.layout.SetBorder(true)
	c.layout.SetBorderPadding(0, 0, 1, 1)
	uiutil.SetupWindow(c.layout, "")
	c.footer = uiutil.NewBorderFooter(c.layout.Box)
	c.footer.SetLeftFunc(c.renderFilterFooter)
	// the clickable parts of the footer (see SetFooterParts); on the layout, which gets the events of its border
	c.layout.SetMouseCapture(func(action tview.MouseAction, event *tcell.EventMouse) (tview.MouseAction, *tcell.EventMouse) {
		enabled := c.footerEnabled == nil || c.footerEnabled()
		if c.footer.HandleMouse(action, event, enabled) {
			return tview.MouseConsumed, nil
		}
		return action, event
	})
}

// syncScrollbar shows the scrollbar if not all rows fit, and updates its position.
func (c *RowSelectionTable[T]) syncScrollbar() {
	if c.scrollbar == nil || c.table == nil {
		return
	}

	// without the (fixed) header row
	dataRows := c.table.GetRowCount() - 1
	_, _, _, height := c.table.GetInnerRect()
	visibleDataRows := height - 1
	if dataRows <= visibleDataRows {
		c.hideScrollbar()
		return
	}
	c.showScrollbar()
	c.updateScrollbarPosition()
}

// tableView is the tview table as placed in the layout: it draws the scrollbar again after the table was drawn.
//
// tview adjusts the row offset while drawing the table (e.g. to keep the selection visible after PgDn), which is
// after the selection changed callback. And tview's Flex draws its focused item (the table) last, after the
// scrollbar. So without this, the bar would show the previous offset and never reach the end.
//
// Before drawing, it renders the cells again if formatted times changed (see uiutil.TimeFormatGeneration).
type tableView struct {
	*tview.Table
	beforeDraw func()
	afterDraw  func(screen tcell.Screen)
	// regions are the cell regions of the table (see SetCellRegionsFunc), which react to the mouse while
	// regionsEnabled
	regions        *clickable.Regions
	regionsEnabled func() bool
}

func (v *tableView) Draw(screen tcell.Screen) {
	v.beforeDraw()
	v.Table.Draw(screen)
	v.afterDraw(screen)
}

// beforeDraw prepares the table for drawing at its size. Runs while drawing.
func (c *RowSelectionTable[T]) beforeDraw() {
	c.renderIfTimeFormatChanged()
	c.keepSelectionVisible()
}

// keepSelectionVisible scrolls the selected row into view when the table got lower (e.g. a smaller terminal, a pane
// above that grew): tview follows the end of the rows once all of them fit, and showed the last rows then, without
// the selected one. Runs while drawing.
func (c *RowSelectionTable[T]) keepSelectionVisible() {
	_, _, _, height := c.table.GetInnerRect()
	shrunk := height < c.drawnHeight
	c.drawnHeight = height
	if !shrunk {
		return
	}
	row, _ := c.table.GetSelection()
	rowOffset, columnOffset := c.table.GetOffset()
	// without the (fixed) header row
	dataRow, visibleDataRows := row-1, height-1
	if dataRow >= 0 && visibleDataRows > 0 {
		rowOffset = max(min(rowOffset, dataRow), dataRow-visibleDataRows+1)
	}
	// also stops following the end
	c.table.SetOffset(rowOffset, columnOffset)
}

// renderIfTimeFormatChanged renders the cells again if times are formatted differently now, e.g. after switching
// to relative times, or because "1 minute ago" became "2 minutes ago". Runs while drawing.
func (c *RowSelectionTable[T]) renderIfTimeFormatChanged() {
	if c.renderedTimeFormat != uiutil.TimeFormatGeneration() {
		c.updateTableContents()
	}
}

// afterDraw draws what is drawn over the table. Runs while drawing.
func (c *RowSelectionTable[T]) afterDraw(screen tcell.Screen) {
	c.drawGroupRows(screen)
	c.addCellRegions()
	c.regions.DrawHover(screen)
	c.drawEmptyState(screen)
	c.redrawScrollbar(screen)
}

// SetPlaceholder shows the text in place of the rows while there are none, e.g. why they could not be loaded.
// An empty text shows nothing. Must be called on the UI thread.
func (c *RowSelectionTable[T]) SetPlaceholder(text string, color tcell.Color) {
	c.placeholder = text
	c.placeholderColor = color
}

// GetPlaceholder returns the text set with SetPlaceholder.
func (c *RowSelectionTable[T]) GetPlaceholder() string {
	return c.placeholder
}

// drawEmptyState draws why there are no rows, centered in the space of the rows: the placeholder (e.g. why they
// could not be loaded, wrapped to the width of the table), or that no entry matches the filter. Runs while drawing.
func (c *RowSelectionTable[T]) drawEmptyState(screen tcell.Screen) {
	if len(c.entries) > 0 {
		return
	}
	x, y, width, height := c.table.GetInnerRect()
	var text string
	color := c.placeholderColor
	switch {
	case c.placeholder != "":
		text = tview.Escape(c.placeholder)
	case c.filterText != "" && len(c.allEntries) > 0:
		colors := theme.Colors.Layout.Table
		text = fmt.Sprintf("No matches for %s%s[-]", txwidgets.ColorTag(colors.Accent), tview.Escape(c.filterText))
		color = colors.EmptyText
	default:
		return
	}
	// the rows start below the header row
	uiutil.DrawEmptyState(screen, x, y+1, width, height-1, text, color)
}

// redrawScrollbar updates the scrollbar from the table as just drawn, and draws it again. Runs while drawing.
func (c *RowSelectionTable[T]) redrawScrollbar(screen tcell.Screen) {
	if !c.isScrollbarVisible {
		return
	}
	c.updateScrollbarPosition()
	c.scrollbar.GetLayout().Draw(screen)
}

// updateScrollbarPosition sets the position and size of the bar from the row offset of the table.
func (c *RowSelectionTable[T]) updateScrollbarPosition() {
	if !c.isScrollbarVisible {
		return
	}
	rowOffset, _ := c.table.GetOffset()
	_, _, _, height := c.table.GetInnerRect()
	// data rows only: the header row is fixed
	c.scrollbar.SetMax(max(c.table.GetRowCount()-1, 1))
	c.scrollbar.SetPosition(rowOffset)
	c.scrollbar.SetWidth(max(height-1, 1))
}

func (c *RowSelectionTable[T]) showScrollbar() {
	if !c.isScrollbarVisible {
		c.layout.AddItem(c.scrollbar.GetLayout(), 1, 0, false)
		c.isScrollbarVisible = true
	}
}

func (c *RowSelectionTable[T]) hideScrollbar() {
	if c.isScrollbarVisible {
		c.layout.RemoveItem(c.scrollbar.GetLayout())
		c.isScrollbarVisible = false
	}
}

func (c *RowSelectionTable[T]) GetLayout() tview.Primitive {
	return c.layout
}

// SetTitle sets the title of the table window.
func (c *RowSelectionTable[T]) SetTitle(title string) {
	c.title = title
	c.updateTitle()
}

func (c *RowSelectionTable[T]) updateTitle() {
	uiutil.SetupWindow(c.layout, c.title)
}

const filterFooterLabel = "Filter: "

// renderFilterFooter returns the active filter for the left side of the footer, e.g. "Filter: *.txt",
// within maxWidth. While it is being typed, it shows the cursor and scrolls to keep the cursor visible.
// Called when the footer is drawn (on the UI thread).
func (c *RowSelectionTable[T]) renderFilterFooter(maxWidth int) string {
	if !c.isEditingFilter && c.filterText == "" {
		return ""
	}

	termWidth := maxWidth - len(filterFooterLabel)
	var term string
	if c.isEditingFilter {
		term = c.filterEditor.RenderWindow(termWidth)
	} else {
		// cut off with an ellipsis by the footer, if too long
		term = tview.Escape(c.filterText)
	}
	return filterFooterLabel + txwidgets.ColorTag(theme.Colors.ShortcutMap.KeyCombo) + term + "[-]"
}

// SetFooter shows the given text right-aligned in the bottom border of the table window,
// e.g. for counts or active filters. It may contain style tags, an empty text hides the footer.
// Must be called on the UI thread.
func (c *RowSelectionTable[T]) SetFooter(text string) {
	c.footer.SetText(text)
}

// SetFooterParts is SetFooter with parts that can be clicked, see uiutil.BorderFooter.SetParts.
// Must be called on the UI thread.
func (c *RowSelectionTable[T]) SetFooterParts(parts ...uiutil.FooterPart) {
	c.footer.SetParts(parts...)
}

// SetFooterEnabledFunc sets a function that returns whether the parts of the footer and the cell regions (see
// SetCellRegionsFunc) can be clicked, e.g. not while a dialog is in front (see rivo/tview#926).
func (c *RowSelectionTable[T]) SetFooterEnabledFunc(enabled func() bool) {
	c.footerEnabled = enabled
}

// GetFooter returns the text set with SetFooter.
func (c *RowSelectionTable[T]) GetFooter() string {
	return c.footer.GetText()
}

func (c *RowSelectionTable[T]) SetColumnSpec(columns []*Column, defaultSortColumn *Column, inverted bool) {
	c.defaultSortColumn = defaultSortColumn
	c.defaultSortInverted = inverted
	c.columnSpec = slices.Clone(columns)

	c.sortByColumn = defaultSortColumn
	c.sortInverted = inverted
	if len(c.columnSpec) > 0 && !slices.Contains(c.columnSpec, c.sortByColumn) {
		c.sortByColumn = c.columnSpec[0]
	}

	c.SortBy(c.sortByColumn, c.sortInverted)
	c.updateTableContents()
}

// SetActiveColumns displays the given columns, keeping the sort order if its column is still displayed.
// Notifies the callback set with SetColumnLayoutChangedCallback. Must be called on the UI thread.
func (c *RowSelectionTable[T]) SetActiveColumns(columns []*Column) {
	if len(columns) <= 0 {
		return
	}
	c.SetColumnLayout(ColumnLayout{Columns: columns, SortColumn: c.sortByColumn, SortInverted: c.sortInverted})
	c.notifyColumnLayoutChanged()
}

func (c *RowSelectionTable[T]) GetColumnSpec() []*Column {
	return slices.Clone(c.columnSpec)
}

func (c *RowSelectionTable[T]) SetData(entries []*T) {
	c.isUpdatingData = true
	defer func() { c.isUpdatingData = false }()

	c.entriesMutex.Lock()
	c.allEntries = entries
	c.entries = c.filterEntries(entries)
	c.entriesMutex.Unlock()
	c.SortBy(c.sortByColumn, c.sortInverted)
	c.cleanupMultiSelection()
	c.updateTableContents()
}

// SetDataKeepingSelection is SetData, keeping the selected entry selected if it is still there (without notifying
// the selection changed callback, as it stays the same), e.g. when entries are added to the shown ones. Must be
// called on the UI thread.
func (c *RowSelectionTable[T]) SetDataKeepingSelection(entries []*T) {
	selected := c.GetSelectedEntry()
	c.SetData(entries)
	if selected != nil {
		c.isUpdatingData = true
		defer func() { c.isUpdatingData = false }()
		c.Select(selected)
	}
}

func (c *RowSelectionTable[T]) UpdateEntry(entry *T) {
	c.entriesMutex.Lock()
	defer c.entriesMutex.Unlock()

	if entry == nil {
		return
	}

	index := slices.Index(c.entries, entry)
	row := slices.Index(c.rowEntries, entry)
	if index < 0 || row < 0 {
		return
	}

	cells := c.toTableCells(index, c.visibleColumns(), entry)
	for column, cell := range cells {
		c.styleCell(entry, cell)
		c.table.SetCell(row, column, cell)
	}
}

// styleCell colors a cell of the entry: in the colors of the multi-selection if it is in it, otherwise in its own text
// color while its row is selected (unless the cell has a selected style of its own), see theme.SelectedStyle.
func (c *RowSelectionTable[T]) styleCell(entry *T, cell *tview.TableCell) {
	switch {
	case c.isInMultiSelection(entry):
		cell.SetBackgroundColor(theme.Colors.Layout.Table.MultiSelectionBackground)
		cell.SetTextColor(theme.Colors.Layout.Table.MultiSelectionForeground)
		cell.SetSelectedStyle(
			tcell.StyleDefault.Background(theme.Colors.Layout.Table.MultiSelectionBackground),
		)
	case cell.SelectedStyle == tcell.StyleDefault:
		uiutil.KeepColorWhenSelected(cell)
	}
}

func (c *RowSelectionTable[T]) SortBy(sortOption *Column, inverted bool) {
	c.entriesMutex.Lock()
	c.sortByColumn = sortOption
	c.sortInverted = inverted
	c.entries = c.sortIntoGroups(c.sortTableEntries(c.entries, c.effectiveSortColumn(), c.sortInverted))
	c.entriesMutex.Unlock()
}

// IsSortedBy returns whether the entries are sorted by the column.
func (c *RowSelectionTable[T]) IsSortedBy(column *Column) bool {
	return c.effectiveSortColumn() == column
}

// SetColumnShownFunc sets a function that returns whether an active column is drawn, for columns that only mean
// something in some modes of the table (see Column.Note). A column that is not drawn stays active, and its place
// in the layout is kept. Sorted by it, the entries are sorted by the default sort column meanwhile. Call
// UpdateShownColumns when the result changes. Must be called on the UI thread.
func (c *RowSelectionTable[T]) SetColumnShownFunc(shown func(column *Column) bool) {
	c.columnShown = shown
	c.UpdateShownColumns()
}

// UpdateShownColumns draws the columns again after the result of the function set with SetColumnShownFunc changed.
// Must be called on the UI thread.
func (c *RowSelectionTable[T]) UpdateShownColumns() {
	c.columnOffset = min(c.columnOffset, max(len(c.shownColumns())-1, 0))
	c.Resort()
}

// shownColumns returns the active columns that are drawn, see SetColumnShownFunc.
func (c *RowSelectionTable[T]) shownColumns() []*Column {
	if c.columnShown == nil {
		return c.columnSpec
	}
	var shown []*Column
	for _, column := range c.columnSpec {
		if c.columnShown(column) {
			shown = append(shown, column)
		}
	}
	return shown
}

// effectiveSortColumn returns the column the entries are sorted by: the chosen one, unless it is not drawn (see
// SetColumnShownFunc), then the default sort column, or the first one drawn.
func (c *RowSelectionTable[T]) effectiveSortColumn() *Column {
	shown := c.shownColumns()
	if c.sortByColumn == nil || slices.Contains(shown, c.sortByColumn) {
		return c.sortByColumn
	}
	if slices.Contains(shown, c.defaultSortColumn) {
		return c.defaultSortColumn
	}
	if len(shown) > 0 {
		return shown[0]
	}
	return c.sortByColumn
}

// Resort sorts the entries again, keeping the selected one selected (without notifying the selection changed
// callback, as it stays the same). Call it after values the entries are sorted by changed, e.g. ones computed in
// the background (UpdateEntry does not sort). Must be called on the UI thread.
func (c *RowSelectionTable[T]) Resort() {
	selected := c.GetSelectedEntry()
	c.isUpdatingData = true
	defer func() { c.isUpdatingData = false }()
	c.SortBy(c.sortByColumn, c.sortInverted)
	c.updateTableContents()
	if selected != nil {
		c.Select(selected)
	}
}

func (c *RowSelectionTable[T]) nextSortOrder() {
	columns := c.shownColumns()
	if len(columns) == 0 {
		return
	}
	currentIndex := slices.Index(columns, c.effectiveSortColumn())
	nextIndex := (currentIndex + 1) % len(columns)
	c.sortByColumnKeepingDirection(columns[nextIndex])
}

func (c *RowSelectionTable[T]) previousSortOrder() {
	columns := c.shownColumns()
	if len(columns) == 0 {
		return
	}
	currentIndex := slices.Index(columns, c.effectiveSortColumn())
	nextIndex := (len(columns) + currentIndex - 1) % len(columns)
	c.sortByColumnKeepingDirection(columns[nextIndex])
}

// sortByColumnKeepingDirection sorts by the column, in the current direction.
func (c *RowSelectionTable[T]) sortByColumnKeepingDirection(column *Column) {
	c.SortBy(column, c.sortInverted)
	c.updateTableContents()
	c.notifyColumnLayoutChanged()
}

// isBesideTheColumns returns whether the position is in the table, but to the right of its last column.
func (c *RowSelectionTable[T]) isBesideTheColumns(x int, y int) bool {
	if !c.table.InRect(x, y) {
		return false
	}
	_, column := c.table.CellAt(x, y)
	return column < 0
}

// handleHeaderClick handles clicks on the header row: like the keys, the first click selects it, the next ones
// sort by the clicked column, or toggle the direction if it is the sort column already. Clicks elsewhere are
// passed on. Runs as the mouse capture of the table.
func (c *RowSelectionTable[T]) handleHeaderClick(action tview.MouseAction, event *tcell.EventMouse) (tview.MouseAction, *tcell.EventMouse) {
	x, y := event.Position()
	switch action {
	case tview.MouseLeftDown:
		// before tview focuses the table: the click that selects the header must not sort as well
		selectedRow, _ := c.table.GetSelection()
		c.headerWasSelected = selectedRow == 0 && c.table.HasFocus()
	case tview.MouseLeftClick:
		if !c.table.InRect(x, y) || !c.headerWasSelected {
			return action, event
		}
		row, column := c.table.CellAt(x, y)
		columns := c.visibleColumns()
		if row != 0 || column < 0 || column >= len(columns) {
			return action, event
		}
		if columns[column] == c.effectiveSortColumn() {
			c.toggleSortDirection()
		} else {
			c.sortByColumnKeepingDirection(columns[column])
		}
		return tview.MouseConsumed, nil
	default:
	}
	return action, event
}

func (c *RowSelectionTable[T]) toggleSortDirection() {
	c.sortInverted = !c.sortInverted
	c.SortBy(c.sortByColumn, c.sortInverted)
	c.updateTableContents()
	c.notifyColumnLayoutChanged()
}

// RenderCells renders all cells again, e.g. after data they show changed outside of the entries.
// Must be called on the UI thread.
func (c *RowSelectionTable[T]) RenderCells() {
	c.updateTableContents()
}

func (c *RowSelectionTable[T]) updateTableContents() {

	table := c.table
	if table == nil {
		return
	}

	table.Clear()
	// tview follows the end of the rows once all of them fit (e.g. while the table is empty): new rows would be
	// shown scrolled to the end, hiding the selected row. Setting the offset stops that.
	table.SetOffset(table.GetOffset())
	c.renderedTimeFormat = uiutil.TimeFormatGeneration()
	columns := c.visibleColumns()
	sortColumn := c.effectiveSortColumn()

	// Table Header
	for column, tableColumn := range columns {
		cellColor := theme.Colors.Layout.Table.HeaderForeground
		cellAlignment := tableColumn.Alignment
		cellExpansion := 0

		cellText := tableColumn.Title
		if tableColumn == sortColumn {
			var sortDirectionIndicator = "↓"
			if !c.sortInverted {
				sortDirectionIndicator = "↑"
			}
			cellText = fmt.Sprintf("%s %s", cellText, sortDirectionIndicator)
		}
		if column == 0 && c.columnOffset > 0 {
			// columns are hidden on the left (horizontal scrolling)
			cellText = "◂ " + cellText
		}

		cell := tview.NewTableCell(cellText).
			SetTextColor(cellColor).
			SetBackgroundColor(theme.Colors.Layout.Table.HeaderBackground).
			SetAttributes(tcell.AttrBold).
			SetAlign(cellAlignment).
			SetExpansion(cellExpansion)
		if tableColumn == sortColumn {
			// a cell's selected style is only applied while its row is selected,
			// so this highlights the sort column only while the header row is selected
			cell.SetSelectedStyle(
				tcell.StyleDefault.
					Foreground(theme.Colors.Layout.Table.SortColumnSelectedForeground).
					Background(theme.Colors.Layout.Table.SortColumnSelectedBackground),
			)
		}
		table.SetCell(0, column, cell)
	}

	// Table Content, with a row above each group (see SetGroupings)
	c.rowEntries = []*T{nil}
	c.groupRows = map[int]groupRow{}
	var groups []Group
	counts := map[Group]int{}
	if c.grouping != nil {
		for _, entry := range c.entries {
			group := c.grouping.GroupOf(entry)
			groups = append(groups, group)
			counts[group]++
		}
	}
	for index, entry := range c.entries {
		if groups != nil && (index == 0 || groups[index] != groups[index-1]) {
			c.setGroupRow(len(c.rowEntries), len(columns), groupRow{group: groups[index], count: counts[groups[index]]})
			c.rowEntries = append(c.rowEntries, nil)
		}
		row := len(c.rowEntries)
		c.rowEntries = append(c.rowEntries, entry)
		cells := c.toTableCells(index, columns, entry)
		for column, cell := range cells {
			c.styleCell(entry, cell)
			table.SetCell(row, column, cell)
		}
	}
	if c.columnOffset == 0 {
		// not while scrolled horizontally: the hidden columns are not measured
		c.contentWidth = c.columnsWidth()
	}
	c.syncScrollbar()
}

func (c *RowSelectionTable[T]) Select(entry *T) {
	row := 0
	if entry != nil {
		row = slices.Index(c.rowEntries, entry)
		if row < 0 {
			return
		}
	}
	if row == 0 || entry == c.entries[0] {
		// the first entry: also the row of its group above it
		c.scrollToTop()
	}
	c.table.Select(row, 0)
	c.syncScrollbar()
}

// visibleColumns returns the displayed columns, without the ones hidden by horizontal scrolling.
func (c *RowSelectionTable[T]) visibleColumns() []*Column {
	shown := c.shownColumns()
	offset := min(c.columnOffset, max(len(shown)-1, 0))
	return shown[offset:]
}

// scrollColumns scrolls horizontally by delta columns: to the right only while columns are cut off on the right,
// and never so far that no column is left. Must be called on the UI thread.
//
// tview's own horizontal scrolling (its column offset) is not used: when the scrolled columns fit with space to
// spare, tview's Draw moves the offset back to the start ("don't waste space"), so tables that are only slightly
// too wide could not be scrolled, or jumped back on the next redraw.
// Workaround for https://github.com/rivo/tview/issues/1169: once a tview release with the fix is used, this can
// be replaced by tview's column offset again (keeping the "◂" indicator would then need GetOffset in Draw).
func (c *RowSelectionTable[T]) scrollColumns(delta int) {
	offset := c.columnOffset + delta
	if offset < 0 || offset >= len(c.shownColumns()) || offset == c.columnOffset {
		return
	}
	if delta > 0 && !c.isCutOffOnTheRight() {
		return
	}
	c.columnOffset = offset
	c.updateTableContents()
}

// isCutOffOnTheRight returns whether the displayed columns are wider than the table.
func (c *RowSelectionTable[T]) isCutOffOnTheRight() bool {
	_, _, width, _ := c.table.GetInnerRect()
	return c.columnsWidth() > width
}

// ContentSize returns the size the table needs to show all of its rows and columns, including its border, padding
// and scrollbar. The width does not change while it is scrolled horizontally. The height counts all entries,
// including the ones hidden by the filter, so filtering does not resize it; at least one row is counted for the
// placeholder. Must be called on the UI thread.
func (c *RowSelectionTable[T]) ContentSize() (width int, height int) {
	// border, padding and scrollbar
	width = c.contentWidth + 2 + 2 + 1
	// border and header, and the rows of the groups
	height = max(len(c.allEntries)+c.groupCount(c.allEntries), 1) + 2 + 1
	return width, height
}

// columnsWidth returns the width of the displayed columns, like tview measures them (the widest cell of each
// column, one space between columns).
func (c *RowSelectionTable[T]) columnsWidth() int {
	tableWidth := -1
	for column := 0; column < c.table.GetColumnCount(); column++ {
		columnWidth := 0
		for row := 0; row < c.table.GetRowCount(); row++ {
			cell := c.table.GetCell(row, column)
			cellWidth := tview.TaggedStringWidth(cell.Text)
			if cell.MaxWidth > 0 && cell.MaxWidth < cellWidth {
				cellWidth = cell.MaxWidth
			}
			columnWidth = max(columnWidth, cellWidth)
		}
		tableWidth += columnWidth + 1
	}
	return max(tableWidth, 0)
}

// EmbedInFrame removes the table's own border (and title), for a table that fills a frame with its own border,
// e.g. a dialog: two borders next to each other look odd. The footer (counts, filter) is drawn into the bottom
// border of the frame instead. Must be called on the UI thread, before the table is drawn.
func (c *RowSelectionTable[T]) EmbedInFrame(frame *tview.Box) {
	c.layout.SetBorder(false)
	text := c.footer.GetText()
	// an empty footer draws nothing
	c.footer.SetText("")
	c.footer.SetLeftFunc(nil)
	c.footer = uiutil.NewBorderFooter(frame)
	c.footer.SetLeftFunc(c.renderFilterFooter)
	c.footer.SetText(text)
}

// scrollToTop scrolls to the first row. Unlike tview's ScrollToBeginning, the horizontal scroll position is kept.
func (c *RowSelectionTable[T]) scrollToTop() {
	_, column := c.table.GetOffset()
	c.table.SetOffset(0, column)
}

func (c *RowSelectionTable[T]) HasFocus() bool {
	return c.layout.HasFocus()
}

// GetAllEntries returns all entries set with SetData, including the ones hidden by the filter.
func (c *RowSelectionTable[T]) GetAllEntries() []*T {
	return c.allEntries
}

// GetEntries returns the displayed entries, i.e. the ones that match the filter.
func (c *RowSelectionTable[T]) GetEntries() []*T {
	return c.entries
}

// FindByRowId returns the displayed entry with the TableRowId, nil if none is displayed. E.g. to select the entry that
// was selected again, after the entries were replaced by new ones (SetData).
func (c *RowSelectionTable[T]) FindByRowId(id string) *T {
	for _, entry := range c.entries {
		if (*entry).TableRowId() == id {
			return entry
		}
	}
	return nil
}

func (c *RowSelectionTable[T]) GetSelectedEntry() *T {
	row, _ := c.table.GetSelection()
	if row >= 0 && row < len(c.rowEntries) {
		return c.rowEntries[row]
	}
	return nil
}

func (c *RowSelectionTable[T]) IsEmpty() bool {
	return len(c.entries) <= 0
}

func (c *RowSelectionTable[T]) SetInputCapture(inputCapture func(event *tcell.EventKey) *tcell.EventKey) {
	c.inputCapture = inputCapture
}

func (c *RowSelectionTable[T]) SetSelectionChangedCallback(f func(selectedEntry *T)) {
	c.selectionChangedCallback = f
}

func (c *RowSelectionTable[T]) SelectHeader() {
	row, col := c.table.GetSelection()
	if row != 0 || col != 0 {
		c.table.Select(0, 0)
	}
}

func (c *RowSelectionTable[T]) SelectFirstIfExists() {
	if len(c.entries) > 0 {
		c.Select(c.entries[0])
	}
}

// toggleMultiSelection toggles the selection state of the given entry for the "multi selection" feature.
func (c *RowSelectionTable[T]) toggleMultiSelection(entry *T) {
	if entry == nil {
		return
	}
	if c.isInMultiSelection(entry) {
		c.removeFromMultiSelection(entry)
	} else {
		c.addToMultiSelection(entry)
	}
}

// isInMultiSelection returns true if the given entry is selected for the "multi selection" feature.
func (c *RowSelectionTable[T]) isInMultiSelection(entry *T) bool {
	if entry == nil {
		return false
	}
	entryId := c.createMultiSelectionEntryId(entry)
	entry, ok := c.multiSelectionEntryMap[entryId]
	return ok && entry != nil
}

// removeFromMultiSelection removes the given entry from the selected entries for the "multi selection" feature.
func (c *RowSelectionTable[T]) removeFromMultiSelection(entry *T) {
	if entry == nil {
		return
	}
	if c.isInMultiSelection(entry) {
		entryId := c.createMultiSelectionEntryId(entry)
		delete(c.multiSelectionEntryMap, entryId)
		c.updateTableContents()
	}
}

// addToMultiSelection adds the given entry to the selected entries for the "multi selection" feature.
func (c *RowSelectionTable[T]) addToMultiSelection(entry *T) {
	if entry == nil {
		return
	}
	entryId := c.createMultiSelectionEntryId(entry)
	c.multiSelectionEntryMap[entryId] = entry
	c.updateTableContents()
}

func (c *RowSelectionTable[T]) createMultiSelectionEntryId(entry *T) string {
	switch e := any(entry).(type) {
	case RowSelectionTableEntry:
		return e.TableRowId()
	default:
		return fmt.Sprintf("%v", e)
	}

}

// ClearMultiSelection clears all selected entries for the "multi selection" feature.
func (c *RowSelectionTable[T]) ClearMultiSelection() {
	c.multiSelectionEntryMap = make(map[string]*T)
	c.updateTableContents()
}

// GetMultiSelection returns all selected entries for the "multi selection" feature.
func (c *RowSelectionTable[T]) GetMultiSelection() []*T {
	entries := maps.Values(c.multiSelectionEntryMap)
	return slices.Collect(entries)
}

// HasMultiSelection returns true if there are any selected entries for the "multi selection" feature.
func (c *RowSelectionTable[T]) HasMultiSelection() bool {
	return len(c.multiSelectionEntryMap) > 0
}

// cleanupMultiSelection removes all entries from the "multi selection" feature that are not part of the current table entries.
func (c *RowSelectionTable[T]) cleanupMultiSelection() {
	currentEntryIds := []string{}
	for _, entry := range c.entries {
		entryId := c.createMultiSelectionEntryId(entry)
		currentEntryIds = append(currentEntryIds, entryId)
	}

	for entryId := range c.multiSelectionEntryMap {
		if !slices.Contains(currentEntryIds, entryId) {
			delete(c.multiSelectionEntryMap, entryId)
		}
	}
}

// Up moves the current selection up one row, over the rows of the groups
func (c *RowSelectionTable[T]) Up() {
	row, col := c.table.GetSelection()
	row--
	for row > 0 && c.isGroupRow(row) {
		row--
	}
	if row >= 0 {
		c.table.Select(row, col)
	}
}

// Down moves the current selection down one row, over the rows of the groups
func (c *RowSelectionTable[T]) Down() {
	row, col := c.table.GetSelection()
	row++
	for row < len(c.rowEntries) && c.isGroupRow(row) {
		row++
	}
	if row < len(c.rowEntries) {
		c.table.Select(row, col)
	}
}

func (c *RowSelectionTable[T]) PageUp() {

}

func (c *RowSelectionTable[T]) PageDown() {

}

// SetFilterFunc enables filtering: pressing Ctrl+F or / starts typing a filter, which hides all entries that don't match.
// This replaces tview's Ctrl+F (page down) for this table, PgDn still works.
// While typing, the filter can be edited like a terminal input line (see lineEditor), ↑/↓/PgUp/PgDn still
// navigate the list, Enter keeps the filter, Esc clears it and Backspace on an empty filter stops typing.
// Esc also clears an active filter while not typing. See MatchesGlob for a matcher.
func (c *RowSelectionTable[T]) SetFilterFunc(matches func(entry *T, filterText string) bool) {
	c.filterMatches = matches
}

// SetFilterChangedCallback sets a function that is called (on the UI thread) after the filter text changed,
// e.g. to update a footer showing the number of matches.
func (c *RowSelectionTable[T]) SetFilterChangedCallback(f func()) {
	c.filterChangedCallback = f
}

// GetFilterText returns the current filter text, "" if no filter is active.
func (c *RowSelectionTable[T]) GetFilterText() string {
	return c.filterText
}

// IsFilterActive returns whether a filter is active, i.e. whether entries may be hidden.
func (c *RowSelectionTable[T]) IsFilterActive() bool {
	return c.filterText != ""
}

// IsFiltered returns whether entries may be hidden, by the typed filter or the extra filter (e.g. for footers like
// "5 of 300 entries").
func (c *RowSelectionTable[T]) IsFiltered() bool {
	return c.IsFilterActive() || c.extraFilter != nil
}

// SetExtraFilter hides the entries for which shown returns false, in addition to the typed filter (nil: none).
// The selection is kept if it is still shown. Must be called on the UI thread.
func (c *RowSelectionTable[T]) SetExtraFilter(shown func(entry *T) bool) {
	c.extraFilter = shown
	c.refilter()
}

// RefreshFilter applies the filters again, e.g. after what the extra filter depends on changed.
// Must be called on the UI thread.
func (c *RowSelectionTable[T]) RefreshFilter() {
	shown := c.filterEntries(c.allEntries)
	if len(shown) == len(c.entries) {
		current := make(map[*T]bool, len(c.entries))
		for _, entry := range c.entries {
			current[entry] = true
		}
		unchanged := true
		for _, entry := range shown {
			if !current[entry] {
				unchanged = false
				break
			}
		}
		if unchanged {
			return
		}
	}
	c.refilter()
}

// IsEditingFilter returns whether the filter is currently being typed.
func (c *RowSelectionTable[T]) IsEditingFilter() bool {
	return c.isEditingFilter
}

// SetFilterText sets the filter and updates the displayed entries.
// The selected entry stays selected if it still matches, otherwise the first match is selected.
// Must be called on the UI thread.
func (c *RowSelectionTable[T]) SetFilterText(filterText string) {
	c.filterEditor.Reset(filterText)
	c.applyFilterText(filterText)
	c.updateTitle()
}

// applyFilterText updates the displayed entries for the given filter text, without touching the filter editor.
func (c *RowSelectionTable[T]) applyFilterText(filterText string) {
	if filterText == c.filterText {
		return
	}
	c.filterText = filterText
	c.refilter()
}

// refilter shows the entries that match the filters, keeping the selection if it is still shown.
func (c *RowSelectionTable[T]) refilter() {
	previousSelection := c.GetSelectedEntry()
	headerSelected := previousSelection == nil && len(c.entries) > 0

	c.isUpdatingData = true
	c.entriesMutex.Lock()
	c.entries = c.filterEntries(c.allEntries)
	c.entriesMutex.Unlock()
	c.SortBy(c.sortByColumn, c.sortInverted)
	c.cleanupMultiSelection()
	c.updateTableContents()
	c.isUpdatingData = false

	// select explicitly, so the selection changed callback keeps the owner in sync
	switch {
	case headerSelected:
		c.SelectHeader()
	case previousSelection != nil && slices.Contains(c.entries, previousSelection):
		c.Select(previousSelection)
	case len(c.entries) > 0:
		c.Select(c.entries[0])
	default:
		c.table.Select(0, 0)
	}

	c.updateTitle()
	if c.filterChangedCallback != nil {
		c.filterChangedCallback()
	}
}

func (c *RowSelectionTable[T]) filterEntries(entries []*T) []*T {
	textFilter := c.filterMatches != nil && c.filterText != ""
	if !textFilter && c.extraFilter == nil {
		return entries
	}
	result := make([]*T, 0, len(entries))
	for _, entry := range entries {
		if textFilter && !c.filterMatches(entry, c.filterText) {
			continue
		}
		if c.extraFilter != nil && !c.extraFilter(entry) {
			continue
		}
		result = append(result, entry)
	}
	return result
}

func (c *RowSelectionTable[T]) startEditingFilter() {
	c.filterEditor.Reset(c.filterText)
	c.isEditingFilter = true
	// the focused primitive is the table or its view, see tableView
	uiutil.SetTextInputActive(c.table, true)
	uiutil.SetTextInputActive(c.view, true)
	c.updateTitle()
}

func (c *RowSelectionTable[T]) stopEditingFilter() {
	if !c.isEditingFilter {
		return
	}
	c.isEditingFilter = false
	uiutil.SetTextInputActive(c.table, false)
	uiutil.SetTextInputActive(c.view, false)
	c.updateTitle()
}

// handleFilterInput handles the keys for typing and clearing the filter.
// If handled is true, the returned event is the result of the input capture.
func (c *RowSelectionTable[T]) handleFilterInput(event *tcell.EventKey) (result *tcell.EventKey, handled bool) {
	if !c.isEditingFilter {
		switch {
		case event.Key() == tcell.KeyCtrlF || (event.Key() == tcell.KeyRune && event.Rune() == '/' && event.Modifiers() == tcell.ModNone):
			c.startEditingFilter()
			return nil, true
		case event.Key() == tcell.KeyEscape && c.IsFilterActive():
			c.SetFilterText("")
			return nil, true
		}
		return event, false
	}

	switch event.Key() {
	case tcell.KeyEnter:
		c.stopEditingFilter()
		return nil, true
	case tcell.KeyEscape:
		c.stopEditingFilter()
		c.SetFilterText("")
		return nil, true
	case tcell.KeyBackspace, tcell.KeyBackspace2:
		if c.filterText == "" {
			c.stopEditingFilter()
			return nil, true
		}
	case tcell.KeyUp, tcell.KeyDown, tcell.KeyPgUp, tcell.KeyPgDn:
		// navigate the list while typing, skipping the owner's shortcuts
		return event, true
	}

	if c.filterEditor.HandleKey(event) {
		c.applyFilterText(c.filterEditor.Text())
		// also for cursor movements
		c.updateTitle()
	}
	// all other keys are consumed while typing
	return nil, true
}

// SetClickOnSelectedFunc calls onClick with the selected entry when its highlighted row is clicked, e.g. to check or
// uncheck it; a click on another row only selects it (see uiutil.OnClickOnSelectedRow). Not for the header row, which
// sorts, or the rows of groups. Must be called on the UI thread.
func (c *RowSelectionTable[T]) SetClickOnSelectedFunc(onClick func(entry *T)) {
	uiutil.OnClickOnSelectedRow(c.table, func(int) {
		if entry := c.GetSelectedEntry(); entry != nil {
			onClick(entry)
		}
	})
}
