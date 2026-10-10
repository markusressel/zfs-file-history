package table

import (
	"fmt"
	"strings"
	"testing"
	"time"
	"zfs-file-history/internal/ui/theme"
	uiutil "zfs-file-history/internal/ui/util"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
			// one rune per cell, so the index in line is the column
			str, _, _ := screen.Get(x, y)
			line = append(line, []rune(str)[0])
		}
		if index := strings.Index(string(line), text); index >= 0 {
			return len([]rune(string(line)[:index])), y, true
		}
	}
	return 0, 0, false
}

func backgroundAt(screen tcell.SimulationScreen, x, y int) tcell.Color {
	_, style, _ := screen.Get(x, y)
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
	_, style, _ := screen.Get(x, y)
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

type namedEntry struct {
	name string
}

func (e namedEntry) TableRowId() string {
	return e.name
}

func newFilterTestTable() (*RowSelectionTable[namedEntry], []*namedEntry, *[]tcell.Key, *int) {
	app := tview.NewApplication()
	cols := []*Column{{Id: 0, Title: "Name"}}
	table := NewTableContainer[namedEntry](
		app,
		func(row int, columns []*Column, entry *namedEntry) []*tview.TableCell {
			return []*tview.TableCell{tview.NewTableCell(entry.name)}
		},
		func(entries []*namedEntry, column *Column, inverted bool) []*namedEntry {
			return entries
		},
	)
	table.SetColumnSpec(cols, cols[0], false)
	table.SetTitle("Things")
	table.SetFilterFunc(func(entry *namedEntry, filterText string) bool {
		return strings.Contains(entry.name, filterText)
	})

	// records the keys that reach the owner's input capture
	var ownerKeys []tcell.Key
	table.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		ownerKeys = append(ownerKeys, event.Key())
		return nil
	})
	filterChangedCalls := 0
	table.SetFilterChangedCallback(func() { filterChangedCalls++ })

	entries := []*namedEntry{{name: "daily-1"}, {name: "weekly-1"}, {name: "daily-2"}, {name: "monthly-1"}}
	table.SetData(entries)
	return table, entries, &ownerKeys, &filterChangedCalls
}

// pressKey sends a key to the table, like the application would, and returns the event that is passed on.
func pressKey(table *RowSelectionTable[namedEntry], key tcell.Key, r rune) *tcell.EventKey {
	return table.table.GetInputCapture()(tcell.NewEventKey(key, r, tcell.ModNone))
}

func typeText(table *RowSelectionTable[namedEntry], text string) {
	for _, r := range text {
		pressKey(table, tcell.KeyRune, r)
	}
}

func entryNames(entries []*namedEntry) []string {
	var result []string
	for _, entry := range entries {
		result = append(result, entry.name)
	}
	return result
}

func TestFilter_SetFilterText(t *testing.T) {
	table, entries, _, filterChangedCalls := newFilterTestTable()

	assert.False(t, table.IsFilterActive())
	assert.Len(t, table.GetEntries(), 4)

	table.Select(entries[2]) // daily-2
	table.SetFilterText("daily")
	assert.True(t, table.IsFilterActive())
	assert.Equal(t, []string{"daily-1", "daily-2"}, entryNames(table.GetEntries()))
	assert.Len(t, table.GetAllEntries(), 4)
	assert.Equal(t, 1, *filterChangedCalls)
	// the selection is kept, if it matches
	assert.Same(t, entries[2], table.GetSelectedEntry())

	// otherwise the first match is selected
	table.SetFilterText("weekly")
	assert.Same(t, entries[1], table.GetSelectedEntry())

	// no match: nothing selected
	table.SetFilterText("nothing")
	assert.Empty(t, table.GetEntries())
	assert.Nil(t, table.GetSelectedEntry())

	// new data is filtered as well
	table.SetFilterText("monthly")
	table.SetData([]*namedEntry{{name: "monthly-1"}, {name: "monthly-2"}, {name: "daily-3"}})
	assert.Equal(t, []string{"monthly-1", "monthly-2"}, entryNames(table.GetEntries()))
	assert.Len(t, table.GetAllEntries(), 3)

	// clearing the filter shows everything again
	table.SetFilterText("")
	assert.False(t, table.IsFilterActive())
	assert.Len(t, table.GetEntries(), 3)
}

