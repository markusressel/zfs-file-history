package table

import (
	"slices"
	"strings"
	"testing"
	"zfs-file-history/internal/testutil"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// groupedEntry is an entry of a kind, e.g. the type of a disk.
type groupedEntry struct {
	name string
	kind string
}

func (e groupedEntry) TableRowId() string {
	return e.name
}

// newGroupingTestTable returns a table of entries sorted by name, which can be grouped by their kind: "b" before "a"
// (by Order).
func newGroupingTestTable() (*RowSelectionTable[groupedEntry], []*groupedEntry) {
	cols := []*Column{{Id: 0, Key: "name", Title: "Name"}}
	table := NewTableContainer[groupedEntry](
		testutil.App(),
		func(row int, columns []*Column, entry *groupedEntry) []*tview.TableCell {
			return []*tview.TableCell{tview.NewTableCell(entry.name)}
		},
		func(entries []*groupedEntry, column *Column, inverted bool) []*groupedEntry {
			slices.SortStableFunc(entries, func(a, b *groupedEntry) int {
				if inverted {
					return strings.Compare(b.name, a.name)
				}
				return strings.Compare(a.name, b.name)
			})
			return entries
		},
	)
	table.SetColumnSpec(cols, cols[0], false)
	table.SetFilterFunc(func(entry *groupedEntry, filterText string) bool {
		return strings.Contains(entry.name, filterText)
	})
	table.SetGroupings([]Grouping[groupedEntry]{{Key: "kind", Name: "Kind", GroupOf: func(entry *groupedEntry) Group {
		order := 0
		if entry.kind == "a" {
			order = 1
		}
		return Group{Title: "Kind " + entry.kind, Order: order}
	}}})
	entries := []*groupedEntry{{name: "a2", kind: "a"}, {name: "b1", kind: "b"}, {name: "a1", kind: "a"}, {name: "b2", kind: "b"}}
	table.SetData(entries)
	return table, entries
}

// groupedNames returns the names of the displayed entries in their order.
func groupedNames(table *RowSelectionTable[groupedEntry]) []string {
	var names []string
	for _, entry := range table.GetEntries() {
		names = append(names, entry.name)
	}
	return names
}

// drawGroupedTable draws the table at 40x12 and returns the lines on the screen.
func drawGroupedTable(t *testing.T, table *RowSelectionTable[groupedEntry]) []string {
	screen := tcell.NewSimulationScreen("UTF-8")
	require.NoError(t, screen.Init())
	screen.SetSize(40, 12)
	table.GetLayout().SetRect(0, 0, 40, 12)
	table.GetLayout().Draw(screen)
	screen.Show()
	cells, width, _ := screen.GetContents()
	var lines []string
	for y := 0; y < len(cells)/width; y++ {
		var line strings.Builder
		for _, cell := range cells[y*width : (y+1)*width] {
			if len(cell.Runes) > 0 {
				line.WriteRune(cell.Runes[0])
			}
		}
		lines = append(lines, strings.TrimSpace(strings.Trim(line.String(), "│")))
	}
	return lines
}

func TestGrouping_GroupsTheEntries(t *testing.T) {
	table, _ := newGroupingTestTable()
	assert.Equal(t, []string{"a1", "a2", "b1", "b2"}, groupedNames(table), "not grouped at first")

	table.CycleGrouping()
	assert.Equal(t, "kind", table.GetGrouping())
	assert.Equal(t, []string{"b1", "b2", "a1", "a2"}, groupedNames(table), "by the order of the groups, sorted within")
	lines := drawGroupedTable(t, table)
	assert.Equal(t, []string{"Name ↑", "Kind b  2", "b1", "b2", "Kind a  2", "a1", "a2"}, lines[1:8])

	table.toggleSortDirection()
	assert.Equal(t, []string{"b2", "b1", "a2", "a1"}, groupedNames(table), "the order of the groups stays")

	table.CycleGrouping()
	assert.Empty(t, table.GetGrouping(), "after the last one, not grouped")
	assert.Equal(t, []string{"Name ↓", "b2", "b1", "a2", "a1"}, drawGroupedTable(t, table)[1:6])
}

func TestGrouping_KeyAndCommands(t *testing.T) {
	table, _ := newGroupingTestTable()
	table.table.GetInputCapture()(tcell.NewEventKey(tcell.KeyRune, 'b', tcell.ModNone))
	assert.Equal(t, "kind", table.GetGrouping())

	commands := table.GroupingCommands()
	require.Len(t, commands, 3)
	assert.Equal(t, "group", commands[0].Query, "searches the others, which are only listed while searching")
	assert.Equal(t, "Do not group", commands[1].Name)
	assert.True(t, commands[1].SearchOnly)
	assert.Equal(t, "Group by Kind", commands[2].Name)
	assert.True(t, commands[2].SearchOnly)
	commands[1].Run()
	assert.Empty(t, table.GetGrouping())

	ungrouped, _, _, _ := newFilterTestTable()
	assert.Empty(t, ungrouped.GroupingCommands(), "a table without groupings")
}

func TestGrouping_SelectionSkipsTheGroups(t *testing.T) {
	table, entries := newGroupingTestTable()
	// SetData sorts the slice itself
	byName := map[string]*groupedEntry{}
	for _, entry := range entries {
		byName[entry.name] = entry
	}
	b1, b2, a1 := byName["b1"], byName["b2"], byName["a1"]
	table.CycleGrouping()
	drawGroupedTable(t, table)
	press := func(key tcell.Key) {
		table.table.InputHandler()(tcell.NewEventKey(key, 0, tcell.ModNone), func(tview.Primitive) {})
	}

	table.SelectHeader()
	press(tcell.KeyDown)
	assert.Equal(t, b1, table.GetSelectedEntry(), "the first entry, below its group")
	table.Select(b2)
	press(tcell.KeyDown)
	assert.Equal(t, a1, table.GetSelectedEntry(), "over the next group")
	press(tcell.KeyUp)
	assert.Equal(t, b2, table.GetSelectedEntry())
	table.Select(b1)
	press(tcell.KeyUp)
	assert.Nil(t, table.GetSelectedEntry(), "the header")

	table.Select(b2)
	table.Down()
	assert.Equal(t, a1, table.GetSelectedEntry(), "Down skips the groups too")
	table.Up()
	assert.Equal(t, b2, table.GetSelectedEntry())

	// a click on a group selects nothing
	lines := drawGroupedTable(t, table)
	groupY := slices.Index(lines, "Kind a  2")
	require.Positive(t, groupY)
	table.table.MouseHandler()(tview.MouseLeftClick, tcell.NewEventMouse(5, groupY, tcell.ButtonNone, tcell.ModNone), func(tview.Primitive) {})
	assert.Equal(t, b2, table.GetSelectedEntry())

	// the selection is kept when grouping is switched
	table.CycleGrouping()
	assert.Equal(t, b2, table.GetSelectedEntry())
}

func TestGrouping_Filter(t *testing.T) {
	table, _ := newGroupingTestTable()
	table.CycleGrouping()
	table.SetFilterText("1")
	assert.Equal(t, []string{"Name ↑", "Kind b  1", "b1", "Kind a  1", "a1", ""}, drawGroupedTable(t, table)[1:7], "counts of the shown entries")
	table.SetFilterText("a")
	assert.Equal(t, []string{"Name ↑", "Kind a  2", "a1", "a2", ""}, drawGroupedTable(t, table)[1:6], "groups without entries are hidden")
}

func TestGrouping_ContentSize(t *testing.T) {
	table, _ := newGroupingTestTable()
	width, height := table.ContentSize()
	table.CycleGrouping()
	groupedWidth, groupedHeight := table.ContentSize()
	assert.Equal(t, height+2, groupedHeight, "a row per group")
	assert.Equal(t, width, groupedWidth, "the titles of the groups do not widen the columns")
}

func TestGrouping_SavedWithTheLayout(t *testing.T) {
	store, _ := newLayoutTestStore(t)
	table, _ := newGroupingTestTable()
	table.BindColumnLayout(store, "grouped", table.GetColumnSpec())

	table.CycleGrouping()
	saved, ok := store.TableLayout("grouped")
	require.True(t, ok)
	assert.Equal(t, "kind", saved.Grouping)

	restored, _ := newGroupingTestTable()
	restored.BindColumnLayout(store, "grouped", restored.GetColumnSpec())
	assert.Equal(t, "kind", restored.GetGrouping())

	restored.ResetColumnLayout()
	assert.Empty(t, restored.GetGrouping(), "not grouped by default")
}
