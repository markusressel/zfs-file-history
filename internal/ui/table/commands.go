package table

import (
	"zfs-file-history/internal/ui/shortcut_helper"
)

// StartFilter starts typing a filter, like ctrl+f. Does nothing if the table has no filter (see SetFilterFunc).
// Must be called on the UI thread.
func (c *RowSelectionTable[T]) StartFilter() {
	if c.filterMatches != nil {
		c.startEditingFilter()
	}
}

// SortCommands returns commands for the command menu (see dialog.CommandMenu) to sort by each of the shown columns
// and to flip the direction.
func (c *RowSelectionTable[T]) SortCommands() []shortcut_helper.ShortcutEntry {
	var commands []shortcut_helper.ShortcutEntry
	for _, column := range c.columnSpec {
		commands = append(commands, shortcut_helper.ShortcutEntry{
			Name:        "Sort by " + column.Title,
			Description: "Sort the entries by the " + column.Title + " column",
			Group:       shortcut_helper.GroupView,
			Run: func() {
				c.SortBy(column, c.sortInverted)
				c.updateTableContents()
				c.notifyColumnLayoutChanged()
			},
			MenuOnly: true,
		})
	}
	return append(commands, shortcut_helper.ShortcutEntry{
		Name:        "Flip sort direction",
		Description: "Switch between ascending and descending order",
		Group:       shortcut_helper.GroupView,
		Run:         c.toggleSortDirection,
		MenuOnly:    true,
	})
}