func TestFilter_HeaderRowStaysSelected(t *testing.T) {
	table, _, _, _ := newFilterTestTable()
	table.SelectHeader()

	table.SetFilterText("daily")
	assert.Nil(t, table.GetSelectedEntry())
	row, _ := table.table.GetSelection()
	assert.Equal(t, 0, row)
}

func TestFilter_HidesEntriesFromMultiSelection(t *testing.T) {
	table, entries, _, _ := newFilterTestTable()
	table.SetMultiSelect(true)
	table.addToMultiSelection(entries[0]) // daily-1
	table.addToMultiSelection(entries[1]) // weekly-1

	// hidden entries must not stay selected, actions on the multi-selection would include invisible entries
	table.SetFilterText("daily")
	assert.Equal(t, []string{"daily-1"}, entryNames(table.GetMultiSelection()))
}

func TestFilter_SlashStartsTyping(t *testing.T) {
	table, _, ownerKeys, _ := newFilterTestTable()
	table.SelectFirstIfExists()

	assert.Nil(t, pressKey(table, tcell.KeyRune, '/'))
	assert.True(t, table.IsEditingFilter())
	assert.Empty(t, table.GetFilterText(), "the '/' starts typing, it is not typed")

	// while typing, '/' is part of the filter, e.g. of a path
	typeText(table, "a/b")
	assert.Equal(t, "a/b", table.GetFilterText())
	assert.Empty(t, *ownerKeys)
}

func TestFilter_Typing(t *testing.T) {
	table, _, ownerKeys, _ := newFilterTestTable()
	table.SelectFirstIfExists()

	// ctrl+f starts typing
	assert.Nil(t, pressKey(table, tcell.KeyCtrlF, 0))
	assert.True(t, table.IsEditingFilter())
	assert.True(t, uiutil.IsTextInputActive(table.table))
	assert.Equal(t, filterFooter{text: "Filter:", cursor: 10}, drawFilterFooter(t, table))
	// the title is not affected
	assert.Equal(t, " Things ", table.layout.GetTitle())

	// typed keys filter, and never reach the owner (e.g. 'd' could be a delete shortcut)
	typeText(table, "dai")
	assert.Equal(t, "dai", table.GetFilterText())
	assert.Equal(t, []string{"daily-1", "daily-2"}, entryNames(table.GetEntries()))
	assert.Equal(t, filterFooter{text: "Filter: dai", cursor: 13}, drawFilterFooter(t, table))

	// backspace removes the last character
	pressKey(table, tcell.KeyBackspace2, 0)
	assert.Equal(t, "da", table.GetFilterText())

	// the cursor can be moved, typing inserts at the cursor and filters immediately
	assert.Nil(t, pressKey(table, tcell.KeyHome, 0)) // edits, does not navigate the list
	pressKey(table, tcell.KeyRune, 'x')
	assert.Equal(t, "xda", table.GetFilterText())
	assert.Empty(t, table.GetEntries())
	assert.Equal(t, filterFooter{text: "Filter: xda", cursor: 11}, drawFilterFooter(t, table))
	pressKey(table, tcell.KeyLeft, 0)
	pressKey(table, tcell.KeyDelete, 0)
	assert.Equal(t, "da", table.GetFilterText())
	assert.Equal(t, []string{"daily-1", "daily-2"}, entryNames(table.GetEntries()))

	// navigation still works, but skips the owner
	assert.NotNil(t, pressKey(table, tcell.KeyDown, 0))
	// other keys are consumed
	assert.Nil(t, pressKey(table, tcell.KeyF2, 0))
	assert.Empty(t, *ownerKeys)

	// enter keeps the filter and stops typing
	assert.Nil(t, pressKey(table, tcell.KeyEnter, 0))
	assert.False(t, table.IsEditingFilter())
	assert.False(t, uiutil.IsTextInputActive(table.table))
	assert.Equal(t, "da", table.GetFilterText())
	assert.Equal(t, filterFooter{text: "Filter: da", cursor: -1}, drawFilterFooter(t, table))

	// now keys reach the owner again
	pressKey(table, tcell.KeyRune, 'd')
	assert.Equal(t, []tcell.Key{tcell.KeyRune}, *ownerKeys)

	// esc clears an active filter, even when not typing
	assert.Nil(t, pressKey(table, tcell.KeyEscape, 0))
	assert.False(t, table.IsFilterActive())
	assert.Equal(t, filterFooter{text: "", cursor: -1}, drawFilterFooter(t, table))
	assert.Len(t, table.GetEntries(), 4)
}

