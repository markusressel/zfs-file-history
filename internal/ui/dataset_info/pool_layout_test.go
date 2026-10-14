package dataset_info

import (
	"strings"
	"testing"
	"zfs-file-history/internal/ui/theme"
	"zfs-file-history/internal/zfs"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testPool is a pool of 100 GiB used: the root's own 10, "a" (own 5, children 20: "a/x" 15 and "a/y" 5) and "b" 65.
func testPool() []*zfs.DatasetListEntry {
	return []*zfs.DatasetListEntry{
		{Name: "tank", Used: 100 * gib, UsedByChildren: 90 * gib},
		// not in the order of the tree
		{Name: "tank/b", Used: 65 * gib},
		{Name: "tank/a/y", Used: 5 * gib},
		{Name: "tank/a", Used: 25 * gib, UsedByChildren: 20 * gib},
		{Name: "tank/a/x", Used: 15 * gib},
	}
}

func TestDatasetOffset(t *testing.T) {
	entries := testPool()
	tests := []struct {
		name     string
		expected uint64
	}{
		{"tank", 0},
		// after the root's own space
		{"tank/a", 10 * gib},
		// within its parent: after its own space
		{"tank/a/x", 15 * gib},
		{"tank/a/y", 30 * gib},
		{"tank/b", 35 * gib},
		// not listed (e.g. created since): after the listed children of its parent
		{"tank/a/new", 35 * gib},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			offset, ok := datasetOffset(entries, test.name)
			assert.True(t, ok)
			assert.Equal(t, test.expected, offset)
		})
	}

	_, ok := datasetOffset(entries, "tank/missing/child")
	assert.False(t, ok, "a parent is not listed")
}

// Moving from a parent to its child only moves the highlight: the child is drawn within the cells of its parent.
func TestFormatPoolBar_ChildWithinItsParent(t *testing.T) {
	entries := testPool()
	facts := func(name string, entry *zfs.DatasetListEntry) *datasetFacts {
		offset, _ := datasetOffset(entries, name)
		return &datasetFacts{
			used: entry.Used, usedByChildren: entry.UsedByChildren, usedByDataset: entry.Used - entry.UsedByChildren,
			poolUsed: 100 * gib, poolAvailable: 100 * gib, poolSpaceKnown: true, poolOffset: offset, offsetKnown: true,
		}
	}
	// 200 GiB on 20 cells: 10 GiB a cell
	parent := formatPoolBar(facts("tank/a", entries[3]))
	child := formatPoolBar(facts("tank/a/x", entries[4]))
	assert.Equal(t, []string{"others", "data/children", "children", "children/others", "others"}, barCells(t, parent)[:5],
		"after the root's own 10 GiB")
	assert.Equal(t, []string{"others", "others/data", "data", "others", "others"}, barCells(t, child)[:5],
		"within the cells of its parent, after its own 5 GiB")
}

// barCells returns the parts of the bar in each cell, e.g. "data", or "data/children" for a cell split between them.
func barCells(t *testing.T, bar string) []string {
	colors := theme.Colors.Info
	names := map[int32]string{
		colors.SpaceData.Hex(): "data", colors.SpaceSnapshots.Hex(): "snapshots", colors.SpaceChildren.Hex(): "children",
		colors.SpaceOthers.Hex(): "others", colors.BarEmpty.Hex(): "free",
	}
	screen := tcell.NewSimulationScreen("UTF-8")
	require.NoError(t, screen.Init())
	screen.SetSize(spaceBarWidth, 1)
	bar, _, _ = strings.Cut(bar, " ")
	tview.Print(screen, bar, 0, 0, spaceBarWidth, tview.AlignLeft, tcell.ColorWhite)
	var cells []string
	for x := range spaceBarWidth {
		text, style, _ := screen.Get(x, 0)
		foreground, background, _ := style.Decompose()
		if text == "█" || text == " " {
			cells = append(cells, names[background.Hex()])
		} else {
			cells = append(cells, names[foreground.Hex()]+"/"+names[background.Hex()])
		}
	}
	return cells
}
