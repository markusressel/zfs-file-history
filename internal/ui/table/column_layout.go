package table

import (
	"slices"
	"zfs-file-history/internal/state"
)

// ColumnLayout is the user-configurable layout of a table: the displayed columns and the sort order.
type ColumnLayout struct {
	Columns      []*Column
	SortColumn   *Column
	SortInverted bool
}

// savedLayoutBinding connects a table to its saved layout, see BindColumnLayout.
type savedLayoutBinding struct {
	store         *state.Store
	key           string
	defaultLayout ColumnLayout
	// allColumns are all columns of the table, saved as the known ones
	allColumns []*Column
	// defaultGrouping is the grouping at the time of BindColumnLayout, see SetGroupings
	defaultGrouping string
}

// GetColumnLayout returns the displayed columns and the sort order.
func (c *RowSelectionTable[T]) GetColumnLayout() ColumnLayout {
	return ColumnLayout{
		Columns:      slices.Clone(c.columnSpec),
		SortColumn:   c.sortByColumn,
		SortInverted: c.sortInverted,
	}
}

// SetColumnLayout displays the given columns and sorts by the given column. Unlike SetActiveColumns, this does not
// notify the callback set with SetColumnLayoutChangedCallback. A layout without columns is ignored, a sort column
// that is not displayed is replaced like in SetActiveColumns. Must be called on the UI thread.
func (c *RowSelectionTable[T]) SetColumnLayout(layout ColumnLayout) {
	if len(layout.Columns) == 0 {
		return
	}
	c.columnSpec = slices.Clone(layout.Columns)
	// keep the horizontal scroll position, as long as a column is left
	c.columnOffset = min(c.columnOffset, len(c.columnSpec)-1)
	c.sortByColumn = layout.SortColumn
	c.sortInverted = layout.SortInverted
	if !slices.Contains(c.columnSpec, c.sortByColumn) {
		if c.defaultSortColumn != nil && slices.Contains(c.columnSpec, c.defaultSortColumn) {
			c.sortByColumn = c.defaultSortColumn
			c.sortInverted = c.defaultSortInverted
		} else {
			c.sortByColumn = c.columnSpec[0]
		}
	}
	c.SortBy(c.sortByColumn, c.sortInverted)
	c.updateTableContents()
}

// SetColumnLayoutChangedCallback sets a function that is called (on the UI thread) whenever the user changes the
// displayed columns (SetActiveColumns) or the sort order (keys on the header row).
func (c *RowSelectionTable[T]) SetColumnLayoutChangedCallback(f func(layout ColumnLayout)) {
	c.columnLayoutChangedCallback = f
}

func (c *RowSelectionTable[T]) notifyColumnLayoutChanged() {
	if c.columnLayoutChangedCallback != nil {
		c.columnLayoutChangedCallback(c.GetColumnLayout())
	}
	if c.savedLayout != nil {
		saved := toSavedLayout(c.GetColumnLayout())
		saved.Grouping = c.GetGrouping()
		saved.Known = columnKeys(c.savedLayout.allColumns)
		c.savedLayout.store.SetTableLayout(c.savedLayout.key, saved)
	}
}

// BindColumnLayout restores the layout saved under key in store and saves every change the user makes to it.
// The current layout becomes the default layout, see ResetColumnLayout, so call this after setting up the columns
// (and the groupings, see SetGroupings). Columns of the default layout that the saved one did not know yet (added in
// a later version) are added to it, next to the column they follow in the default layout.
// allColumns are all columns of the table, used to resolve the saved column keys.
// Does nothing if store is nil. Must be called on the UI thread.
func (c *RowSelectionTable[T]) BindColumnLayout(store *state.Store, key string, allColumns []*Column) {
	if store == nil {
		return
	}
	c.savedLayout = &savedLayoutBinding{
		store:           store,
		key:             key,
		defaultLayout:   c.GetColumnLayout(),
		allColumns:      slices.Clone(allColumns),
		defaultGrouping: c.GetGrouping(),
	}
	if saved, ok := store.TableLayout(key); ok {
		saved.Columns = addUnknownColumns(saved, c.savedLayout.defaultLayout.Columns, allColumns)
		if layout, ok := fromSavedLayout(saved, allColumns); ok {
			c.SetColumnLayout(layout)
			c.SetGrouping(saved.Grouping)
		}
	}
}

