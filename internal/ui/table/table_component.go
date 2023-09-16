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

	rootLayout  *tview.Flex
	tableLayout *tview.Table

	entries       []*T
	selectedEntry *T
	title         string

	sortByColumn     *Column
	sortTableEntries func(entries []*T, column *Column, inverted bool) []*T
	toTableCells     func(row int, columns []*Column, entry *T) (cells []*tview.TableCell)
	tableColumns     []*Column
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
	flex := tview.NewFlex()
	table := tview.NewTable()
	flex.AddItem(table, 0, 1, true)

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

	uiutil.SetupWindow(table, c.title)

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

	c.tableLayout = table
	c.rootLayout = flex
}

func (c *RowSelectionTable[T]) GetLayout() *tview.Flex {
	return c.rootLayout
}

func (c *RowSelectionTable[T]) SetTitle(title string) {
	c.title = title
	c.updateTableContents()
}

func (c *RowSelectionTable[T]) SetData(columns []*Column, entries []*T) {
	c.tableColumns = columns
	c.entries = entries
	c.updateTableContents()
}

func (c *RowSelectionTable[T]) onItemDoubleClicked() {
	// TODO: implement
}

func (c *RowSelectionTable[T]) selectEntry(selection *T) {
	c.selectedEntry = selection
}

func (c *RowSelectionTable[T]) nextSortOrder() {
	currentIndex := slices.Index(c.tableColumns, c.sortByColumn)
	nextIndex := currentIndex + 1%len(c.tableColumns)
	c.sortByColumn = c.tableColumns[nextIndex]
	c.updateTableContents()
}

func (c *RowSelectionTable[T]) previousSortOrder() {
	currentIndex := slices.Index(c.tableColumns, c.sortByColumn)
	nextIndex := currentIndex - 1%len(c.tableColumns)
	c.sortByColumn = c.tableColumns[nextIndex]
	c.updateTableContents()
}

func (c *RowSelectionTable[T]) toggleSortDirection() {
	c.sortInverted = !c.sortInverted
	c.updateTableContents()
}

func (c *RowSelectionTable[T]) updateTableContents() {
	table := c.tableLayout
	if table == nil {
		return
	}

	table.Clear()

	uiutil.SetupWindow(table, c.title)

	tableEntries := c.sortTableEntries(c.entries, c.sortByColumn, c.sortInverted)

	// Table Header
	for column, tableColumn := range c.tableColumns {
		columnId := tableColumn
		cellColor := tcell.ColorWhite
		cellAlignment := tableColumn.Alignment
		cellExpansion := 0

		cellText := tableColumn.Title
		if columnId == c.sortByColumn {
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
	for row, entry := range tableEntries {
		cells := c.toTableCells(row, c.tableColumns, entry)
		for column, cell := range cells {
			table.SetCell(row+1, column, cell)
		}
	}
}

func (c *RowSelectionTable[T]) Select(row int) {
	c.tableLayout.Select(row, 0)
}

func (c *RowSelectionTable[T]) HasFocus() bool {
	return c.tableLayout.HasFocus()
}

func (c *RowSelectionTable[T]) GetEntries() []*T {
	return c.entries
}
