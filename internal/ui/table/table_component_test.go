package table

import (
	"testing"

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