func TestFilter_TypingStopsWithEscBackspaceAndBlur(t *testing.T) {
	table, _, _, _ := newFilterTestTable()

	// esc while typing clears the filter and stops typing
	pressKey(table, tcell.KeyCtrlF, 0)
	typeText(table, "week")
	pressKey(table, tcell.KeyEscape, 0)
	assert.False(t, table.IsEditingFilter())
	assert.False(t, table.IsFilterActive())

	// backspace on an empty filter stops typing
	pressKey(table, tcell.KeyCtrlF, 0)
	pressKey(table, tcell.KeyBackspace2, 0)
	assert.False(t, table.IsEditingFilter())

	// backspace with the cursor at the start of a non-empty filter does nothing
	pressKey(table, tcell.KeyCtrlF, 0)
	typeText(table, "da")
	pressKey(table, tcell.KeyHome, 0)
	pressKey(table, tcell.KeyBackspace2, 0)
	assert.True(t, table.IsEditingFilter())
	assert.Equal(t, "da", table.GetFilterText())
	pressKey(table, tcell.KeyEscape, 0)

	// losing focus stops typing, but keeps the filter
	pressKey(table, tcell.KeyCtrlF, 0)
	typeText(table, "month")
	table.table.Blur()
	assert.False(t, table.IsEditingFilter())
	assert.False(t, uiutil.IsTextInputActive(table.table))
	assert.Equal(t, "month", table.GetFilterText())
}

func TestFilter_DisabledWithoutFilterFunc(t *testing.T) {
	table, _, ownerKeys, _ := newFilterTestTable()
	table.SetFilterFunc(nil)

	pressKey(table, tcell.KeyCtrlF, 0)
	assert.False(t, table.IsEditingFilter())
	assert.Equal(t, []tcell.Key{tcell.KeyCtrlF}, *ownerKeys)
}

type filterFooter struct {
	// text is the visible text in the bottom border, without the border characters
	text string
	// cursor is the screen column of the cell in reverse video, -1 if none
	cursor int
}

// drawFilterFooter draws the table window and returns the left part of its bottom border.
func drawFilterFooter(t *testing.T, table *RowSelectionTable[namedEntry]) filterFooter {
	screen := tcell.NewSimulationScreen("UTF-8")
	require.NoError(t, screen.Init())
	defer screen.Fini()
	screen.SetSize(40, 8)
	table.layout.SetRect(0, 0, 40, 8)
	table.layout.Draw(screen)

	result := filterFooter{cursor: -1}
	var line strings.Builder
	for x := 0; x < 40; x++ {
		str, style, _ := screen.Get(x, 7)
		line.WriteString(str)
		_, _, attributes := style.Decompose()
		if attributes&tcell.AttrReverse != 0 {
			result.cursor = x
		}
	}
	result.text = strings.TrimSpace(strings.Trim(line.String(), "└┘─╚╝═"))
	return result
}

func TestFilter_FooterAndCountsDoNotOverlap(t *testing.T) {
	table, _, _, _ := newFilterTestTable()
	table.SetFooter("2 of 4 things")
	table.SetFilterText("daily")

	screen := tcell.NewSimulationScreen("UTF-8")
	require.NoError(t, screen.Init())
	defer screen.Fini()
	screen.SetSize(40, 8)
	table.layout.SetRect(0, 0, 40, 8)
	table.layout.Draw(screen)

	var line strings.Builder
	for x := 0; x < 40; x++ {
		str, _, _ := screen.Get(x, 7)
		line.WriteString(str)
	}
	assert.Equal(t, "└ Filter: daily ──────── 2 of 4 things ┘", line.String())
}

