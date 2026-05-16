package snapshot_browser

import (
	"math"
	"testing"
	"zfs-file-history/internal/data"
	"zfs-file-history/internal/data/diff_state"
	"zfs-file-history/internal/ui/table"
	"zfs-file-history/internal/zfs"

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

	cells := createSnapshotBrowserTableCells(0, []*table.Column{columnClones}, entry)

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
