package table

import (
	"fmt"
	"maps"
	"slices"
	"sync"
	"time"
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
	table  *tview.Table
	// view is the table as placed in the layout, see tableView
	view *tableView
	// renderedTimeFormat is the uiutil.TimeFormatGeneration the cells were rendered with
	renderedTimeFormat uint64
	scrollbar          *scrollbar.ScrollbarComponent

	// allEntries are all entries set with SetData, entries are the ones that match the filter (displayed)
	allEntries   []*T
	entries      []*T
	entriesMutex sync.Mutex

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

	columnSpec   []*Column
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

	lastSyncHeight int
	resizeTimer    *time.Timer
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
	c.view = &tableView{Table: c.table, beforeDraw: c.renderIfTimeFormatChanged, afterDraw: c.redrawScrollbar}

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
}

func (v *tableView) Draw(screen tcell.Screen) {
	v.beforeDraw()
	v.Table.Draw(screen)
	v.afterDraw(screen)
}

// renderIfTimeFormatChanged renders the cells again if times are formatted differently now, e.g. after switching
// to relative times, or because "1 minute ago" became "2 minutes ago". Runs while drawing.
func (c *RowSelectionTable[T]) renderIfTimeFormatChanged() {
	if c.renderedTimeFormat != uiutil.TimeFormatGeneration() {
		c.updateTableContents()
	}
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

func (c *RowSelectionTable[T]) UpdateEntry(entry *T) {
	c.entriesMutex.Lock()
	defer c.entriesMutex.Unlock()

	if entry == nil {
		return
	}

	index := slices.Index(c.entries, entry)
	if index < 0 {
		return
	}

	cells := c.toTableCells(index, c.visibleColumns(), entry)
	for column, cell := range cells {
		if c.isInMultiSelection(entry) {
			cell.SetBackgroundColor(theme.Colors.Layout.Table.MultiSelectionBackground)
			cell.SetTextColor(theme.Colors.Layout.Table.MultiSelectionForeground)
			cell.SetSelectedStyle(
				tcell.StyleDefault.Background(theme.Colors.Layout.Table.MultiSelectionBackground),
			)
		}
		c.table.SetCell(index+1, column, cell)
	}
}

func (c *RowSelectionTable[T]) SortBy(sortOption *Column, inverted bool) {
	c.entriesMutex.Lock()
	c.sortByColumn = sortOption
	c.sortInverted = inverted
	c.entries = c.sortTableEntries(c.entries, c.sortByColumn, c.sortInverted)
	c.entriesMutex.Unlock()
}

// IsSortedBy returns whether the entries are sorted by the column.
func (c *RowSelectionTable[T]) IsSortedBy(column *Column) bool {
	return c.sortByColumn == column
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
	currentIndex := slices.Index(c.columnSpec, c.sortByColumn)
	nextIndex := (currentIndex + 1) % len(c.columnSpec)
	column := c.columnSpec[nextIndex]
	c.SortBy(column, c.sortInverted)
	c.updateTableContents()
	c.notifyColumnLayoutChanged()
}

func (c *RowSelectionTable[T]) previousSortOrder() {
	currentIndex := slices.Index(c.columnSpec, c.sortByColumn)
	nextIndex := (len(c.columnSpec) + currentIndex - 1) % len(c.columnSpec)
	column := c.columnSpec[nextIndex]
	c.SortBy(column, c.sortInverted)
	c.updateTableContents()
	c.notifyColumnLayoutChanged()
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

	// Table Header
	for column, tableColumn := range columns {
		cellColor := theme.Colors.Layout.Table.HeaderForeground
		cellAlignment := tableColumn.Alignment
		cellExpansion := 0

		cellText := tableColumn.Title
		if tableColumn == c.sortByColumn {
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
		if tableColumn == c.sortByColumn {
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

	// Table Content
	for row, entry := range c.entries {
		cells := c.toTableCells(row, columns, entry)
		for column, cell := range cells {
			if c.isInMultiSelection(entry) {
				cell.SetBackgroundColor(theme.Colors.Layout.Table.MultiSelectionBackground)
				cell.SetTextColor(theme.Colors.Layout.Table.MultiSelectionForeground)
				cell.SetSelectedStyle(
					tcell.StyleDefault.Background(theme.Colors.Layout.Table.MultiSelectionBackground),
				)
			}
			table.SetCell(row+1, column, cell)
		}
	}
	c.syncScrollbar()
}

func (c *RowSelectionTable[T]) Select(entry *T) {
	index := 0
	if entry != nil {
		index = slices.Index(c.entries, entry)
		if index < 0 {
			return
		} else {
			index += 1
		}
	}
	if index <= 1 {
		c.scrollToTop()
	}
	c.table.Select(index, 0)
	c.syncScrollbar()
}

// visibleColumns returns the displayed columns, without the ones hidden by horizontal scrolling.
func (c *RowSelectionTable[T]) visibleColumns() []*Column {
	offset := min(c.columnOffset, max(len(c.columnSpec)-1, 0))
	return c.columnSpec[offset:]
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
	if offset < 0 || offset >= len(c.columnSpec) || offset == c.columnOffset {
		return
	}
	if delta > 0 && !c.isCutOffOnTheRight() {
		return
	}
	c.columnOffset = offset
	c.updateTableContents()
}

// isCutOffOnTheRight returns whether the displayed columns are wider than the table, like tview measures them
// (the widest cell of each column, one space between columns).
func (c *RowSelectionTable[T]) isCutOffOnTheRight() bool {
	_, _, width, _ := c.table.GetInnerRect()
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
	return tableWidth > width
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

func (c *RowSelectionTable[T]) GetSelectedEntry() *T {
	row, _ := c.table.GetSelection()
	row -= 1
	if row >= 0 && row < len(c.entries) {
		return c.entries[row]
	} else {
		return nil
	}
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

// Up moves the current selection up one row
func (c *RowSelectionTable[T]) Up() {
	row, col := c.table.GetSelection()
	if row > 0 {
		c.table.Select(row-1, col)
	}
}

// Down moves the current selection down one row
func (c *RowSelectionTable[T]) Down() {
	row, col := c.table.GetSelection()
	if row < len(c.entries) {
		c.table.Select(row+1, col)
	}
}

func (c *RowSelectionTable[T]) PageUp() {

}

func (c *RowSelectionTable[T]) PageDown() {

}

// SetFilterFunc enables filtering: pressing Ctrl+F starts typing a filter, which hides all entries that don't match.
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
		case event.Key() == tcell.KeyCtrlF:
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