// newWideTestTable returns a table with the given number of columns (20 characters each, so 21 with the space
// between them) in a 40 characters wide view, and 50 rows.
func newWideTestTable(columnCount int) (*RowSelectionTable[namedEntry], []*namedEntry, []*Column) {
	var cols []*Column
	for i := 0; i < columnCount; i++ {
		cols = append(cols, &Column{Id: ColumnId(i), Title: fmt.Sprintf("col%d", i)})
	}
	table := NewTableContainer[namedEntry](
		tview.NewApplication(),
		func(row int, columns []*Column, entry *namedEntry) []*tview.TableCell {
			var cells []*tview.TableCell
			for _, column := range columns {
				cells = append(cells, tview.NewTableCell(fmt.Sprintf("%-20s", entry.name+"/"+column.Title)))
			}
			return cells
		},
		func(entries []*namedEntry, column *Column, inverted bool) []*namedEntry {
			return entries
		},
	)
	table.SetColumnSpec(cols, cols[0], false)
	var entries []*namedEntry
	for i := 0; i < 50; i++ {
		entries = append(entries, &namedEntry{name: fmt.Sprintf("entry-%02d", i)})
	}
	table.SetData(entries)
	table.table.SetRect(0, 0, 40, 10)
	return table, entries, cols
}

// pressTableKey sends a key through the input capture of the component and then to tview, like the application.
func pressTableKey(table *RowSelectionTable[namedEntry], key tcell.Key) {
	event := table.table.GetInputCapture()(tcell.NewEventKey(key, 0, tcell.ModNone))
	if event != nil {
		table.table.InputHandler()(event, func(p tview.Primitive) {})
	}
}

func headerTexts(table *RowSelectionTable[namedEntry]) []string {
	var texts []string
	for column := 0; column < table.table.GetColumnCount(); column++ {
		texts = append(texts, table.table.GetCell(0, column).Text)
	}
	return texts
}

func TestTable_HorizontalScroll(t *testing.T) {
	table, entries, cols := newWideTestTable(4)
	table.Select(entries[0])

	// → hides the first column, while columns are cut off on the right (4 columns: 83 characters in 40)
	pressTableKey(table, tcell.KeyRight)
	assert.Equal(t, 1, table.columnOffset)
	assert.Equal(t, []string{"◂ col1", "col2", "col3"}, headerTexts(table), "the indicator shows hidden columns")
	assert.Equal(t, "entry-00/col1", strings.TrimSpace(table.table.GetCell(1, 0).Text))
	pressTableKey(table, tcell.KeyRight)
	assert.Equal(t, 2, table.columnOffset, "col2 and col3 fit now (41 characters in 40 do not)")
	pressTableKey(table, tcell.KeyRight)
	assert.Equal(t, 3, table.columnOffset)
	pressTableKey(table, tcell.KeyRight)
	assert.Equal(t, 3, table.columnOffset, "nothing is cut off anymore, and at least one column is left")
	_, tviewOffset := table.table.GetOffset()
	assert.Zero(t, tviewOffset, "tview's own offset is not used")

	// ← scrolls back
	pressTableKey(table, tcell.KeyLeft)
	assert.Equal(t, 2, table.columnOffset)
	assert.Equal(t, cols[2], table.visibleColumns()[0])
}

func TestTable_HorizontalScrollOnlyIfCutOff(t *testing.T) {
	table, entries, _ := newWideTestTable(1)
	table.Select(entries[0])

	pressTableKey(table, tcell.KeyRight)
	assert.Zero(t, table.columnOffset)
}

func TestTable_HorizontalScrollIsKept(t *testing.T) {
	table, entries, cols := newWideTestTable(4)
	table.Select(entries[0])
	pressTableKey(table, tcell.KeyRight)
	pressTableKey(table, tcell.KeyRight)
	require.Equal(t, 2, table.columnOffset)

	// changing the selection, reloading and selecting the first entry again keep it
	pressTableKey(table, tcell.KeyDown)
	table.SetData(entries)
	table.Select(entries[0])
	table.UpdateEntry(entries[0])
	assert.Equal(t, 2, table.columnOffset)
	assert.Equal(t, "entry-00/col2", strings.TrimSpace(table.table.GetCell(1, 0).Text))

	// on the header row, ← and → change the sort column instead of scrolling
	pressTableKey(table, tcell.KeyUp)
	require.Nil(t, table.GetSelectedEntry())
	pressTableKey(table, tcell.KeyRight)
	assert.Equal(t, 2, table.columnOffset)
	assert.Equal(t, cols[1], table.sortByColumn)

	// a page jump to the header row still scrolls to the top row (PgUp workaround), the columns stay scrolled
	table.Select(entries[20])
	table.table.SetOffset(15, 0)
	table.SelectHeader()
	row, _ := table.table.GetOffset()
	assert.Equal(t, 0, row)
	assert.Equal(t, 2, table.columnOffset)

	// fewer columns (F2): at least one column is left
	table.SetActiveColumns(cols[:2])
	assert.Equal(t, 1, table.columnOffset)
	assert.Equal(t, []*Column{cols[1]}, table.visibleColumns())
}

