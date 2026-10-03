package file_browser

import (
	"os"
	"path/filepath"
	"testing"
	"zfs-file-history/internal/data"
	"zfs-file-history/internal/data/diff_state"
	"zfs-file-history/internal/ui/table"
	"zfs-file-history/internal/ui/theme"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Regression: entering a folder made the selection flash white (unknown states) before it got the color of the
// state, and the Diff column flash "N/A" before "=".
func TestFileBrowser_NoFlashingWhileStatesAreDetermined(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a.txt")
	require.NoError(t, os.WriteFile(path, []byte("a"), 0o644))
	stat, err := os.Lstat(path)
	require.NoError(t, err)
	fileBrowser := NewFileBrowser(tview.NewApplication())
	entry := func(state diff_state.DiffState, loading bool) *data.FileBrowserEntry {
		return &data.FileBrowserEntry{Name: "a.txt", Type: data.File, RealFile: &data.RealFile{Name: "a.txt", Path: path, Stat: stat},
			DiffState: state, IsLoading: loading}
	}
	diffCell := func(entry *data.FileBrowserEntry) *tview.TableCell {
		return fileBrowser.fileBrowserEntryTableCellsFunction(0, []*table.Column{columnDiff}, entry)[0]
	}
	selectedBackground := func(entry *data.FileBrowserEntry) tcell.Color {
		_, background, _ := diffCell(entry).SelectedStyle.Decompose()
		return background
	}

	loading := entry(diff_state.Unknown, true)
	assert.Equal(t, "", diffCell(loading).Text, "empty while it is determined")
	assert.Equal(t, selectedBackground(entry(diff_state.Equal, false)), selectedBackground(loading),
		"the selection keeps the color of equal entries, the most common result")
	assert.NotEqual(t, theme.Colors.Layout.Table.SelectedBackground, selectedBackground(loading))

	assert.Equal(t, "N/A", diffCell(entry(diff_state.Unknown, false)).Text, "cannot be determined, e.g. without a snapshot")
	assert.Equal(t, "=", diffCell(entry(diff_state.Equal, false)).Text)
	// a known state stays shown while it is determined again
	assert.Equal(t, "≠", diffCell(entry(diff_state.Modified, true)).Text)
}
