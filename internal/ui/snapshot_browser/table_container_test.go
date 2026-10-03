package snapshot_browser

import (
	"math"
	"testing"
	"zfs-file-history/internal/data"
	"zfs-file-history/internal/data/diff_state"
	"zfs-file-history/internal/ui/table"
	"zfs-file-history/internal/ui/theme"
	uiutil "zfs-file-history/internal/ui/util"
	"zfs-file-history/internal/zfs"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCreateSnapshotBrowserTableCells_ClonesColumn(t *testing.T) {
	entry := &data.SnapshotBrowserEntry{
		Snapshot: &zfs.Snapshot{
			Name: "snap-a",
			Properties: zfs.SnapshotProperties{
				Clones: 42,
			},
		},
		DiffState: diff_state.Unknown,
	}

	snapshotBrowser := &SnapshotBrowserComponent{}
	cells := snapshotBrowser.createSnapshotBrowserTableCells(0, []*table.Column{columnClones}, entry)

	if assert.Len(t, cells, 1) {
		assert.Equal(t, "42", cells[0].Text)
	}
}

func TestCreateSnapshotBrowserTableSortFunction_ClonesAscendingAndDescending(t *testing.T) {
	entries := []*data.SnapshotBrowserEntry{
		newSnapshotEntryWithClones("low", 0),
		newSnapshotEntryWithClones("high", math.MaxUint64),
		newSnapshotEntryWithClones("mid", 1),
	}

	ascending := append([]*data.SnapshotBrowserEntry{}, entries...)
	createSnapshotBrowserTableSortFunction(ascending, columnClones, false)
	assert.Equal(t, []string{"low", "mid", "high"}, snapshotNames(ascending))

	descending := append([]*data.SnapshotBrowserEntry{}, entries...)
	createSnapshotBrowserTableSortFunction(descending, columnClones, true)
	assert.Equal(t, []string{"high", "mid", "low"}, snapshotNames(descending))
}

func newSnapshotEntryWithClones(name string, clones uint64) *data.SnapshotBrowserEntry {
	return &data.SnapshotBrowserEntry{
		Snapshot: &zfs.Snapshot{
			Name: name,
			Properties: zfs.SnapshotProperties{
				Clones: clones,
			},
		},
		DiffState: diff_state.Unknown,
	}
}

func snapshotNames(entries []*data.SnapshotBrowserEntry) []string {
	result := make([]string, 0, len(entries))
	for _, entry := range entries {
		result = append(result, entry.Snapshot.Name)
	}
	return result
}

func TestHoldsColumn(t *testing.T) {
	newEntry := func(name string, holds uint64) *data.SnapshotBrowserEntry {
		return &data.SnapshotBrowserEntry{Snapshot: &zfs.Snapshot{Name: name, Properties: zfs.SnapshotProperties{Holds: holds}}}
	}
	snapshotBrowser := &SnapshotBrowserComponent{}

	// empty without holds, so held snapshots stand out
	cells := snapshotBrowser.createSnapshotBrowserTableCells(0, []*table.Column{columnHolds}, newEntry("a", 0))
	assert.Equal(t, "", cells[0].Text)
	cells = snapshotBrowser.createSnapshotBrowserTableCells(0, []*table.Column{columnHolds}, newEntry("a", 2))
	assert.Equal(t, "2", cells[0].Text)

	entries := []*data.SnapshotBrowserEntry{newEntry("two", 2), newEntry("none", 0), newEntry("one", 1)}
	createSnapshotBrowserTableSortFunction(entries, columnHolds, false)
	assert.Equal(t, []string{"none", "one", "two"}, snapshotNames(entries))
}

// Written is the space written since the previous snapshot: dimmed for empty snapshots, sorted largest first.
func TestWrittenColumn(t *testing.T) {
	newEntry := func(name string, written uint64) *data.SnapshotBrowserEntry {
		return &data.SnapshotBrowserEntry{Snapshot: &zfs.Snapshot{Name: name, Properties: zfs.SnapshotProperties{Written: written}}}
	}
	snapshotBrowser := &SnapshotBrowserComponent{}

	empty := snapshotBrowser.createSnapshotBrowserTableCells(0, []*table.Column{columnWritten}, newEntry("a", 0))[0]
	written := snapshotBrowser.createSnapshotBrowserTableCells(0, []*table.Column{columnWritten}, newEntry("b", 3*1024*1024))[0]
	assert.Equal(t, uiutil.StableLengthHumanizedBytes(0), empty.Text)
	assert.Equal(t, uiutil.StableLengthHumanizedBytes(3*1024*1024), written.Text)
	foreground := func(cell *tview.TableCell) tcell.Color {
		color, _, _ := cell.Style.Decompose()
		return color
	}
	assert.Equal(t, theme.Colors.Layout.Table.ZeroSize, foreground(empty))
	assert.NotEqual(t, theme.Colors.Layout.Table.ZeroSize, foreground(written))

	// beyond the range of int, where subtracting would overflow
	entries := []*data.SnapshotBrowserEntry{newEntry("small", 1), newEntry("none", 0), newEntry("huge", math.MaxUint64), newEntry("big", 1<<40)}
	createSnapshotBrowserTableSortFunction(entries, columnWritten, false)
	assert.Equal(t, []string{"huge", "big", "small", "none"}, snapshotNames(entries))
	createSnapshotBrowserTableSortFunction(entries, columnWritten, true)
	assert.Equal(t, []string{"none", "small", "big", "huge"}, snapshotNames(entries))
}

// Used and Written are colored by how big they are compared to all snapshots: the smallest calm, the largest warm,
// 0 dimmed.
func TestSizeColors(t *testing.T) {
	newSnapshot := func(name string, used uint64, written uint64) *zfs.Snapshot {
		return &zfs.Snapshot{Name: name, Properties: zfs.SnapshotProperties{Used: used, Written: written}}
	}
	snapshots := []*zfs.Snapshot{newSnapshot("small", 1<<10, 0), newSnapshot("large", 1<<30, 1<<20), newSnapshot("none", 0, 1<<10)}
	snapshotBrowser := &SnapshotBrowserComponent{}
	snapshotBrowser.updateSizeScales(snapshots)

	color := func(snapshot *zfs.Snapshot, column *table.Column) tcell.Color {
		cell := snapshotBrowser.createSnapshotBrowserTableCells(0, []*table.Column{column}, &data.SnapshotBrowserEntry{Snapshot: snapshot})[0]
		foreground, _, _ := cell.Style.Decompose()
		return foreground
	}
	stops := theme.Colors.Magnitude
	zero := theme.Colors.Layout.Table.ZeroSize
	assert.Equal(t, stops[0].Color, color(snapshots[0], columnUsed))
	assert.Equal(t, stops[len(stops)-1].Color, color(snapshots[1], columnUsed))
	assert.Equal(t, zero, color(snapshots[2], columnUsed))
	// each column has its own scale
	assert.Equal(t, zero, color(snapshots[0], columnWritten))
	assert.Equal(t, stops[len(stops)-1].Color, color(snapshots[1], columnWritten))
	assert.Equal(t, stops[0].Color, color(snapshots[2], columnWritten))
}

// Held snapshots have a lock in front of their name; the name itself (sorting, filter) is unchanged.
func TestHeldSnapshotName(t *testing.T) {
	held := &zfs.Snapshot{Name: "daily-1", Properties: zfs.SnapshotProperties{Holds: 1}}
	free := &zfs.Snapshot{Name: "daily-2"}
	assert.Equal(t, "🔒 daily-1", formatName(held))
	assert.Equal(t, "daily-2", formatName(free))
	assert.True(t, snapshotMatchesFilter(&data.SnapshotBrowserEntry{Snapshot: held}, "daily-*"))

	// the lock is two cells wide, the columns behind it stay aligned
	tableView := tview.NewTable()
	tableView.SetCell(0, 0, tview.NewTableCell(formatName(held)))
	tableView.SetCell(0, 1, tview.NewTableCell("A"))
	tableView.SetCell(1, 0, tview.NewTableCell(formatName(free)))
	tableView.SetCell(1, 1, tview.NewTableCell("B"))
	screen := tcell.NewSimulationScreen("UTF-8")
	require.NoError(t, screen.Init())
	screen.SetSize(40, 2)
	tableView.SetRect(0, 0, 40, 2)
	tableView.Draw(screen)
	column := func(y int, r rune) int {
		for x := 0; x < 40; x++ {
			if char, _, _, _ := screen.GetContent(x, y); char == r {
				return x
			}
		}
		return -1
	}
	assert.Equal(t, column(0, 'A'), column(1, 'B'))
	assert.Positive(t, column(0, 'A'))
}