func TestTable_EmbedInFrame(t *testing.T) {
	table, _, _, _ := newFilterTestTable()
	frame := tview.NewFlex()
	frame.SetBorder(true)
	frame.AddItem(table.GetLayout(), 0, 1, true)

	table.EmbedInFrame(frame.Box)
	table.SetFooter("4 things")
	table.SetFilterText("daily")

	screen := tcell.NewSimulationScreen("UTF-8")
	require.NoError(t, screen.Init())
	screen.SetSize(40, 8)
	frame.SetRect(0, 0, 40, 8)
	frame.Draw(screen)
	screen.Show()

	cells, width, height := screen.GetContents()
	line := func(y int) string {
		var text strings.Builder
		for x := 0; x < width; x++ {
			if runes := cells[y*width+x].Runes; len(runes) > 0 {
				text.WriteRune(runes[0])
			}
		}
		return text.String()
	}

	bottom := line(height - 1)
	assert.Contains(t, bottom, "4 things", "the footer is drawn into the frame")
	assert.Contains(t, bottom, "Filter: daily")
	assert.Equal(t, "4 things", table.GetFooter())
	// the content directly above the frame's border is not overwritten by the table's former footer
	assert.NotContains(t, line(height-2), "4 things")
	// with a border of its own, line 1 would be the table's border
	assert.Contains(t, line(1), "Name", "the header row starts right below the frame's top border")
}

// drawTableLayout draws the whole table component (table and scrollbar), like the application does after each key.
func drawTableLayout(table *RowSelectionTable[namedEntry], screen tcell.Screen) {
	table.GetLayout().Draw(screen)
}

func TestTable_ScrollbarReachesTheEndWithPgDn(t *testing.T) {
	table, entries, _ := newWideTestTable(1)
	screen := tcell.NewSimulationScreen("UTF-8")
	require.NoError(t, screen.Init())
	screen.SetSize(40, 12)
	table.GetLayout().SetRect(0, 0, 40, 12)
	// like in the application: tview's Flex draws the focused item (the table) last, after the scrollbar
	table.table.Focus(func(tview.Primitive) {})
	table.Select(entries[0])
	drawTableLayout(table, screen)
	require.False(t, table.scrollbar.IsAtEnd(), "50 rows do not fit")

	for i := 0; i < 20 && table.GetSelectedEntry() != entries[len(entries)-1]; i++ {
		pressTableKey(table, tcell.KeyPgDn)
		drawTableLayout(table, screen)
	}
	require.Equal(t, entries[len(entries)-1], table.GetSelectedEntry(), "the last row is selected")

	rowOffset, _ := table.table.GetOffset()
	assert.Equal(t, rowOffset, table.scrollbar.GetPosition(), "the scrollbar shows the offset of the table as drawn")
	assert.True(t, table.scrollbar.IsAtEnd(), "the bar reaches the bottom")

	// and back to the top
	for i := 0; i < 20 && table.GetSelectedEntry() != entries[0]; i++ {
		pressTableKey(table, tcell.KeyPgUp)
		drawTableLayout(table, screen)
	}
	assert.Zero(t, table.scrollbar.GetPosition())
}

