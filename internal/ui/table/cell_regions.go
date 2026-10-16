package table

import (
	"zfs-file-history/internal/ui/clickable"
	"zfs-file-history/internal/ui/theme"
)

// CellRegion is a part of the text of a cell that reacts to the mouse, see SetCellRegionsFunc.
type CellRegion struct {
	// Start and Width are the cells of the text it covers, from the left of the cell (left-aligned columns only)
	Start, Width int
	// Key identifies it across draws, see clickable.Region
	Key any
	// OnClick is called on the UI thread when it is clicked
	OnClick func()
}

// SetCellRegionsFunc sets a function that returns the parts of the cell of an entry in a column that react to the
// mouse: highlighted while hovered, and running OnClick when clicked instead of selecting the row (e.g. the toggle of
// a tree). Must be called on the UI thread.
func (c *RowSelectionTable[T]) SetCellRegionsFunc(cellRegions func(entry *T, column *Column) []CellRegion) {
	c.cellRegions = cellRegions
}

// addCellRegions adds the cell regions of the visible rows where they were drawn. The columns are where tview drew
// them (CellAt), so horizontal scrolling and the widths of the columns need not be computed again. Runs while drawing.
func (c *RowSelectionTable[T]) addCellRegions() {
	c.regions.Reset()
	if c.cellRegions == nil {
		return
	}
	x, y, width, height := c.table.GetInnerRect()
	// the columns of the table, without the ones scrolled out on the left (see scrollColumns)
	columns := c.visibleColumns()
	// the left edge of each column as drawn, -1 for the ones not drawn
	columnX := make([]int, len(columns))
	for i := range columnX {
		columnX[i] = -1
	}
	for screenX := x; screenX < x+width; screenX++ {
		if _, column := c.table.CellAt(screenX, y); column >= 0 && column < len(columnX) && columnX[column] < 0 {
			columnX[column] = screenX
		}
	}
	rowOffset, _ := c.table.GetOffset()
	// below the fixed header row
	for screenY := y + 1; screenY < y+height; screenY++ {
		row := screenY - y + rowOffset
		if row >= len(c.rowEntries) {
			break
		}
		entry := c.rowEntries[row]
		if entry == nil {
			continue
		}
		for column, left := range columnX {
			if left < 0 {
				continue
			}
			for _, region := range c.cellRegions(entry, columns[column]) {
				start := max(left+region.Start, x)
				end := min(left+region.Start+region.Width, x+width)
				c.regions.Add(clickable.Region{
					X: start, Y: screenY, Width: end - start, Key: region.Key,
					HoverBackground: theme.Colors.Layout.CellHoverBackground,
					OnClick:         region.OnClick,
				})
			}
		}
	}
}

// MouseHandler runs a clicked cell region (see SetCellRegionsFunc) instead of selecting its row, and otherwise
// handles the mouse like the table, see clickable.Regions.WrapMouseHandler.
func (v *tableView) MouseHandler() clickable.MouseHandler {
	enabled := func() bool { return v.regionsEnabled == nil || v.regionsEnabled() }
	return v.regions.WrapMouseHandler(v, enabled, v.Table.MouseHandler())
}
