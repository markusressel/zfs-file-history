package dialog

import (
	"path/filepath"
	"testing"
	"time"
	"zfs-file-history/internal/data"
	"zfs-file-history/internal/state/statetest"
	"zfs-file-history/internal/testutil"
	"zfs-file-history/internal/ui/table"
	"zfs-file-history/internal/zfs"

	"github.com/gdamore/tcell/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The keys identify the columns in the saved table layouts, see table.RowSelectionTable.BindColumnLayout.
func TestDialogColumnKeysAreUnique(t *testing.T) {
	for name, columns := range map[string][]*table.Column{
		"history": historyColumns, "timeline": timelineColumns, "changes": changeColumns, "properties": propertyColumns,
		"pool history": poolHistoryColumns,
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

func TestFolderHistoryOverlay_ModeIsRemembered(t *testing.T) {
	store := statetest.Use(t)
	ft := newFolderHistoryTest(t)
	testutil.OnUiThread(t, ft.app, func() { assert.Equal(t, diffModePredecessor, ft.overlay.mode, "the default") })

	ft.press(tcell.KeyRune, 'd')
	ft.waitFor("saved", func() bool { return store.Toggle(toggleFolderHistoryCompareNow, false) })

	// a new overlay starts in the remembered mode
	var mode diffMode
	testutil.OnUiThread(t, ft.app, func() {
		mode = NewFolderHistoryOverlay(ft.app, filepath.Join(ft.ds.root, "docs"), nil).mode
	})
	assert.Equal(t, diffModeWorkingCopy, mode)
}

func TestFileHistoryOverlay_ModeIsRemembered(t *testing.T) {
	statetest.Use(t)
	ds := newFakeDataset(t)
	ds.addSnapshot("d1", day(1), map[string]string{"notes.txt": "one\n"})
	testutil.Replace(t, &findSnapshotsOfPath, func(string, []*data.SnapshotBrowserEntry) ([]*zfs.Snapshot, error) { return ds.snapshots, nil })
	app, _ := testutil.NewApp()
	file := &data.FileBrowserEntry{Name: "notes.txt", Type: data.File, RealFile: &data.RealFile{Name: "notes.txt", Path: filepath.Join(ds.root, "notes.txt")}}

	overlay := NewFileHistoryOverlay(app, file, nil)
	assert.Equal(t, diffModeWorkingCopy, overlay.currentDiffMode, "the default")
	overlay.toggleDiffMode()

	assert.Equal(t, diffModePredecessor, NewFileHistoryOverlay(app, file, nil).currentDiffMode)
}

func TestFolderHistoryOverlay_ColumnsAreEditableAndSaved(t *testing.T) {
	store := statetest.Use(t)
	ft := newFolderHistoryTest(t)
	ft.selectChange("b.txt")

	ft.press(tcell.KeyF2, 0)
	ft.waitFor("column dialog", func() bool { return ft.hasPage(string(ColumnSelectionDialogPage)) })
	// remove the first column ("±")
	ft.press(tcell.KeyEnter, 0)
	ft.waitFor("saved", func() bool {
		layout, ok := store.TableLayout(stateKeyFolderHistoryChanges)
		return ok && assert.ObjectsAreEqual([]string{"name", "size", "modified"}, layout.Columns)
	})
	ft.press(tcell.KeyEscape, 0)
	ft.waitFor("column dialog closed", func() bool { return !ft.hasPage(string(ColumnSelectionDialogPage)) })

	// a new overlay uses the saved columns
	var columns []*table.Column
	testutil.OnUiThread(t, ft.app, func() {
		columns = NewFolderHistoryOverlay(ft.app, filepath.Join(ft.ds.root, "docs"), nil).changes.GetColumnSpec()
	})
	assert.Equal(t, []*table.Column{changeColumnName, changeColumnSize, changeColumnModified}, columns)
}

func TestDatasetPropertiesDialog_ColumnsAreEditable(t *testing.T) {
	store := statetest.Use(t)
	pt := newPropertiesTest(t, false)

	pt.press(tcell.KeyF2, 0)
	pt.waitFor("column dialog", func() bool { return pt.hasPage(string(ColumnSelectionDialogPage)) })
	pt.press(tcell.KeyEnter, 0)
	require.Eventually(t, func() bool {
		_, ok := store.TableLayout(stateKeyPropertiesTable)
		return ok
	}, 3*time.Second, 10*time.Millisecond)
}

func TestVersionAt(t *testing.T) {
	type version struct{ created time.Time }
	created := func(v *version) time.Time { return v.created }
	d1, d3, d5 := &version{day(1)}, &version{day(3)}, &version{day(5)}
	versions := []*version{d5, d3, d1} // newest first, like the histories

	assert.Same(t, d3, versionAt(versions, created, day(3)), "the snapshot itself")
	assert.Same(t, d3, versionAt(versions, created, day(4)), "the last change before it")
	assert.Same(t, d5, versionAt(versions, created, day(9)))
	assert.Same(t, d1, versionAt(versions, created, day(0)), "older than all versions: the oldest")
	assert.Same(t, d1, versionAt([]*version{d1, d5, d3}, created, day(2)), "independent of the order")
	assert.Nil(t, versionAt([]*version{}, created, day(1)))
}
