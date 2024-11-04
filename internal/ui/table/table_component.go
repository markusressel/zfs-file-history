package table

import (
	"fmt"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"maps"
	"slices"
	"sync"
	"zfs-file-history/internal/ui/theme"
	uiutil "zfs-file-history/internal/ui/util"
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

	layout *tview.Table

	entries      []*T
	entriesMutex sync.Mutex

	multiSelectEnabled     bool
	multiSelectionEntryMap map[string]*T

	sortByColumn     *Column
	sortTableEntries func(entries []*T, column *Column, inverted bool) []*T
	toTableCells     func(row int, columns []*Column, entry *T) (cells []*tview.TableCell)

	inputCapture             func(event *tcell.EventKey) *tcell.EventKey
	doubleClickCallback      func()
	selectionChangedCallback func(selectedEntry *T)

	columnSpec   []*Column
	sortInverted bool
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
		doubleClickCallback:      func() {},
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

	table.SetMouseCapture(func(action tview.MouseAction, event *tcell.EventMouse) (tview.MouseAction, *tcell.EventMouse) {
		switch action {
		case tview.MouseLeftDoubleClick:
			go func() {
				c.doubleClickCallback()
			}()
			return action, nil
		}
		return action, event
	})

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
		c.selectionChangedCallback(selectedEntry)
	})

	table.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		event = c.inputCapture(event)
		if event == nil {
			return event
		}
		key := event.Key()

		// current selection is on HEADER row
		if c.GetSelectedEntry() == nil {
			if key == tcell.KeyRight {
				c.nextSortOrder()
				return nil
			} else if key == tcell.KeyLeft {
				c.previousSortOrder()
				return nil
			} else if key == tcell.KeyEnter {
				c.toggleSortDirection()
				return nil
			}
		}

		// current selection is on DATA row
		if event.Rune() == SpaceRune && c.multiSelectEnabled {
			currentEntry := c.GetSelectedEntry()
			c.toggleMultiSelection(currentEntry)
		}

		return event
	})

	c.layout = table
}

func (c *RowSelectionTable[T]) GetLayout() *tview.Table {
	return c.layout
}

func (c *RowSelectionTable[T]) SetTitle(title string) {
	uiutil.SetupWindow(c.layout, title)
}

func (c *RowSelectionTable[T]) SetColumnSpec(columns []*Column, defaultSortColumn *Column, inverted bool) {
	c.columnSpec = columns
	c.SortBy(defaultSortColumn, inverted)
	c.updateTableContents()
}

func (c *RowSelectionTable[T]) SetData(entries []*T) {
	c.entriesMutex.Lock()
	c.entries = entries
	c.SortBy(c.sortByColumn, c.sortInverted)
	c.cleanupMultiSelection()
	c.entriesMutex.Unlock()
	c.updateTableContents()
}

func (c *RowSelectionTable[T]) SetDoubleClickCallback(f func()) {
	c.doubleClickCallback = f
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

	table := c.layout
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
		c.layout.ScrollToBeginning()
	}
	c.layout.Select(index, 0)
	c.application.ForceDraw()
}

func (c *RowSelectionTable[T]) HasFocus() bool {
	return c.layout.HasFocus()
}

func (c *RowSelectionTable[T]) GetEntries() []*T {
	return c.entries
}

func (c *RowSelectionTable[T]) GetSelectedEntry() *T {
	row, _ := c.layout.GetSelection()
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
	row, col := c.layout.GetSelection()
	if row != 0 || col != 0 {
		c.layout.Select(0, 0)
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
