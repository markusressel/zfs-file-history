package table

import (
	"strings"
	"testing"
	"zfs-file-history/internal/ui/theme"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"github.com/stretchr/testify/assert"
)

type mockEntry struct {
	id string
}

func (m mockEntry) TableRowId() string {
	return m.id
}

func TestTableSorting(t *testing.T) {
	app := tview.NewApplication()
	cols := []*Column{
		{Id: 0, Title: "Col0"},
		{Id: 1, Title: "Col1"},
	}

	table := NewTableContainer[mockEntry](
		app,
		func(row int, columns []*Column, entry *mockEntry) []*tview.TableCell {
			return []*tview.TableCell{tview.NewTableCell("cell")}
		},
		func(entries []*mockEntry, column *Column, inverted bool) []*mockEntry {
			return entries
		},
	)
	table.SetColumnSpec(cols, cols[0], false)

	assert.Equal(t, cols[0], table.sortByColumn)

	table.nextSortOrder()
	assert.Equal(t, cols[1], table.sortByColumn)

	table.nextSortOrder()
	assert.Equal(t, cols[0], table.sortByColumn)

	table.previousSortOrder()
	assert.Equal(t, cols[1], table.sortByColumn)

	assert.False(t, table.sortInverted)
	table.toggleSortDirection()
	assert.True(t, table.sortInverted)
}

func TestMultiSelection(t *testing.T) {
	app := tview.NewApplication()
	table := NewTableContainer[mockEntry](
		app,
		func(row int, columns []*Column, entry *mockEntry) []*tview.TableCell {
			return []*tview.TableCell{tview.NewTableCell("cell")}
		},
		func(entries []*mockEntry, column *Column, inverted bool) []*mockEntry {
			return entries
		},
	)

	e1 := &mockEntry{id: "1"}
	e2 := &mockEntry{id: "2"}

	assert.False(t, table.HasMultiSelection())

	table.addToMultiSelection(e1)
	assert.True(t, table.HasMultiSelection())
	assert.True(t, table.isInMultiSelection(e1))
	assert.False(t, table.isInMultiSelection(e2))

	table.addToMultiSelection(e2)
	assert.True(t, table.isInMultiSelection(e2))
	assert.Len(t, table.GetMultiSelection(), 2)

	table.toggleMultiSelection(e1)
	assert.False(t, table.isInMultiSelection(e1))
	assert.Len(t, table.GetMultiSelection(), 1)

	table.ClearMultiSelection()
	assert.False(t, table.HasMultiSelection())
	assert.Len(t, table.GetMultiSelection(), 0)
}

func TestCreateMultiSelectionEntryId(t *testing.T) {
	app := tview.NewApplication()
	table := NewTableContainer[mockEntry](
		app,
		func(row int, columns []*Column, entry *mockEntry) []*tview.TableCell { return nil },
		func(entries []*mockEntry, column *Column, inverted bool) []*mockEntry { return entries },
	)

	e1 := &mockEntry{id: "test-id"}
	assert.Equal(t, "test-id", table.createMultiSelectionEntryId(e1))
}

func newSortHighlightTestTable() (*RowSelectionTable[mockEntry], []*Column) {
	app := tview.NewApplication()
	cols := []*Column{
		{Id: 0, Title: "Col0"},
		{Id: 1, Title: "Col1"},
		{Id: 2, Title: "Col2"},
	}
	table := NewTableContainer[mockEntry](
		app,
		func(row int, columns []*Column, entry *mockEntry) []*tview.TableCell {
			var cells []*tview.TableCell
			for range columns {
				cells = append(cells, tview.NewTableCell("cell"))
			}
			return cells
		},
		func(entries []*mockEntry, column *Column, inverted bool) []*mockEntry {
			return entries
		},
	)
	table.SetColumnSpec(cols, cols[1], false)
	table.SetData([]*mockEntry{{id: "1"}, {id: "2"}})
	return table, cols
}

func TestSortColumnHeaderHasSelectedStyle(t *testing.T) {
	table, cols := newSortHighlightTestTable()

	sortColumnStyle := tcell.StyleDefault.
		Foreground(theme.Colors.Layout.Table.SortColumnSelectedForeground).
		Background(theme.Colors.Layout.Table.SortColumnSelectedBackground)

	assertHighlighted := func(sortColumnIndex int) {
		for column := range cols {
			cell := table.table.GetCell(0, column)
			if column == sortColumnIndex {
				assert.Equal(t, sortColumnStyle, cell.SelectedStyle, "column %d", column)
			} else {
				assert.Equal(t, tcell.StyleDefault, cell.SelectedStyle, "column %d", column)
			}
		}
		// data rows are not affected
		for column := range cols {
			assert.Equal(t, tcell.StyleDefault, table.table.GetCell(1, column).SelectedStyle)
		}
	}

	assertHighlighted(1)
	table.nextSortOrder()
	assertHighlighted(2)
	table.nextSortOrder()
	assertHighlighted(0)
	table.previousSortOrder()
	assertHighlighted(2)
	table.toggleSortDirection()
	assertHighlighted(2)
	table.SetData([]*mockEntry{{id: "3"}})
	assertHighlighted(2)
}

