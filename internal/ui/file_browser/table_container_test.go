package file_browser

import (
	"testing"
	"zfs-file-history/internal/data"
	"zfs-file-history/internal/data/diff_state"
	"zfs-file-history/internal/ui/theme"

	"github.com/gdamore/tcell/v2"
	"github.com/stretchr/testify/assert"
)

func TestDetermineStatusIndicatorAndColor(t *testing.T) {
	entryAdded := &data.FileBrowserEntry{DiffState: diff_state.Added}
	assert.Equal(t, "+", determineStatusIndicator(entryAdded))
	assert.Equal(t, theme.Colors.FileBrowser.Table.State.Added, determineStatusColor(entryAdded))

	entryDeleted := &data.FileBrowserEntry{DiffState: diff_state.Deleted}
	assert.Equal(t, "-", determineStatusIndicator(entryDeleted))
	assert.Equal(t, theme.Colors.FileBrowser.Table.State.Deleted, determineStatusColor(entryDeleted))

	entryModified := &data.FileBrowserEntry{DiffState: diff_state.Modified}
	assert.Equal(t, "≠", determineStatusIndicator(entryModified))
	assert.Equal(t, theme.Colors.FileBrowser.Table.State.Modified, determineStatusColor(entryModified))

	entryEqual := &data.FileBrowserEntry{DiffState: diff_state.Equal}
	assert.Equal(t, "=", determineStatusIndicator(entryEqual))
	assert.Equal(t, theme.Colors.FileBrowser.Table.State.Equal, determineStatusColor(entryEqual))

	entryUnknown := &data.FileBrowserEntry{DiffState: diff_state.Unknown}
	assert.Equal(t, "N/A", determineStatusIndicator(entryUnknown))
	assert.Equal(t, theme.Colors.FileBrowser.Table.State.Unknown, determineStatusColor(entryUnknown))
}

func TestDetermineTypeCellTextAndColor(t *testing.T) {
	entryDir := &data.FileBrowserEntry{Type: data.Directory}
	assert.Equal(t, "D", determineTypeCellText(entryDir))
	assert.Equal(t, theme.Colors.Layout.Table.Accent, determineTypeCellColor(entryDir))

	entryFile := &data.FileBrowserEntry{Type: data.File}
	assert.Equal(t, "F", determineTypeCellText(entryFile))
	assert.Equal(t, tcell.ColorGray, determineTypeCellColor(entryFile))

	entryLink := &data.FileBrowserEntry{Type: data.Link}
	assert.Equal(t, "L", determineTypeCellText(entryLink))
	assert.Equal(t, tcell.ColorYellow, determineTypeCellColor(entryLink))

	entryOther := &data.FileBrowserEntry{Type: data.FileBrowserEntryType(99)}
	assert.Equal(t, "?", determineTypeCellText(entryOther))
}

func TestCompareUint32WithMissing(t *testing.T) {
	assert.Equal(t, 0, compareUint32WithMissing(0, false, 0, false))
	assert.Equal(t, -1, compareUint32WithMissing(0, false, 10, true))
	assert.Equal(t, 1, compareUint32WithMissing(10, true, 0, false))
	assert.Equal(t, -1, compareUint32WithMissing(5, true, 10, true))
	assert.Equal(t, 1, compareUint32WithMissing(10, true, 5, true))
	assert.Equal(t, 0, compareUint32WithMissing(10, true, 10, true))
}

func TestFileBrowserEntrySortFunction(t *testing.T) {
	e1 := &data.FileBrowserEntry{Name: "apple", Type: data.File, DiffState: diff_state.Added}
	e2 := &data.FileBrowserEntry{Name: "banana", Type: data.Directory, DiffState: diff_state.Deleted}
	e3 := &data.FileBrowserEntry{Name: "cherry", Type: data.Link, DiffState: diff_state.Modified}

	entries := []*data.FileBrowserEntry{e2, e3, e1}

	// Sort by name
	sorted := fileBrowserEntrySortFunction(entries, columnName, false)
	assert.Equal(t, "apple", sorted[0].Name)
	assert.Equal(t, "banana", sorted[1].Name)
	assert.Equal(t, "cherry", sorted[2].Name)

	// Sort by name inverted
	sortedInverted := fileBrowserEntrySortFunction(entries, columnName, true)
	assert.Equal(t, "cherry", sortedInverted[0].Name)
	assert.Equal(t, "banana", sortedInverted[1].Name)
	assert.Equal(t, "apple", sortedInverted[2].Name)

	// Sort by type
	sortedByType := fileBrowserEntrySortFunction(entries, columnType, false)
	assert.NotEmpty(t, sortedByType)

	// Sort by diff
	sortedByDiff := fileBrowserEntrySortFunction(entries, columnDiff, false)
	assert.NotEmpty(t, sortedByDiff)
}
