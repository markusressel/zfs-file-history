package ui

import (
	"fmt"
	"github.com/dustin/go-humanize"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"math"
	"strings"
	"time"
	"zfs-file-history/internal/data"
	uiutil "zfs-file-history/internal/ui/util"
	"zfs-file-history/internal/util"
)

type TableColumnId int

type TableContainer[T any] struct {
	application *tview.Application

	layout *tview.Table

	entries       []*T
	selectedEntry *T
	title         string
	sortByColumn  TableColumnId

	sortTableEntries func(entries []*T, column TableColumnId) []*T
	toTableCells     func(row int, entry *T) (cells []*tview.TableCell)
}

func NewTableContainer[T any](application *tview.Application) *TableContainer[T] {
	tableContainer := &TableContainer[T]{
		application: application,
	}
	tableContainer.createLayout()
	return tableContainer
}

func (c *TableContainer[T]) createLayout() {
	table := tview.NewTable()
	c.layout = table

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
			} else if key == tcell.KeyEnter || key == tcell.KeyUp {
				c.toggleSortOrder()
				return nil
			}
		}
		return event
	})

	c.layout = table
}

func (c *TableContainer[T]) SetTitle(title string) {
	c.title = title
}

func (c *TableContainer[T]) SetEntries(entries []*T) {
	c.entries = entries
}

func (c *TableContainer[T]) onItemDoubleClicked() {
	// TODO: implement
}

func (c *TableContainer[T]) selectEntry(selection *T) {
	c.selectedEntry = selection
}

func (c *TableContainer[T]) nextSortOrder() {

}

func (c *TableContainer[T]) previousSortOrder() {

}

func (c *TableContainer[T]) toggleSortOrder() {

}

func (c *TableContainer[T]) updateTableContents() {
	columnTitles := []FileBrowserColumn{Size, ModTime, Type, Status, Name}

	table := c.layout
	if table == nil {
		return
	}

	table.Clear()

	uiutil.SetupWindow(table, c.title)

	tableEntries := c.sortTableEntries(c.entries, c.sortByColumn)

	for row, entry := range tableEntries {
		cells := c.toTableCells(row, entry)
		for column, cell := range cells {
			table.SetCell(row, column, cell)
		}
	}

	cols, rows := len(columnTitles), len(tableEntries)+1
	fileIndex := 0
	for row := 0; row < rows; row++ {
		if (row) == 0 {

			for column := 0; column < cols; column++ {
				columnId := columnTitles[column]
				var cellColor = tcell.ColorWhite
				var cellText string
				var cellAlignment = tview.AlignLeft
				var cellExpansion = 0

				if columnId == Name {
					cellText = "Name"
				} else if columnId == ModTime {
					cellText = "Date/Time"
				} else if columnId == Type {
					cellText = "Type"
					cellAlignment = tview.AlignCenter
				} else if columnId == Status {
					cellText = "Diff"
					cellAlignment = tview.AlignCenter
				} else if columnId == Size {
					cellText = "Size"
					cellAlignment = tview.AlignCenter
				} else {
					panic("Unknown column")
				}

				if columnId == FileBrowserColumn(math.Abs(float64(fileBrowser.sortByColumn))) {
					var sortDirectionIndicator = "↓"
					if fileBrowser.sortByColumn > 0 {
						sortDirectionIndicator = "↑"
					}
					cellText = fmt.Sprintf("%s %s", cellText, sortDirectionIndicator)
				}

				table.SetCell(row, column,
					tview.NewTableCell(cellText).
						SetTextColor(cellColor).
						SetAlign(cellAlignment).
						SetExpansion(cellExpansion),
				)
			}
			continue
		}

		currentFileEntry := tableEntries[fileIndex]

		var status = "="
		var statusColor = tcell.ColorGray
		switch currentFileEntry.Status {
		case data.Equal:
			status = "="
			statusColor = tcell.ColorGray
		case data.Deleted:
			status = "-"
			statusColor = tcell.ColorRed
		case data.Added:
			status = "+"
			statusColor = tcell.ColorGreen
		case data.Modified:
			status = "≠"
			statusColor = tcell.ColorYellow
		case data.Unknown:
			status = "N/A"
			statusColor = tcell.ColorGray
		}

		var typeCellText = "?"
		var typeCellColor = tcell.ColorGray
		switch currentFileEntry.Type {
		case data.File:
			typeCellText = "F"
		case data.Directory:
			typeCellText = "D"
			typeCellColor = tcell.ColorSteelBlue
		case data.Link:
			typeCellText = "L"
			typeCellColor = tcell.ColorYellow
		}

		for column := 0; column < cols; column++ {
			columnId := columnTitles[column]
			var cellColor = tcell.ColorWhite
			var cellText string
			var cellAlignment = tview.AlignLeft
			var cellExpansion = 0

			if columnId == Name {
				cellText = currentFileEntry.Name
				if currentFileEntry.GetStat().IsDir() {
					cellText = fmt.Sprintf("/%s", cellText)
				}
				cellColor = statusColor
			} else if columnId == Type {
				cellText = typeCellText
				cellColor = typeCellColor
				cellAlignment = tview.AlignCenter
			} else if columnId == Status {
				cellText = status
				cellColor = statusColor
				cellAlignment = tview.AlignCenter
			} else if columnId == ModTime {
				cellText = currentFileEntry.GetStat().ModTime().Format(time.DateTime)

				switch currentFileEntry.Status {
				case data.Added, data.Deleted:
					cellColor = statusColor
				case data.Modified:
					if currentFileEntry.RealFile.Stat.ModTime() != currentFileEntry.SnapshotFiles[0].Stat.ModTime() {
						cellColor = statusColor
					} else {
						cellColor = tcell.ColorWhite
					}
				default:
					cellColor = tcell.ColorGray
				}
			} else if columnId == Size {
				cellText = humanize.IBytes(uint64(currentFileEntry.GetStat().Size()))
				if strings.HasSuffix(cellText, " B") {
					withoutSuffix := strings.TrimSuffix(cellText, " B")
					cellText = fmt.Sprintf("%s   B", withoutSuffix)
				}
				if len(cellText) < 10 {
					cellText = fmt.Sprintf("%s%s", strings.Repeat(" ", 10-len(cellText)), cellText)
				}

				switch currentFileEntry.Status {
				case data.Added, data.Deleted:
					cellColor = statusColor
				case data.Modified:
					if currentFileEntry.RealFile.Stat.Size() != currentFileEntry.SnapshotFiles[0].Stat.Size() {
						cellColor = statusColor
					} else {
						cellColor = tcell.ColorWhite
					}
				default:
					cellColor = tcell.ColorGray
				}

				cellAlignment = tview.AlignRight
			} else {
				panic("Unknown column")
			}

			table.SetCell(row, column,
				tview.NewTableCell(cellText).
					SetTextColor(cellColor).
					SetAlign(cellAlignment).
					SetExpansion(cellExpansion),
			)
		}
		fileIndex = (fileIndex + 1) % rows
	}
}
