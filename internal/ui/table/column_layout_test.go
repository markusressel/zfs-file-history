package table

import (
	"path/filepath"
	"testing"
	"zfs-file-history/internal/state"

	"github.com/rivo/tview"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var (
	layoutColumnName = &Column{Id: 0, Key: "name", Title: "Name"}
	layoutColumnSize = &Column{Id: 1, Key: "size", Title: "Size"}
	layoutColumnDate = &Column{Id: 2, Key: "date", Title: "Date"}
	layoutAllColumns = []*Column{layoutColumnName, layoutColumnSize, layoutColumnDate}
)

// newLayoutTestTable returns a table set up like the browsers do: all columns, the name and size columns active,
// sorted by name.
func newLayoutTestTable() *RowSelectionTable[mockEntry] {
	table := NewTableContainer[mockEntry](
		tview.NewApplication(),
		func(row int, columns []*Column, entry *mockEntry) []*tview.TableCell {
			return nil
		},
		func(entries []*mockEntry, column *Column, inverted bool) []*mockEntry {
			return entries
		},
	)
	table.SetColumnSpec(layoutAllColumns, layoutColumnName, false)
	table.SetActiveColumns([]*Column{layoutColumnName, layoutColumnSize})
	return table
}

// newLayoutTestStore returns a store in a temporary directory, and its path.
func newLayoutTestStore(t *testing.T) (*state.Store, string) {
	path := filepath.Join(t.TempDir(), "state.json")
	store := state.Load(path)
	// stop a pending background save before the temporary directory is removed
	t.Cleanup(func() { _ = store.Flush() })
	return store, path
}

func TestBindColumnLayout_SavesChanges(t *testing.T) {
	store, path := newLayoutTestStore(t)
	table := newLayoutTestTable()
	table.BindColumnLayout(store, "files", layoutAllColumns)

	// binding does not save the default layout
	_, ok := store.TableLayout("files")
	assert.False(t, ok)

	table.SetActiveColumns([]*Column{layoutColumnDate, layoutColumnName})
	table.nextSortOrder()
	table.toggleSortDirection()

	expected := state.TableLayout{Columns: []string{"date", "name"}, SortColumn: "date", SortInverted: true}
	saved, ok := store.TableLayout("files")
	require.True(t, ok)
	assert.Equal(t, expected, saved)

	require.NoError(t, store.Flush())
	saved, _ = state.Load(path).TableLayout("files")
	assert.Equal(t, expected, saved)
}

func TestBindColumnLayout_RestoresSavedLayout(t *testing.T) {
	store, _ := newLayoutTestStore(t)
	store.SetTableLayout("files", state.TableLayout{
		// unknown and duplicate keys are skipped
		Columns:      []string{"size", "removed", "date", "size"},
		SortColumn:   "date",
		SortInverted: true,
	})

	table := newLayoutTestTable()
	table.BindColumnLayout(store, "files", layoutAllColumns)

	assert.Equal(t, ColumnLayout{
		Columns:      []*Column{layoutColumnSize, layoutColumnDate},
		SortColumn:   layoutColumnDate,
		SortInverted: true,
	}, table.GetColumnLayout())
}

func TestBindColumnLayout_SortColumnNotDisplayed(t *testing.T) {
	store, _ := newLayoutTestStore(t)
	store.SetTableLayout("files", state.TableLayout{Columns: []string{"size", "date"}, SortColumn: "name", SortInverted: true})

	table := newLayoutTestTable()
	table.BindColumnLayout(store, "files", layoutAllColumns)

	// like SetActiveColumns: the first column, as the default sort column (name) is not displayed
	layout := table.GetColumnLayout()
	assert.Equal(t, []*Column{layoutColumnSize, layoutColumnDate}, layout.Columns)
	assert.Equal(t, layoutColumnSize, layout.SortColumn)
}

func TestBindColumnLayout_IgnoresUnusableLayout(t *testing.T) {
	store, _ := newLayoutTestStore(t)
	store.SetTableLayout("files", state.TableLayout{Columns: []string{"removed"}, SortColumn: "removed"})

	table := newLayoutTestTable()
	defaultLayout := table.GetColumnLayout()
	table.BindColumnLayout(store, "files", layoutAllColumns)

	assert.Equal(t, defaultLayout, table.GetColumnLayout())
}

func TestBindColumnLayout_NilStore(t *testing.T) {
	table := newLayoutTestTable()
	table.BindColumnLayout(nil, "files", layoutAllColumns)

	assert.False(t, table.CanResetColumnLayout())
	// changes still work, but are not saved
	table.SetActiveColumns([]*Column{layoutColumnDate})
	table.ResetColumnLayout()
	assert.Equal(t, []*Column{layoutColumnDate}, table.GetColumnSpec())
}

func TestResetColumnLayout(t *testing.T) {
	store, _ := newLayoutTestStore(t)
	table := newLayoutTestTable()
	defaultLayout := table.GetColumnLayout()
	table.BindColumnLayout(store, "files", layoutAllColumns)

	table.SetActiveColumns([]*Column{layoutColumnDate})
	table.toggleSortDirection()
	require.True(t, table.CanResetColumnLayout())

	table.ResetColumnLayout()

	assert.Equal(t, defaultLayout, table.GetColumnLayout())
	_, ok := store.TableLayout("files")
	assert.False(t, ok, "the saved layout is removed")
}

func TestSetColumnLayout_DoesNotNotify(t *testing.T) {
	table := newLayoutTestTable()
	var notified []ColumnLayout
	table.SetColumnLayoutChangedCallback(func(layout ColumnLayout) { notified = append(notified, layout) })

	table.SetColumnLayout(ColumnLayout{Columns: []*Column{layoutColumnSize}, SortColumn: layoutColumnSize})
	assert.Empty(t, notified)

	table.SetActiveColumns([]*Column{layoutColumnSize, layoutColumnName})
	require.Len(t, notified, 1)
	assert.Equal(t, []*Column{layoutColumnSize, layoutColumnName}, notified[0].Columns)

	// ignored, like an empty SetActiveColumns
	table.SetColumnLayout(ColumnLayout{})
	assert.Equal(t, []*Column{layoutColumnSize, layoutColumnName}, table.GetColumnSpec())
}
