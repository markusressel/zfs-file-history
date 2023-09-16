package table

import (
	"fmt"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"golang.org/x/exp/slices"
	uiutil "zfs-file-history/internal/ui/util"
	"zfs-file-history/internal/util"
)

type ColumnId int

type Column struct {
	Id        ColumnId
	Title     string
	Alignment int
}

type RowSelectionTable[T any] struct {
	application *tview.Application

	layout *tview.Table

	entries       []*T
	selectedEntry *T

	sortByColumn     *Column
	sortTableEntries func(entries []*T, column *Column, inverted bool) []*T
	toTableCells     func(row int, columns []*Column, entry *T) (cells []*tview.TableCell)
	columnSpec       []*Column
	sortInverted     bool
}

func NewTableContainer[T any](
	application *tview.Application,
	toTableCells func(row int, columns []*Column, entry *T) (cells []*tview.TableCell),
	sortTableEntries func(entries []*T, column *Column, inverted bool) []*T,
) *RowSelectionTable[T] {
	tableContainer := &RowSelectionTable[T]{
		application:      application,
		toTableCells:     toTableCells,
		sortTableEntries: sortTableEntries,
	}
	tableContainer.createLayout()
	return tableContainer
}

func (c *RowSelectionTable[T]) createLayout() {
	table := tview.NewTable()

	table.SetMouseCapture(func(action tview.MouseAction, event *tcell.EventMouse) (tview.MouseAction, *tcell.EventMouse) {
		switch action {
		case tview.MouseLeftDoubleClick:
			go func() {
				c.onItemDoubleClicked()
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

	uiutil.SetupWindow(table, "")

	table.SetSelectable(true, false)

	table.SetSelectionChangedFunc(func(row int, column int) {
		selectionIndex := util.Coerce(row-1, -1, len(c.entries)-1)
		var newSelection *T
		if selectionIndex < 0 {
			newSelection = nil
		} else {
			newSelection = c.entries[selectionIndex]
		}

		c.selectEntry(newSelection)
	})

	table.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		key := event.Key()
		if c.selectedEntry == nil {
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

func (c *RowSelectionTable[T]) SetData(columns []*Column, entries []*T) {
	c.columnSpec = columns
	c.entries = entries

	c.sortByColumn = columns[0]
	c.entries = c.sortTableEntries(c.entries, c.sortByColumn, c.sortInverted)

	c.updateTableContents()
}

func (c *RowSelectionTable[T]) onItemDoubleClicked() {
	// TODO: implement
}

func (c *RowSelectionTable[T]) selectEntry(selection *T) {
	c.selectedEntry = selection
}

func (c *RowSelectionTable[T]) nextSortOrder() {
	currentIndex := slices.Index(c.columnSpec, c.sortByColumn)
	nextIndex := (currentIndex + 1) % len(c.columnSpec)
	c.sortByColumn = c.columnSpec[nextIndex]
	c.entries = c.sortTableEntries(c.entries, c.sortByColumn, c.sortInverted)
	c.updateTableContents()
}

func (c *RowSelectionTable[T]) previousSortOrder() {
	currentIndex := slices.Index(c.columnSpec, c.sortByColumn)
	nextIndex := (len(c.columnSpec) + currentIndex - 1) % len(c.columnSpec)
	c.sortByColumn = c.columnSpec[nextIndex]
	c.entries = c.sortTableEntries(c.entries, c.sortByColumn, c.sortInverted)
	c.updateTableContents()
}

func (c *RowSelectionTable[T]) toggleSortDirection() {
	c.sortInverted = !c.sortInverted
	c.entries = c.sortTableEntries(c.entries, c.sortByColumn, c.sortInverted)
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
	} else {
		c.layout.ScrollToBeginning()
	}
	c.layout.Select(index, 0)
}

func (c *RowSelectionTable[T]) HasFocus() bool {
	return c.layout.HasFocus()
}

func (c *RowSelectionTable[T]) GetEntries() []*T {
	return c.entries
}

func (c *RowSelectionTable[T]) GetSelectedEntry() *T {
	row, _ := c.layout.GetSelection()
	if row >= 1 {
		return c.entries[row-1]
	} else {
		return nil
	}
}

func (c *RowSelectionTable[T]) IsEmpty() bool {
	return len(c.entries) <= 0
}