func TestTable_ScrollbarVisibility(t *testing.T) {
	screen := tcell.NewSimulationScreen("UTF-8")
	require.NoError(t, screen.Init())
	screen.SetSize(40, 12)

	// 12 lines: the border (2) and the header row (1) leave 9 lines for data rows
	tests := []struct {
		rows    int
		visible bool
	}{
		{9, false},
		{10, true}, // one row does not fit
	}
	for _, test := range tests {
		t.Run(fmt.Sprintf("%d rows", test.rows), func(t *testing.T) {
			table, entries, _ := newWideTestTable(1)
			table.SetData(entries[:test.rows])
			table.GetLayout().SetRect(0, 0, 40, 12)
			drawTableLayout(table, screen)
			table.syncScrollbar()
			assert.Equal(t, test.visible, table.isScrollbarVisible)
		})
	}
}

func TestTable_RendersAgainWhenTheTimeFormatChanged(t *testing.T) {
	then := time.Now().Add(-2 * time.Hour)
	table := NewTableContainer[namedEntry](
		tview.NewApplication(),
		func(row int, columns []*Column, entry *namedEntry) []*tview.TableCell {
			return []*tview.TableCell{tview.NewTableCell(uiutil.FormatTime(then))}
		},
		func(entries []*namedEntry, column *Column, inverted bool) []*namedEntry { return entries },
	)
	cols := []*Column{{Id: 0, Title: "Time"}}
	table.SetColumnSpec(cols, cols[0], false)
	table.SetData([]*namedEntry{{name: "a"}})
	screen := tcell.NewSimulationScreen("UTF-8")
	require.NoError(t, screen.Init())
	screen.SetSize(40, 6)
	table.GetLayout().SetRect(0, 0, 40, 6)
	t.Cleanup(func() {
		if uiutil.IsRelativeTimes() {
			uiutil.ToggleRelativeTimes()
		}
	})

	before := table.table.GetCell(1, 0).Text
	uiutil.ToggleRelativeTimes()
	assert.Equal(t, before, table.table.GetCell(1, 0).Text, "only rendered again when drawn")

	table.GetLayout().Draw(screen)
	assert.Equal(t, "2 hours ago", table.table.GetCell(1, 0).Text)
}

// tview's table follows the end of the rows once all of them fit (e.g. while it is empty, before new data is
// loaded), so the next data would be shown scrolled to the end, also hiding the selected row
func TestTable_NewDataIsNotScrolledToTheEnd(t *testing.T) {
	table, entries, _ := newWideTestTable(1)
	screen := tcell.NewSimulationScreen("UTF-8")
	require.NoError(t, screen.Init())
	screen.SetSize(40, 12)
	table.GetLayout().SetRect(0, 0, 40, 12)
	table.table.Focus(func(tview.Primitive) {})

	// e.g. while the snapshots of another dataset are loaded
	table.SetData([]*namedEntry{})
	drawTableLayout(table, screen)

	table.SetData(entries)
	table.Select(entries[2])
	drawTableLayout(table, screen)

	rowOffset, _ := table.table.GetOffset()
	assert.Zero(t, rowOffset, "the first rows are shown")
	assert.Equal(t, entries[2], table.GetSelectedEntry())

	// also without selecting again, e.g. if the same row is selected as before
	table.SetData([]*namedEntry{})
	drawTableLayout(table, screen)
	table.SetData(entries)
	drawTableLayout(table, screen)

	rowOffset, _ = table.table.GetOffset()
	assert.Zero(t, rowOffset, "the first rows are shown")
}

func TestTable_Placeholder(t *testing.T) {
	table, entries, _ := newWideTestTable(1)
	screen := tcell.NewSimulationScreen("UTF-8")
	require.NoError(t, screen.Init())
	screen.SetSize(40, 12)
	table.GetLayout().SetRect(0, 0, 40, 12)
	screenText := func() string {
		screen.Clear()
		drawTableLayout(table, screen)
		screen.Show()
		cells, width, _ := screen.GetContents()
		var text strings.Builder
		for i, cell := range cells {
			if i > 0 && i%width == 0 {
				text.WriteRune('\n')
			}
			if len(cell.Runes) > 0 {
				text.WriteRune(cell.Runes[0])
			}
		}
		return text.String()
	}

	table.SetPlaceholder("open /x: permission denied", tcell.ColorRed)
	table.SetData([]*namedEntry{})
	assert.Contains(t, screenText(), "open /x: permission denied", "shown while there are no rows")

	table.SetData(entries)
	assert.NotContains(t, screenText(), "permission denied", "not shown over rows")
}
