package table

import (
	"cmp"
	"fmt"
	"slices"
	"zfs-file-history/internal/ui/shortcut_helper"
	"zfs-file-history/internal/ui/theme"
	"zfs-file-history/internal/ui/txwidgets"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// Group is the group of an entry, see Grouping.
type Group struct {
	// Title is shown in the row above the entries of the group, e.g. "Hard disks"
	Title string
	// Order is the position of the group among the others: groups are shown by Order, then by Title
	Order int
}

// Grouping is a way to group the entries of a table, see RowSelectionTable.SetGroupings.
type Grouping[T any] struct {
	// Key identifies it in the saved layout (see BindColumnLayout), so it must never change
	Key string
	// Name is what the entries are grouped by, e.g. "Type", shown in the command menu
	Name string
	// GroupOf returns the group of an entry
	GroupOf func(entry *T) Group
}

// groupRow is a row of the table that starts a group, see RowSelectionTable.updateTableContents.
type groupRow struct {
	group Group
	// count is the number of displayed entries in the group
	count int
}

// compareGroups orders groups by their Order, then by their Title.
func compareGroups(a Group, b Group) int {
	if result := cmp.Compare(a.Order, b.Order); result != 0 {
		return result
	}
	return cmp.Compare(a.Title, b.Title)
}

// SetGroupings sets the ways the entries can be grouped, which the user switches between with CycleGrouping (none
// at first, see SetGrouping). Call it before BindColumnLayout, which restores the saved one. Must be called on the UI
// thread.
func (c *RowSelectionTable[T]) SetGroupings(groupings []Grouping[T]) {
	c.groupings = groupings
	c.grouping = nil
	c.Resort()
}

// SetGrouping groups the entries by the grouping with the key, "" (or an unknown key): not at all. The selected
// entry stays selected. Unlike CycleGrouping, the saved layout is not changed. Must be called on the UI thread.
func (c *RowSelectionTable[T]) SetGrouping(key string) {
	c.grouping = nil
	for i := range c.groupings {
		if c.groupings[i].Key == key {
			c.grouping = &c.groupings[i]
		}
	}
	c.Resort()
}

// GetGrouping returns the key of the grouping of the entries, "" if they are not grouped.
func (c *RowSelectionTable[T]) GetGrouping() string {
	if c.grouping == nil {
		return ""
	}
	return c.grouping.Key
}

// CycleGrouping groups the entries by the next grouping (see SetGroupings), after the last one not at all, and
// saves it with the layout. Must be called on the UI thread.
func (c *RowSelectionTable[T]) CycleGrouping() {
	if len(c.groupings) == 0 {
		return
	}
	next := ""
	index := slices.IndexFunc(c.groupings, func(grouping Grouping[T]) bool { return grouping.Key == c.GetGrouping() })
	if index+1 < len(c.groupings) {
		next = c.groupings[index+1].Key
	}
	c.selectGrouping(next)
}

// selectGrouping groups the entries by the grouping with the key, as the user chose, and saves it with the layout.
func (c *RowSelectionTable[T]) selectGrouping(key string) {
	c.SetGrouping(key)
	c.notifyColumnLayoutChanged()
}

// GroupingCommands returns commands for the command menu (see dialog.CommandMenu) to group the entries by each of
// the groupings, or not at all. None if the table has no groupings.
func (c *RowSelectionTable[T]) GroupingCommands() []shortcut_helper.ShortcutEntry {
	if len(c.groupings) == 0 {
		return nil
	}
	// only listed while searching, like the commands to sort; "Group by…" searches them
	commands := []shortcut_helper.ShortcutEntry{{
		Name:        "Group by…",
		Description: "Choose how to group the entries",
		Group:       shortcut_helper.GroupView,
		Query:       "group",
		MenuOnly:    true,
	}, {
		Name:        "Do not group",
		Description: "Show the entries in one list",
		Group:       shortcut_helper.GroupView,
		Run:         func() { c.selectGrouping("") },
		MenuOnly:    true,
		SearchOnly:  true,
	}}
	for _, grouping := range c.groupings {
		commands = append(commands, shortcut_helper.ShortcutEntry{
			Name:        "Group by " + grouping.Name,
			Description: "Show the entries in groups by their " + grouping.Name,
			Group:       shortcut_helper.GroupView,
			Run:         func() { c.selectGrouping(grouping.Key) },
			MenuOnly:    true,
			SearchOnly:  true,
		})
	}
	return commands
}

// sortIntoGroups sorts the entries by their group, keeping the order within each group. Does nothing while they are
// not grouped.
func (c *RowSelectionTable[T]) sortIntoGroups(entries []*T) []*T {
	if c.grouping == nil {
		return entries
	}
	groups := make(map[*T]Group, len(entries))
	for _, entry := range entries {
		groups[entry] = c.grouping.GroupOf(entry)
	}
	slices.SortStableFunc(entries, func(a, b *T) int { return compareGroups(groups[a], groups[b]) })
	return entries
}

// groupCount returns the number of groups of the entries, 0 while they are not grouped.
func (c *RowSelectionTable[T]) groupCount(entries []*T) int {
	if c.grouping == nil {
		return 0
	}
	groups := map[Group]bool{}
	for _, entry := range entries {
		groups[c.grouping.GroupOf(entry)] = true
	}
	return len(groups)
}

// setGroupRow adds a row that starts the group, which cannot be selected: its title is drawn over it, across all
// columns (see drawGroupRows), so a long title does not widen the first column.
func (c *RowSelectionTable[T]) setGroupRow(row int, columns int, group groupRow) {
	c.groupRows[row] = group
	for column := range max(columns, 1) {
		c.table.SetCell(row, column, tview.NewTableCell("").
			SetSelectable(false).
			// a click selects nothing, tview would select the row
			SetClickedFunc(func() bool { return true }))
	}
}

// isGroupRow returns whether the row of the table starts a group.
func (c *RowSelectionTable[T]) isGroupRow(row int) bool {
	_, ok := c.groupRows[row]
	return ok
}

// drawGroupRows draws the titles of the visible group rows across the width of the table. Runs while drawing.
func (c *RowSelectionTable[T]) drawGroupRows(screen tcell.Screen) {
	if len(c.groupRows) == 0 {
		return
	}
	x, y, width, height := c.table.GetInnerRect()
	rowOffset, _ := c.table.GetOffset()
	colors := theme.Colors.Layout.Table
	for row, group := range c.groupRows {
		// below the fixed header row, scrolled by rowOffset
		screenY := y + row - rowOffset
		if row-1 < rowOffset || screenY >= y+height {
			continue
		}
		for column := range width {
			screen.SetContent(x+column, screenY, ' ', nil, tcell.StyleDefault)
		}
		text := "[::b]" + txwidgets.Span(colors.GroupTitle, "%s", group.group.Title) + "[::-]" +
			txwidgets.Span(colors.GroupCount, "  %s", fmt.Sprint(group.count))
		tview.Print(screen, text, x, screenY, width, tview.AlignLeft, colors.GroupTitle)
	}
}