// findText returns the position of the first occurrence of text on the screen.
func findText(screen tcell.SimulationScreen, text string) (int, int, bool) {
	width, height := screen.Size()
	for y := 0; y < height; y++ {
		var line []rune
		for x := 0; x < width; x++ {
			primary, _, _, _ := screen.GetContent(x, y)
			line = append(line, primary)
		}
		if index := strings.Index(string(line), text); index >= 0 {
			return len([]rune(string(line)[:index])), y, true
		}
	}
	return 0, 0, false
}

func backgroundAt(screen tcell.SimulationScreen, x, y int) tcell.Color {
	_, _, style, _ := screen.GetContent(x, y)
	_, background, _ := style.Decompose()
	return background
}

func TestSortColumnHeaderHighlightRendering(t *testing.T) {
	table, _ := newSortHighlightTestTable()

	screen := tcell.NewSimulationScreen("UTF-8")
	assert.NoError(t, screen.Init())
	defer screen.Fini()
	screen.SetSize(60, 8)

	draw := func() {
		screen.Clear()
		table.layout.SetRect(0, 0, 60, 8)
		table.layout.Draw(screen)
	}

	// data row selected: no highlight in the header
	table.Select(table.GetEntries()[0])
	draw()
	x, y, found := findText(screen, "Col1")
	assert.True(t, found)
	assert.NotEqual(t, theme.Colors.Layout.Table.SortColumnSelectedBackground, backgroundAt(screen, x, y))

	// header row selected: the sort column is highlighted, other columns use the regular selection color
	table.SelectHeader()
	draw()
	x, y, found = findText(screen, "Col1")
	assert.True(t, found)
	assert.Equal(t, theme.Colors.Layout.Table.SortColumnSelectedBackground, backgroundAt(screen, x, y))
	x, y, found = findText(screen, "Col0")
	assert.True(t, found)
	assert.Equal(t, theme.Colors.Layout.Table.SelectedBackground, backgroundAt(screen, x, y))

	// the highlight follows the sort column
	table.nextSortOrder()
	draw()
	x, y, _ = findText(screen, "Col2")
	assert.Equal(t, theme.Colors.Layout.Table.SortColumnSelectedBackground, backgroundAt(screen, x, y))
	x, y, _ = findText(screen, "Col1")
	assert.Equal(t, theme.Colors.Layout.Table.SelectedBackground, backgroundAt(screen, x, y))
}

func isBoldAt(screen tcell.SimulationScreen, x, y int) bool {
	_, _, style, _ := screen.GetContent(x, y)
	_, _, attributes := style.Decompose()
	return attributes&tcell.AttrBold != 0
}

func TestHeaderIsBold(t *testing.T) {
	table, _ := newSortHighlightTestTable()

	screen := tcell.NewSimulationScreen("UTF-8")
	assert.NoError(t, screen.Init())
	defer screen.Fini()
	screen.SetSize(60, 8)

	draw := func() {
		screen.Clear()
		table.layout.SetRect(0, 0, 60, 8)
		table.layout.Draw(screen)
	}

	// data row selected
	table.Select(table.GetEntries()[0])
	draw()
	for _, title := range []string{"Col0", "Col1", "Col2"} {
		x, y, found := findText(screen, title)
		assert.True(t, found)
		assert.True(t, isBoldAt(screen, x, y), title)
	}
	x, y, found := findText(screen, "cell")
	assert.True(t, found)
	assert.False(t, isBoldAt(screen, x, y), "data cells are not bold")

	// header row selected: the selection styles keep the header bold
	table.SelectHeader()
	draw()
	for _, title := range []string{"Col0", "Col1", "Col2"} {
		x, y, _ := findText(screen, title)
		assert.True(t, isBoldAt(screen, x, y), title)
	}
}

func TestHeaderBackground(t *testing.T) {
	table, _ := newSortHighlightTestTable()
	headerBackground := theme.Colors.Layout.Table.HeaderBackground

	screen := tcell.NewSimulationScreen("UTF-8")
	assert.NoError(t, screen.Init())
	defer screen.Fini()
	screen.SetSize(60, 8)

	draw := func() {
		screen.Clear()
		table.layout.SetRect(0, 0, 60, 8)
		table.layout.Draw(screen)
	}

	table.Select(table.GetEntries()[0])
	draw()

	col0X, headerY, _ := findText(screen, "Col0")
	col2X, _, _ := findText(screen, "Col2")
	// the whole header from the first to the last title is one band, including the gaps between columns
	for x := col0X; x <= col2X; x++ {
		assert.Equal(t, headerBackground, backgroundAt(screen, x, headerY), "x=%d", x)
	}

	// data rows keep the default background
	cellX, cellY, _ := findText(screen, "cell")
	assert.NotEqual(t, headerBackground, backgroundAt(screen, cellX, cellY))

	// while selected, the selection colors replace the header background
	table.SelectHeader()
	draw()
	x, y, _ := findText(screen, "Col0")
	assert.Equal(t, theme.Colors.Layout.Table.SelectedBackground, backgroundAt(screen, x, y))
	x, y, _ = findText(screen, "Col1")
	assert.Equal(t, theme.Colors.Layout.Table.SortColumnSelectedBackground, backgroundAt(screen, x, y))
}