// ResetColumnLayout restores the default layout (the one at the time of BindColumnLayout) and removes the saved one.
// Must be called on the UI thread.
func (c *RowSelectionTable[T]) ResetColumnLayout() {
	if c.savedLayout == nil {
		return
	}
	c.SetColumnLayout(c.savedLayout.defaultLayout)
	c.SetGrouping(c.savedLayout.defaultGrouping)
	c.savedLayout.store.DeleteTableLayout(c.savedLayout.key)
}

// CanResetColumnLayout returns whether ResetColumnLayout is available, i.e. the layout is saved.
func (c *RowSelectionTable[T]) CanResetColumnLayout() bool {
	return c.savedLayout != nil
}

// SaveColumnLayout saves the layout under key in store, for tables that are not a RowSelectionTable (e.g. one with
// virtual content, see tview.TableContent). Does nothing if store is nil.
func SaveColumnLayout(store *state.Store, key string, layout ColumnLayout) {
	store.SetTableLayout(key, toSavedLayout(layout))
}

// LoadColumnLayout returns the layout saved under key in store with SaveColumnLayout, false if there is none or
// none of its columns is left. allColumns are all columns of the table, used to resolve the saved column keys.
func LoadColumnLayout(store *state.Store, key string, allColumns []*Column) (ColumnLayout, bool) {
	saved, ok := store.TableLayout(key)
	if !ok {
		return ColumnLayout{}, false
	}
	return fromSavedLayout(saved, allColumns)
}

func toSavedLayout(layout ColumnLayout) state.TableLayout {
	saved := state.TableLayout{SortInverted: layout.SortInverted}
	for _, column := range layout.Columns {
		saved.Columns = append(saved.Columns, column.Key)
	}
	if layout.SortColumn != nil {
		saved.SortColumn = layout.SortColumn.Key
	}
	return saved
}

// fromSavedLayout resolves the column keys of a saved layout. Unknown keys (e.g. of removed columns) and duplicates
// are skipped. Returns false if no column is left.
func fromSavedLayout(saved state.TableLayout, allColumns []*Column) (ColumnLayout, bool) {
	byKey := make(map[string]*Column, len(allColumns))
	for _, column := range allColumns {
		byKey[column.Key] = column
	}

	layout := ColumnLayout{SortInverted: saved.SortInverted}
	for _, key := range saved.Columns {
		column, ok := byKey[key]
		if !ok || slices.Contains(layout.Columns, column) {
			continue
		}
		layout.Columns = append(layout.Columns, column)
	}
	layout.SortColumn = byKey[saved.SortColumn]
	return layout, len(layout.Columns) > 0
}

// columnKeys returns the keys of the columns.
func columnKeys(columns []*Column) []string {
	keys := make([]string, len(columns))
	for i, column := range columns {
		keys[i] = column.Key
	}
	return keys
}

// addUnknownColumns returns the column keys of the saved layout with the columns of the default layout added that
// it did not know (see TableLayout.Known; layouts without it knew all but the columns AddedLater), each after the
// column it follows in the default layout (or first).
func addUnknownColumns(saved state.TableLayout, defaultColumns []*Column, allColumns []*Column) []string {
	known := saved.Known
	if known == nil {
		for _, column := range allColumns {
			if !column.AddedLater {
				known = append(known, column.Key)
			}
		}
	}
	keys := slices.Clone(saved.Columns)
	for i, column := range defaultColumns {
		if slices.Contains(known, column.Key) || slices.Contains(keys, column.Key) {
			continue
		}
		position := 0
		for j := i - 1; j >= 0; j-- {
			if index := slices.Index(keys, defaultColumns[j].Key); index >= 0 {
				position = index + 1
				break
			}
		}
		keys = slices.Insert(keys, position, column.Key)
	}
	return keys
}
