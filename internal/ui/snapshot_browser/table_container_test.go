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
	assert.Equal(t, theme.Colors.SnapshotBrowser.Table.EmptySnapshot, foreground(empty))
	assert.NotEqual(t, theme.Colors.SnapshotBrowser.Table.EmptySnapshot, foreground(written))

	// beyond the range of int, where subtracting would overflow
	entries := []*data.SnapshotBrowserEntry{newEntry("small", 1), newEntry("none", 0), newEntry("huge", math.MaxUint64), newEntry("big", 1<<40)}
	createSnapshotBrowserTableSortFunction(entries, columnWritten, false)
	assert.Equal(t, []string{"huge", "big", "small", "none"}, snapshotNames(entries))
	createSnapshotBrowserTableSortFunction(entries, columnWritten, true)
	assert.Equal(t, []string{"none", "small", "big", "huge"}, snapshotNames(entries))
}
