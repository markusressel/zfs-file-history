package table

import (
	"fmt"
	"maps"
	"slices"
	"sync"
	"zfs-file-history/internal/ui/scrollbar"
	"zfs-file-history/internal/ui/theme"
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
	Id        ColumnId
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

	layout    *tview.Flex
	table     *tview.Table
	scrollbar *scrollbar.ScrollbarComponent

	entries      []*T
	entriesMutex sync.Mutex

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

	defaultSortColumn   *Column
	defaultSortInverted bool

	isScrollbarVisible bool
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
	return tableContainer
}

func (c *RowSelectionTable[T]) SetMultiSelect(multiSelect bool) {
	c.multiSelectEnabled = multiSelect
	c.ClearMultiSelection()
}

func (c *RowSelectionTable[T]) createLayout() {
	table := tview.NewTable()

	table.SetBorder(true)
	table.SetBorders(false)
	table.SetBorderPadding(0, 0, 1, 1)

	// fixed header row
	table.SetFixed(1, 0)

	table.SetSelectedStyle(
		tcell.StyleDefault.
			Foreground(theme.Colors.Layout.Table.SelectedForeground).
			Background(theme.Colors.Layout.Table.SelectedBackground),
	)

	uiutil.SetupWindow(table, "")

	table.SetSelectable(true, false)
	table.SetSelectionChangedFunc(func(row, column int) {
		selectedEntry := c.GetSelectedEntry()

		if c.lastSelectedEntry != nil && selectedEntry == nil {
			// workaround to the table not scrolling to the top when PgUp is pressed and a page jump will
			// select the table header row.
			table.ScrollToBeginning()
		}
		c.selectionChangedCallback(selectedEntry)

		c.lastSelectedEntry = selectedEntry
		c.syncScrollbar()
	})

	table.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
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

	c.isScrollbarVisible = true
	c.layout = tview.NewFlex().
		SetDirection(tview.FlexColumn).
		AddItem(c.table, 0, 1, true).
		AddItem(c.scrollbar.GetLayout(), 1, 0, false)
}

func (c *RowSelectionTable[T]) syncScrollbar() {
	if c.scrollbar == nil || c.table == nil {
		return
	}

	rowOffset, _ := c.table.GetOffset()
	rowCount := c.table.GetRowCount()
	_, _, _, height := c.table.GetInnerRect()

	// rowCount - 1 because of the header row
	if rowCount-1 <= height {
		c.hideScrollbar()
		return
	} else {
		c.showScrollbar()
	}

	if c.isScrollbarVisible {
		c.scrollbar.SetMax(rowCount)
		c.scrollbar.SetPosition(rowOffset)
		c.scrollbar.SetWidth(height)
	}
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

func (c *RowSelectionTable[T]) SetTitle(title string) {
	uiutil.SetupWindow(c.table, title)
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

func (c *RowSelectionTable[T]) SetActiveColumns(columns []*Column) {
	if len(columns) <= 0 {
		return
	}

	c.columnSpec = slices.Clone(columns)

	if !slices.Contains(c.columnSpec, c.sortByColumn) {
		if c.defaultSortColumn != nil && slices.Contains(c.columnSpec, c.defaultSortColumn) {
			c.sortByColumn = c.defaultSortColumn
			c.sortInverted = c.defaultSortInverted
		} else {
			c.sortByColumn = c.columnSpec[0]
		}
	}

	c.SortBy(c.sortByColumn, c.sortInverted)
	c.updateTableContents()
}

func (c *RowSelectionTable[T]) GetColumnSpec() []*Column {
	return slices.Clone(c.columnSpec)
}

func (c *RowSelectionTable[T]) SetData(entries []*T) {
	c.entriesMutex.Lock()
	c.entries = entries
	c.entriesMutex.Unlock()
	c.SortBy(c.sortByColumn, c.sortInverted)
	c.cleanupMultiSelection()
	c.updateTableContents()
}

func (c *RowSelectionTable[T]) SortBy(sortOption *Column, inverted bool) {
	c.entriesMutex.Lock()
	c.sortByColumn = sortOption
	c.sortInverted = inverted
	c.entries = c.sortTableEntries(c.entries, c.sortByColumn, c.sortInverted)
	c.entriesMutex.Unlock()
}

func (c *RowSelectionTable[T]) nextSortOrder() {
	currentIndex := slices.Index(c.columnSpec, c.sortByColumn)
	nextIndex := (currentIndex + 1) % len(c.columnSpec)
	column := c.columnSpec[nextIndex]
	c.SortBy(column, c.sortInverted)
	c.updateTableContents()
}

func (c *RowSelectionTable[T]) previousSortOrder() {
	currentIndex := slices.Index(c.columnSpec, c.sortByColumn)
	nextIndex := (len(c.columnSpec) + currentIndex - 1) % len(c.columnSpec)
	column := c.columnSpec[nextIndex]
	c.SortBy(column, c.sortInverted)
	c.updateTableContents()
}

func (c *RowSelectionTable[T]) toggleSortDirection() {
	c.sortInverted = !c.sortInverted
	c.SortBy(c.sortByColumn, c.sortInverted)
	c.updateTableContents()
}

func (c *RowSelectionTable[T]) updateTableContents() {

	table := c.table
	if table == nil {
		return
	}

	table.Clear()

	// Table Header
	for column, tableColumn := range c.columnSpec {
		cellColor := tcell.ColorWhite
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

		cell := tview.NewTableCell(cellText).
			SetTextColor(cellColor).
			SetAlign(cellAlignment).
			SetExpansion(cellExpansion)
		table.SetCell(0, column, cell)
	}

	// Table Content
	for row, entry := range c.entries {
		cells := c.toTableCells(row, c.columnSpec, entry)
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
		c.table.ScrollToBeginning()
	}
	c.table.Select(index, 0)
	c.syncScrollbar()
	c.application.ForceDraw()
}

func (c *RowSelectionTable[T]) HasFocus() bool {
	return c.layout.HasFocus()
}

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
