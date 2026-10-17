package table

import (
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"
	"zfs-file-history/internal/testutil"
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
	app, _ := testutil.NewApp()
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
	app, _ := testutil.NewApp()
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
	app, _ := testutil.NewApp()
	table := NewTableContainer[mockEntry](
		app,
		func(row int, columns []*Column, entry *mockEntry) []*tview.TableCell { return nil },
		func(entries []*mockEntry, column *Column, inverted bool) []*mockEntry { return entries },
	)

	e1 := &mockEntry{id: "test-id"}
	assert.Equal(t, "test-id", table.createMultiSelectionEntryId(e1))
}

func newSortHighlightTestTable() (*RowSelectionTable[mockEntry], []*Column) {
	app, _ := testutil.NewApp()
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
			assert.NotEqual(t, sortColumnStyle, table.table.GetCell(1, column).SelectedStyle)
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

func backgroundAt(screen tcell.SimulationScreen, x, y int) tcell.Color {
	_, style, _ := screen.Get(x, y)
	_, background, _ := style.Decompose()
	return background
}

func TestSortColumnHeaderHighlightRendering(t *testing.T) {
	table, _ := newSortHighlightTestTable()

	screen := testutil.NewScreen(t, 60, 8)

	draw := func() {
		screen.Clear()
		table.layout.SetRect(0, 0, 60, 8)
		table.layout.Draw(screen)
	}

	// data row selected: no highlight in the header
	table.Select(table.GetEntries()[0])
	draw()
	x, y, found := testutil.FindInScreen(screen, "Col1")
	assert.True(t, found)
	assert.NotEqual(t, theme.Colors.Layout.Table.SortColumnSelectedBackground, backgroundAt(screen, x, y))

	// header row selected: the sort column is highlighted, other columns use the regular selection color
	table.SelectHeader()
	draw()
	x, y, found = testutil.FindInScreen(screen, "Col1")
	assert.True(t, found)
	assert.Equal(t, theme.Colors.Layout.Table.SortColumnSelectedBackground, backgroundAt(screen, x, y))
	x, y, found = testutil.FindInScreen(screen, "Col0")
	assert.True(t, found)
	assert.Equal(t, theme.Colors.Layout.Table.SelectedBackground, backgroundAt(screen, x, y))

	// the highlight follows the sort column
	table.nextSortOrder()
	draw()
	x, y, _ = testutil.FindInScreen(screen, "Col2")
	assert.Equal(t, theme.Colors.Layout.Table.SortColumnSelectedBackground, backgroundAt(screen, x, y))
	x, y, _ = testutil.FindInScreen(screen, "Col1")
	assert.Equal(t, theme.Colors.Layout.Table.SelectedBackground, backgroundAt(screen, x, y))
}

func isBoldAt(screen tcell.SimulationScreen, x, y int) bool {
	_, style, _ := screen.Get(x, y)
	_, _, attributes := style.Decompose()
	return attributes&tcell.AttrBold != 0
}

func TestHeaderIsBold(t *testing.T) {
	table, _ := newSortHighlightTestTable()

	screen := testutil.NewScreen(t, 60, 8)

	draw := func() {
		screen.Clear()
		table.layout.SetRect(0, 0, 60, 8)
		table.layout.Draw(screen)
	}

	// data row selected
	table.Select(table.GetEntries()[0])
	draw()
	for _, title := range []string{"Col0", "Col1", "Col2"} {
		x, y, found := testutil.FindInScreen(screen, title)
		assert.True(t, found)
		assert.True(t, isBoldAt(screen, x, y), title)
	}
	x, y, found := testutil.FindInScreen(screen, "cell")
	assert.True(t, found)
	assert.False(t, isBoldAt(screen, x, y), "data cells are not bold")

	// header row selected: the selection styles keep the header bold
	table.SelectHeader()
	draw()
	for _, title := range []string{"Col0", "Col1", "Col2"} {
		x, y, _ := testutil.FindInScreen(screen, title)
		assert.True(t, isBoldAt(screen, x, y), title)
	}
}

func TestHeaderBackground(t *testing.T) {
	table, _ := newSortHighlightTestTable()
	headerBackground := theme.Colors.Layout.Table.HeaderBackground

	screen := testutil.NewScreen(t, 60, 8)

	draw := func() {
		screen.Clear()
		table.layout.SetRect(0, 0, 60, 8)
		table.layout.Draw(screen)
	}

	table.Select(table.GetEntries()[0])
	draw()

	col0X, headerY, _ := testutil.FindInScreen(screen, "Col0")
	col2X, _, _ := testutil.FindInScreen(screen, "Col2")
	// the whole header from the first to the last title is one band, including the gaps between columns
	for x := col0X; x <= col2X; x++ {
		assert.Equal(t, headerBackground, backgroundAt(screen, x, headerY), "x=%d", x)
	}

	// data rows keep the default background
	cellX, cellY, _ := testutil.FindInScreen(screen, "cell")
	assert.NotEqual(t, headerBackground, backgroundAt(screen, cellX, cellY))

	// while selected, the selection colors replace the header background
	table.SelectHeader()
	draw()
	x, y, _ := testutil.FindInScreen(screen, "Col0")
	assert.Equal(t, theme.Colors.Layout.Table.SelectedBackground, backgroundAt(screen, x, y))
	x, y, _ = testutil.FindInScreen(screen, "Col1")
	assert.Equal(t, theme.Colors.Layout.Table.SortColumnSelectedBackground, backgroundAt(screen, x, y))
}

type namedEntry struct {
	name string
}

func (e namedEntry) TableRowId() string {
	return e.name
}

func newFilterTestTable() (*RowSelectionTable[namedEntry], []*namedEntry, *[]tcell.Key, *int) {
	app, _ := testutil.NewApp()
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
	screen := testutil.NewScreen(t, 40, 8)
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

	screen := testutil.NewScreen(t, 40, 8)
	table.layout.SetRect(0, 0, 40, 8)
	table.layout.Draw(screen)

	assert.Equal(t, "└ Filter: daily ──────── 2 of 4 things ┘", testutil.ScreenLines(screen)[7])
}

// newWideTestTable returns a table with the given number of columns (20 characters each, so 21 with the space
// between them) in a 40 characters wide view, and 50 rows.
func newWideTestTable(columnCount int) (*RowSelectionTable[namedEntry], []*namedEntry, []*Column) {
	var cols []*Column
	for i := 0; i < columnCount; i++ {
		cols = append(cols, &Column{Id: ColumnId(i), Title: fmt.Sprintf("col%d", i)})
	}
	table := NewTableContainer[namedEntry](
		testutil.App(),
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

	screen := testutil.NewScreen(t, 40, 8)
	frame.SetRect(0, 0, 40, 8)
	frame.Draw(screen)

	lines := testutil.ScreenLines(screen)
	line := func(y int) string { return lines[y] }
	height := len(lines)

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
	screen := testutil.NewScreen(t, 40, 12)
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
	screen := testutil.NewScreen(t, 40, 12)

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
		testutil.App(),
		func(row int, columns []*Column, entry *namedEntry) []*tview.TableCell {
			return []*tview.TableCell{tview.NewTableCell(uiutil.FormatTime(then))}
		},
		func(entries []*namedEntry, column *Column, inverted bool) []*namedEntry { return entries },
	)
	cols := []*Column{{Id: 0, Title: "Time"}}
	table.SetColumnSpec(cols, cols[0], false)
	table.SetData([]*namedEntry{{name: "a"}})
	screen := testutil.NewScreen(t, 40, 6)
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
	screen := testutil.NewScreen(t, 40, 12)
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

// newTextDrawer returns a function that draws the table (its layout at 40x12) and returns the text on the screen, a
// line per row.
func newTextDrawer(t *testing.T, table *RowSelectionTable[namedEntry]) func() string {
	screen := testutil.NewScreen(t, 40, 12)
	table.GetLayout().SetRect(0, 0, 40, 12)
	return func() string {
		screen.Clear()
		drawTableLayout(table, screen)
		return strings.Join(testutil.ScreenLines(screen), "\n")
	}
}

func TestTable_Placeholder(t *testing.T) {
	table, entries, _ := newWideTestTable(1)
	screenText := newTextDrawer(t, table)

	table.SetPlaceholder("open /x: permission denied", tcell.ColorRed)
	table.SetData([]*namedEntry{})
	assert.Contains(t, screenText(), "open /x: permission denied", "shown while there are no rows")

	table.SetData(entries)
	assert.NotContains(t, screenText(), "permission denied", "not shown over rows")
}

func TestTable_NoMatchesForTheFilter(t *testing.T) {
	table, _, _, _ := newFilterTestTable()
	screenText := newTextDrawer(t, table)

	table.SetFilterText("nothing")
	text := screenText()
	assert.Contains(t, text, "No matches for nothing")
	lines := strings.Split(text, "\n")
	row := slices.IndexFunc(lines, func(line string) bool { return strings.Contains(line, "No matches") })
	// centered in the 9 rows below the border and the header (12 lines, without the bottom border)
	assert.Equal(t, 2+4, row, "centered vertically:\n%s", text)

	table.SetFilterText("daily")
	assert.NotContains(t, screenText(), "No matches", "not shown while entries match")

	// an empty table is not empty because of the filter
	table.SetFilterText("nothing")
	table.SetData([]*namedEntry{})
	assert.NotContains(t, screenText(), "No matches")

	// the placeholder (e.g. why the entries could not be loaded) says more
	table.SetPlaceholder("open /x: permission denied", tcell.ColorRed)
	assert.Contains(t, screenText(), "permission denied")
}

// values computed in the background (e.g. the diff states of files) are sorted once Resort is called, keeping the
// selection
func TestTable_Resort(t *testing.T) {
	cols := []*Column{{Id: 0, Title: "Name"}}
	table := NewTableContainer[namedEntry](
		testutil.App(),
		func(row int, columns []*Column, entry *namedEntry) []*tview.TableCell {
			return []*tview.TableCell{tview.NewTableCell(entry.name)}
		},
		func(entries []*namedEntry, column *Column, inverted bool) []*namedEntry {
			slices.SortStableFunc(entries, func(a, b *namedEntry) int { return strings.Compare(a.name, b.name) })
			return entries
		},
	)
	table.SetColumnSpec(cols, cols[0], false)
	selectionChanges := 0
	table.SetSelectionChangedCallback(func(*namedEntry) { selectionChanges++ })

	a, b, c := &namedEntry{name: "a"}, &namedEntry{name: "b"}, &namedEntry{name: "c"}
	table.SetData([]*namedEntry{c, a, b})
	table.Select(b)
	selectionChanges = 0
	assert.True(t, table.IsSortedBy(cols[0]))

	a.name = "z"
	table.UpdateEntry(a)
	assert.Equal(t, []*namedEntry{a, b, c}, table.GetEntries(), "not sorted by UpdateEntry")

	table.Resort()
	assert.Equal(t, []*namedEntry{b, c, a}, table.GetEntries())
	assert.Equal(t, b, table.GetSelectedEntry(), "the selection is kept")
	assert.Zero(t, selectionChanges, "the selection did not change")
}

// Regression: a click into the empty space below the rows selected the header row.
func TestTable_ClickBelowTheRowsKeepsTheSelection(t *testing.T) {
	table, entries, _, _ := newFilterTestTable()
	table.table.SetRect(0, 0, 40, 20)
	table.Select(entries[1])

	click := tcell.NewEventMouse(5, 15, tcell.ButtonNone, tcell.ModNone)
	table.table.MouseHandler()(tview.MouseLeftClick, click, func(tview.Primitive) {})

	assert.Equal(t, entries[1], table.GetSelectedEntry())
}

// pressAndClick presses and releases the mouse button at the position, like the application does for a click.
func pressAndClick(table *RowSelectionTable[mockEntry], x int, y int) {
	handler := table.table.MouseHandler()
	focus := func(p tview.Primitive) { p.Focus(nil) }
	handler(tview.MouseLeftDown, tcell.NewEventMouse(x, y, tcell.Button1, tcell.ModNone), focus)
	handler(tview.MouseLeftUp, tcell.NewEventMouse(x, y, tcell.ButtonNone, tcell.ModNone), focus)
	handler(tview.MouseLeftClick, tcell.NewEventMouse(x, y, tcell.ButtonNone, tcell.ModNone), focus)
}

// Clicking the header: the first click selects it, the next ones sort by the clicked column, or toggle the
// direction of the sort column.
func TestTable_HeaderClicks(t *testing.T) {
	table, cols := newSortHighlightTestTable()
	var layoutChanges int
	table.SetColumnLayoutChangedCallback(func(ColumnLayout) { layoutChanges++ })
	screen := testutil.NewScreen(t, 40, 10)
	table.table.SetRect(0, 0, 40, 10)
	table.table.Draw(screen)
	col0X, headerY, found := testutil.FindInScreen(screen, "Col0")
	require.True(t, found)
	table.Select(table.GetEntries()[0])
	require.NotNil(t, table.GetSelectedEntry())

	// not focused, a row selected: selects the header, without sorting
	pressAndClick(table, col0X, headerY)
	assert.Nil(t, table.GetSelectedEntry(), "the header is selected")
	assert.True(t, table.IsSortedBy(cols[1]))
	assert.Zero(t, layoutChanges)

	// again: sorts by the clicked column, in the same direction
	pressAndClick(table, col0X, headerY)
	assert.True(t, table.IsSortedBy(cols[0]))
	assert.False(t, table.sortInverted)
	assert.Nil(t, table.GetSelectedEntry(), "the header stays selected")
	assert.Equal(t, 1, layoutChanges, "saved")

	// the sort column again: the direction is toggled
	pressAndClick(table, col0X, headerY)
	assert.True(t, table.IsSortedBy(cols[0]))
	assert.True(t, table.sortInverted)
	// the sort indicator moved the column
	table.table.Draw(screen)
	col1X, _, _ := testutil.FindInScreen(screen, "Col1")
	pressAndClick(table, col1X, headerY)
	assert.True(t, table.IsSortedBy(cols[1]))
	assert.True(t, table.sortInverted, "the direction is kept")

	// next to the columns: nothing
	table.table.Draw(screen)
	pressAndClick(table, 38, headerY)
	assert.True(t, table.IsSortedBy(cols[1]))
	assert.True(t, table.sortInverted)
	assert.Nil(t, table.GetSelectedEntry())

	// a row: selected
	pressAndClick(table, col0X, headerY+1)
	assert.NotNil(t, table.GetSelectedEntry())
}

// The header is selected, but another pane is focused: the click focuses the table, without sorting.
func TestTable_HeaderClickWhileNotFocused(t *testing.T) {
	table, cols := newSortHighlightTestTable()
	screen := testutil.NewScreen(t, 40, 10)
	table.table.SetRect(0, 0, 40, 10)
	table.table.Draw(screen)
	col0X, headerY, _ := testutil.FindInScreen(screen, "Col0")
	table.SelectHeader()
	table.table.Blur()

	pressAndClick(table, col0X, headerY)
	assert.True(t, table.IsSortedBy(cols[1]))
	assert.True(t, table.table.HasFocus())
}

// Regression: a click into the empty space to the right of a row selected it.
func TestTable_ClickBesideTheColumnsKeepsTheSelection(t *testing.T) {
	table, entries, _, _ := newFilterTestTable()
	screen := testutil.NewScreen(t, 40, 10)
	table.table.SetRect(0, 0, 40, 10)
	table.table.Draw(screen)
	table.Select(entries[1])

	click := func(x int, y int) {
		event := tcell.NewEventMouse(x, y, tcell.ButtonNone, tcell.ModNone)
		table.table.MouseHandler()(tview.MouseLeftClick, event, func(tview.Primitive) {})
	}
	// the names are at most 9 characters wide
	click(30, 3)
	assert.Equal(t, entries[1], table.GetSelectedEntry())
	click(30, 0)
	assert.Equal(t, entries[1], table.GetSelectedEntry(), "the header is not selected either")

	// on the name: selected
	click(2, 3)
	assert.Equal(t, table.GetEntries()[2], table.GetSelectedEntry())
}

func TestTable_ContentSize(t *testing.T) {
	table, entries, _ := newWideTestTable(4)
	table.Select(entries[0])

	// 4 columns of 20 characters with spaces between them, plus the border, the padding and the scrollbar;
	// 50 rows plus the header and the border
	width, height := table.ContentSize()
	assert.Equal(t, 83+5, width)
	assert.Equal(t, 50+3, height)

	// not while scrolled horizontally: the hidden columns still count
	pressTableKey(table, tcell.KeyRight)
	require.Equal(t, 1, table.columnOffset)
	width, _ = table.ContentSize()
	assert.Equal(t, 83+5, width)

	// the placeholder of an empty table takes a row
	table.SetData(nil)
	_, height = table.ContentSize()
	assert.Equal(t, 1+3, height)
}

func TestTable_ContentSizeIgnoresTheFilter(t *testing.T) {
	table, _, _, _ := newFilterTestTable()
	_, before := table.ContentSize()

	table.SetFilterText("daily")
	require.Len(t, table.GetEntries(), 2)
	_, after := table.ContentSize()
	assert.Equal(t, before, after, "the height counts the hidden entries as well")
}

func TestTable_FindByRowId(t *testing.T) {
	table, _, _, _ := newFilterTestTable()
	assert.Equal(t, "weekly-1", table.FindByRowId("weekly-1").name)
	assert.Nil(t, table.FindByRowId("yearly-1"))
	table.SetFilterText("daily")
	assert.Nil(t, table.FindByRowId("weekly-1"), "only the displayed ones")
}

// Regression: the cells of the selected row keep their own text color (on the background of the selection), so
// colors that mean something (e.g. the health of a pool) stay visible while a row is selected. The color was read
// from TableCell.Color, but tview.NewTableCell keeps it in the style of the cell, so selected rows were all white.
func TestSelectedRowKeepsTextColors(t *testing.T) {
	cols := []*Column{{Id: 0, Title: "Col0"}}
	table := NewTableContainer[mockEntry](
		testutil.App(),
		func(row int, columns []*Column, entry *mockEntry) []*tview.TableCell {
			return []*tview.TableCell{tview.NewTableCell("ONLINE").SetTextColor(tcell.ColorGreen)}
		},
		func(entries []*mockEntry, column *Column, inverted bool) []*mockEntry { return entries },
	)
	table.SetColumnSpec(cols, cols[0], false)
	table.SetData([]*mockEntry{{id: "1"}})

	assert.Equal(t, theme.SelectedStyle(tcell.ColorGreen), table.table.GetCell(1, 0).SelectedStyle)
}

// Regression: tview's table follows the end of the rows once all of them fit, so when it got lower (e.g. a smaller
// terminal), it showed the last rows, without the selected one.
func TestTable_SelectionStaysVisibleWhenLower(t *testing.T) {
	table, entries, _ := newWideTestTable(1)
	table.SetData(entries[:8])
	screen := testutil.NewScreen(t, 40, 12)
	table.GetLayout().SetRect(0, 0, 40, 12)
	table.table.Focus(func(tview.Primitive) {})
	table.Select(entries[1])
	drawTableLayout(table, screen)

	// the border (2) and the header row (1) leave 3 lines for data rows
	table.GetLayout().SetRect(0, 0, 40, 6)
	screen.Clear()
	drawTableLayout(table, screen)
	rowOffset, _ := table.table.GetOffset()
	assert.Zero(t, rowOffset, "the first rows are still shown")
	assert.Equal(t, entries[1], table.GetSelectedEntry())

	// a selection below the rows shown at the new height comes into view
	table.GetLayout().SetRect(0, 0, 40, 12)
	table.Select(entries[6])
	drawTableLayout(table, screen)
	table.GetLayout().SetRect(0, 0, 40, 6)
	drawTableLayout(table, screen)
	rowOffset, _ = table.table.GetOffset()
	assert.Equal(t, 4, rowOffset, "the selected row is the last one shown")
}

// Cell regions are where their column is drawn, also scrolled horizontally. A click on one runs it instead of selecting
// the row, a hovered one is highlighted.
func TestTable_CellRegions(t *testing.T) {
	table, entries, cols := newWideTestTable(4)
	table.Select(entries[0])
	var clicked []string
	table.SetCellRegionsFunc(func(entry *namedEntry, column *Column) []CellRegion {
		if column != cols[2] {
			return nil
		}
		name := entry.name
		return []CellRegion{{Start: 2, Width: 3, Key: name, OnClick: func() { clicked = append(clicked, name) }}}
	})
	screen := testutil.NewScreen(t, 40, 10)
	draw := func() { table.view.Draw(screen) }
	mouse := func(action tview.MouseAction, x int, y int) {
		event := tcell.NewEventMouse(x, y, tcell.Button1, tcell.ModNone)
		table.view.MouseHandler()(action, event, func(p tview.Primitive) {})
	}

	// col2 starts at 42, out of the view
	draw()
	for x := range 40 {
		assert.False(t, table.regions.Contains(x, 3), "x %d", x)
	}

	// scrolled to col1 and col2: col2 at 21
	pressTableKey(table, tcell.KeyRight)
	draw()
	x, y := 21+2, 3 // the third entry, below the header
	mouse(tview.MouseMove, x, y)
	draw()
	_, style, _ := screen.Get(x, y)
	_, background, _ := style.Decompose()
	assert.Equal(t, theme.Colors.Layout.CellHoverBackground, background)
	mouse(tview.MouseLeftDown, x+2, y)
	mouse(tview.MouseLeftClick, x+2, y)
	assert.Equal(t, []string{"entry-02"}, clicked)
	assert.Same(t, entries[0], table.GetSelectedEntry(), "the row is not selected")

	// next to it: the row is selected
	mouse(tview.MouseLeftDown, x+3, y)
	mouse(tview.MouseLeftClick, x+3, y)
	assert.Len(t, clicked, 1)
	assert.Same(t, entries[2], table.GetSelectedEntry())
}

// A column that is not drawn (see SetColumnShownFunc) stays active, is skipped when changing the sort column, and the
// entries are sorted by the default sort column while it is hidden, by it again once it is drawn.
func TestTable_ColumnShownFunc(t *testing.T) {
	app, _ := testutil.NewApp()
	cols := []*Column{{Id: 0, Key: "a", Title: "A"}, {Id: 1, Key: "b", Title: "B"}, {Id: 2, Key: "c", Title: "C"}}
	var sortedBy []*Column
	table := NewTableContainer[mockEntry](
		app,
		func(row int, columns []*Column, entry *mockEntry) []*tview.TableCell {
			var cells []*tview.TableCell
			for _, column := range columns {
				cells = append(cells, tview.NewTableCell(column.Title))
			}
			return cells
		},
		func(entries []*mockEntry, column *Column, inverted bool) []*mockEntry {
			sortedBy = append(sortedBy, column)
			return entries
		},
	)
	table.SetColumnSpec(cols, cols[0], false)
	table.SetActiveColumns(cols)
	table.SortBy(cols[1], false)
	shown := false
	table.SetColumnShownFunc(func(column *Column) bool { return column != cols[1] || shown })

	assert.Equal(t, []*Column{cols[0], cols[2]}, table.visibleColumns())
	assert.Equal(t, cols, table.GetColumnSpec(), "still active")
	assert.Equal(t, cols[0], sortedBy[len(sortedBy)-1], "sorted by the default column meanwhile")
	assert.True(t, table.IsSortedBy(cols[0]))
	assert.Equal(t, cols[1], table.GetColumnLayout().SortColumn, "the chosen one is kept")
	table.nextSortOrder()
	assert.True(t, table.IsSortedBy(cols[2]), "the hidden column is skipped")
	table.SortBy(cols[1], false)

	shown = true
	table.UpdateShownColumns()
	assert.Equal(t, cols, table.visibleColumns())
	assert.Equal(t, cols[1], sortedBy[len(sortedBy)-1])
	assert.True(t, table.IsSortedBy(cols[1]))
}
