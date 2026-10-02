package state

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/mitchellh/go-homedir"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestStore(t *testing.T) (*Store, string) {
	path := filepath.Join(t.TempDir(), "sub", "state.json")
	store := Load(path)
	// long enough to never fire during a test, unless the test waits for it
	store.saveDelay = time.Hour
	return store, path
}

func TestLoad_MissingFile(t *testing.T) {
	store, path := newTestStore(t)

	_, ok := store.TableLayout("files")
	assert.False(t, ok)
	// nothing is written without changes
	require.NoError(t, store.Flush())
	assert.NoFileExists(t, path)
}

func TestStore_SaveAndLoad(t *testing.T) {
	store, path := newTestStore(t)
	layout := TableLayout{Columns: []string{"name", "size"}, SortColumn: "size", SortInverted: true}

	store.SetTableLayout("files", layout)
	store.SetTableLayout("other", TableLayout{Columns: []string{"a"}})
	store.DeleteTableLayout("other")
	require.NoError(t, store.Flush())

	loaded, ok := Load(path).TableLayout("files")
	require.True(t, ok)
	assert.Equal(t, layout, loaded)
	_, ok = Load(path).TableLayout("other")
	assert.False(t, ok)

	content, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Contains(t, string(content), `"version": 1`)

	// only the state file is left, no temporary files
	entries, err := os.ReadDir(filepath.Dir(path))
	require.NoError(t, err)
	require.Len(t, entries, 1)
	assert.Equal(t, "state.json", entries[0].Name())
}

func TestStore_SavesInBackground(t *testing.T) {
	store, path := newTestStore(t)
	store.saveDelay = 10 * time.Millisecond

	store.SetTableLayout("files", TableLayout{Columns: []string{"name"}})

	assert.Eventually(t, func() bool {
		_, ok := Load(path).TableLayout("files")
		return ok
	}, 2*time.Second, 10*time.Millisecond)
}

func TestStore_ReturnsCopies(t *testing.T) {
	store, _ := newTestStore(t)
	columns := []string{"name", "size"}
	store.SetTableLayout("files", TableLayout{Columns: columns})
	columns[0] = "changed"

	loaded, _ := store.TableLayout("files")
	assert.Equal(t, []string{"name", "size"}, loaded.Columns)
	loaded.Columns[0] = "changed"

	loaded, _ = store.TableLayout("files")
	assert.Equal(t, []string{"name", "size"}, loaded.Columns)
}

func TestLoad_CorruptFileStartsEmptyAndIsReplaced(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	require.NoError(t, os.WriteFile(path, []byte("{not json"), 0o600))

	store := Load(path)
	_, ok := store.TableLayout("files")
	assert.False(t, ok)

	store.SetTableLayout("files", TableLayout{Columns: []string{"name"}})
	require.NoError(t, store.Flush())
	_, ok = Load(path).TableLayout("files")
	assert.True(t, ok)
}

func TestLoad_KeepsKnownValuesOfNewerVersion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	content := `{"version": 99, "tables": {"files": {"columns": ["name"]}}, "unknown": true}`
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))

	loaded, ok := Load(path).TableLayout("files")
	require.True(t, ok)
	assert.Equal(t, []string{"name"}, loaded.Columns)
}

func TestStore_Nil(t *testing.T) {
	var store *Store
	store.SetTableLayout("files", TableLayout{Columns: []string{"name"}})
	store.DeleteTableLayout("files")
	_, ok := store.TableLayout("files")
	assert.False(t, ok)
	assert.NoError(t, store.Flush())
}

func TestStore_FlushErrorKeepsChanges(t *testing.T) {
	dir := t.TempDir()
	// the parent "directory" of the state file is a file, so writing fails
	blocker := filepath.Join(dir, "blocker")
	require.NoError(t, os.WriteFile(blocker, nil, 0o600))
	store := Load(filepath.Join(blocker, "state.json"))
	store.saveDelay = time.Hour

	store.SetTableLayout("files", TableLayout{Columns: []string{"name"}})
	assert.Error(t, store.Flush())

	// the change is written once writing works again
	require.NoError(t, os.Remove(blocker))
	require.NoError(t, store.Flush())
	_, ok := Load(filepath.Join(blocker, "state.json")).TableLayout("files")
	assert.True(t, ok)
}

func TestDefaultPath(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", "/custom/state")
	path, err := DefaultPath()
	require.NoError(t, err)
	assert.Equal(t, "/custom/state/zfs-file-history/state.json", path)

	// relative paths are invalid according to the XDG spec, so the default is used
	t.Setenv("XDG_STATE_HOME", "relative")
	t.Setenv("HOME", "/home/test")
	// go-homedir caches the home directory
	homedir.Reset()
	t.Cleanup(homedir.Reset)
	path, err = DefaultPath()
	require.NoError(t, err)
	assert.Equal(t, "/home/test/.local/state/zfs-file-history/state.json", path)
}
