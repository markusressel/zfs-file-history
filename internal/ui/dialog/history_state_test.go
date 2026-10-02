package dialog

import (
	"path/filepath"
	"testing"
	"time"
	"zfs-file-history/internal/data"
	"zfs-file-history/internal/state"
	"zfs-file-history/internal/ui/table"
	"zfs-file-history/internal/zfs"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The keys identify the columns in the saved table layouts, see table.RowSelectionTable.BindColumnLayout.
func TestDialogColumnKeysAreUnique(t *testing.T) {
	for name, columns := range map[string][]*table.Column{
		"history": historyColumns, "timeline": timelineColumns, "changes": changeColumns, "properties": propertyColumns,
	} {
		keys := map[string]bool{}
		for _, column := range columns {
			assert.NotEmpty(t, column.Key, "%s: %s", name, column.Title)
			assert.NotEmpty(t, column.Title, "%s: shown in the column dialog", name)
			assert.False(t, keys[column.Key], "%s: duplicate key %q", name, column.Key)
			keys[column.Key] = true
		}
	}
}

// useTestState sets state.Current to a store in a temporary directory, for the duration of the test.
func useTestState(t *testing.T) *state.Store {
	store := state.Load(filepath.Join(t.TempDir(), "state.json"))
	state.Current = store
	t.Cleanup(func() {
		_ = store.Flush()
		state.Current = nil
	})
	return store
}

func TestFolderHistoryOverlay_ModeIsRemembered(t *testing.T) {
	store := useTestState(t)
	ft := newFolderHistoryTest(t)
	onUiThread(t, ft.app, func() { assert.Equal(t, diffModePredecessor, ft.overlay.mode, "the default") })

	ft.press(tcell.KeyRune, 'd')
	ft.waitFor("saved", func() bool { return store.Toggle(toggleFolderHistoryCompareNow, false) })

	// a new overlay starts in the remembered mode
	var mode diffMode
	onUiThread(t, ft.app, func() {
		mode = NewFolderHistoryOverlay(ft.app, filepath.Join(ft.ds.root, "docs"), nil).mode
	})
	assert.Equal(t, diffModeWorkingCopy, mode)
}

func TestFileHistoryOverlay_ModeIsRemembered(t *testing.T) {
	useTestState(t)
	ds := newFakeDataset(t)
	ds.addSnapshot("d1", day(1), map[string]string{"notes.txt": "one\n"})
	original := findSnapshotsOfPath
	t.Cleanup(func() { findSnapshotsOfPath = original })
	findSnapshotsOfPath = func(string, []*data.SnapshotBrowserEntry) ([]*zfs.Snapshot, error) { return ds.snapshots, nil }
	app := tview.NewApplication()
	file := &data.FileBrowserEntry{Name: "notes.txt", Type: data.File, RealFile: &data.RealFile{Name: "notes.txt", Path: filepath.Join(ds.root, "notes.txt")}}

	overlay := NewFileHistoryOverlay(app, file, nil)
	assert.Equal(t, diffModeWorkingCopy, overlay.currentDiffMode, "the default")
	overlay.toggleDiffMode()

	assert.Equal(t, diffModePredecessor, NewFileHistoryOverlay(app, file, nil).currentDiffMode)
}

func TestFolderHistoryOverlay_ColumnsAreEditableAndSaved(t *testing.T) {
	store := useTestState(t)
	ft := newFolderHistoryTest(t)
	ft.selectChange("b.txt")

	ft.press(tcell.KeyF2, 0)
	ft.waitFor("column dialog", func() bool { return ft.hasPage(string(ColumnSelectionDialogPage)) })
	// remove the first column ("±")
	ft.press(tcell.KeyDelete, 0)
	ft.waitFor("saved", func() bool {
		layout, ok := store.TableLayout(stateKeyFolderHistoryChanges)
		return ok && assert.ObjectsAreEqual([]string{"name", "size", "modified"}, layout.Columns)
	})
	ft.press(tcell.KeyEscape, 0)
	ft.waitFor("column dialog closed", func() bool { return !ft.hasPage(string(ColumnSelectionDialogPage)) })

	// a new overlay uses the saved columns
	var columns []*table.Column
	onUiThread(t, ft.app, func() {
		columns = NewFolderHistoryOverlay(ft.app, filepath.Join(ft.ds.root, "docs"), nil).changes.GetColumnSpec()
	})
	assert.Equal(t, []*table.Column{changeColumnName, changeColumnSize, changeColumnModified}, columns)
}

func TestDatasetPropertiesDialog_ColumnsAreEditable(t *testing.T) {
	store := useTestState(t)
	pt := newPropertiesTest(t, false)

	pt.press(tcell.KeyF2, 0)
	pt.waitFor("column dialog", func() bool { return pt.hasPage(string(ColumnSelectionDialogPage)) })
	pt.press(tcell.KeyDelete, 0)
	require.Eventually(t, func() bool {
		_, ok := store.TableLayout(stateKeyPropertiesTable)
		return ok
	}, 3*time.Second, 10*time.Millisecond)
}
