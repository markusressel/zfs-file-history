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
	assert.Contains(t, string(content), `"version": 2`)
	assert.Contains(t, string(content), `"compatibleVersion": 2`)

	// only the state file and its lock are left, no temporary files
	entries, err := os.ReadDir(filepath.Dir(path))
	require.NoError(t, err)
	var names []string
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	assert.Equal(t, []string{"state.json", "state.json.lock"}, names)
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

// A file of a newer version that this one cannot write is read as far as possible, but never written, so its
// values are not lost.
func TestLoad_NewerVersionIsNotWritten(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	content := `{"version": 99, "tables": {"files": {"columns": ["name"]}}, "unknown": true}`
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))

	store := Load(path)
	loaded, ok := store.TableLayout("files")
	require.True(t, ok)
	assert.Equal(t, []string{"name"}, loaded.Columns)
	version, newer := store.NewerVersion()
	assert.True(t, newer)
	assert.Equal(t, 99, version)

	// changes work in memory, but are not saved
	store.SetToggle("tree", true)
	assert.True(t, store.Toggle("tree", false))
	require.NoError(t, store.Flush())
	written, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, content, string(written))
}

func TestLoad_UndecodableNewerVersionIsNotWritten(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	content := `{"version": 5, "compatibleVersion": 5, "namespaces": ["changed structure"]}`
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))

	store := Load(path)
	version, newer := store.NewerVersion()
	assert.True(t, newer)
	assert.Equal(t, 5, version)

	store.SetToggle("tree", true)
	require.NoError(t, store.Flush())
	written, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, content, string(written))
}

// A newer version that is compatible with this one (e.g. one with additional namespaces) is written, keeping its
// values and its version.
func TestLoad_CompatibleNewerVersionIsWritten(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	content := `{"version": 3, "compatibleVersion": 2, "namespaces": {"future": {"key": {"value": 1}}}}`
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))

	store := Load(path)
	_, newer := store.NewerVersion()
	assert.False(t, newer)
	store.SetToggle("tree", true)
	require.NoError(t, store.Flush())

	loaded := Load(path)
	assert.True(t, loaded.Toggle("tree", false))
	value, ok := Get[int](loaded, "future", "key")
	assert.True(t, ok)
	assert.Equal(t, 1, value)
	written, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Contains(t, string(written), `"version": 3`)
	assert.Contains(t, string(written), `"compatibleVersion": 2`)
}

// A newer version started meanwhile wrote the file: it is not overwritten.
func TestStore_DoesNotOverwriteNewerVersionWrittenMeanwhile(t *testing.T) {
	store, path := newTestStore(t)
	store.SetToggle("tree", true)
	require.NoError(t, store.Flush())
	_, newer := store.NewerVersion()
	require.False(t, newer)

	content := `{"version": 4, "compatibleVersion": 4, "namespaces": {}}`
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	store.SetToggle("tree", false)
	require.NoError(t, store.Flush())

	written, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, content, string(written))
	version, newer := store.NewerVersion()
	assert.True(t, newer)
	assert.Equal(t, 4, version)
}

func TestStore_Nil(t *testing.T) {
	var store *Store
	store.SetTableLayout("files", TableLayout{Columns: []string{"name"}})
	store.DeleteTableLayout("files")
	_, newer := store.NewerVersion()
	assert.False(t, newer)
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

func TestStore_Toggles(t *testing.T) {
	store, path := newTestStore(t)

	assert.True(t, store.Toggle("tree", true))
	assert.False(t, store.Toggle("tree", false))

	store.SetToggle("tree", false)
	store.SetToggle("hidden", true)
	assert.False(t, store.Toggle("tree", true))
	require.NoError(t, store.Flush())

	loaded := Load(path)
	assert.False(t, loaded.Toggle("tree", true))
	assert.True(t, loaded.Toggle("hidden", false))
	// table layouts and toggles are independent
	_, ok := loaded.TableLayout("tree")
	assert.False(t, ok)
}

func TestStore_SetToggleWithoutChangeDoesNotSave(t *testing.T) {
	store, path := newTestStore(t)
	store.SetToggle("tree", true)
	require.NoError(t, store.Flush())
	require.NoError(t, os.Remove(path))

	store.SetToggle("tree", true)
	require.NoError(t, store.Flush())
	assert.NoFileExists(t, path)
}

func TestStore_NilToggle(t *testing.T) {
	var store *Store
	store.SetToggle("tree", false)
	assert.True(t, store.Toggle("tree", true))
}

func TestLoad_MigratesVersion1(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	content := `{"version": 1, "tables": {"files": {"columns": ["name"], "sortColumn": "name"}}, "toggles": {"tree": false}}`
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))

	store := Load(path)
	layout, ok := store.TableLayout("files")
	require.True(t, ok)
	assert.Equal(t, TableLayout{Columns: []string{"name"}, SortColumn: "name"}, layout)
	assert.False(t, store.Toggle("tree", true))

	// written in the new format, with the old values
	store.SetToggle("hidden", true)
	require.NoError(t, store.Flush())
	written, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Contains(t, string(written), `"namespaces"`)
	assert.NotContains(t, string(written), "\n  \"tables\"")
	loaded := Load(path)
	_, ok = loaded.TableLayout("files")
	assert.True(t, ok)
	assert.False(t, loaded.Toggle("tree", true))
	assert.True(t, loaded.Toggle("hidden", false))
}

type testSelection struct {
	Name  string `json:"name"`
	Index int    `json:"index"`
}

func TestGetAndSet(t *testing.T) {
	store, path := newTestStore(t)

	_, ok := Get[testSelection](store, "selection", "/tank")
	assert.False(t, ok)

	Set(store, "selection", "/tank", testSelection{Name: "a", Index: 2})
	require.NoError(t, store.Flush())

	value, ok := Get[testSelection](Load(path), "selection", "/tank")
	require.True(t, ok)
	assert.Equal(t, testSelection{Name: "a", Index: 2}, value)

	// a value of another type is ignored
	_, ok = Get[int](store, "selection", "/tank")
	assert.False(t, ok)

	store.Delete("selection", "/tank")
	require.NoError(t, store.Flush())
	_, ok = Get[testSelection](Load(path), "selection", "/tank")
	assert.False(t, ok)
}

func TestGetAndSet_Nil(t *testing.T) {
	var store *Store
	Set(store, "selection", "/tank", 1)
	store.Delete("selection", "/tank")
	store.SetLimit("selection", 1)
	_, ok := Get[int](store, "selection", "/tank")
	assert.False(t, ok)
}

// newClock returns a clock for Store.now that advances by a second on every call.
func newClock() func() time.Time {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	return func() time.Time {
		now = now.Add(time.Second)
		return now
	}
}

func TestStore_SetLimitDropsLeastRecentlySaved(t *testing.T) {
	store, path := newTestStore(t)
	store.now = newClock()
	store.SetLimit("selection", 2)

	Set(store, "selection", "a", 1)
	Set(store, "selection", "b", 2)
	Set(store, "selection", "c", 3)
	_, ok := Get[int](store, "selection", "a")
	assert.False(t, ok)

	require.NoError(t, store.Flush())
	loaded := Load(path)
	_, ok = Get[int](loaded, "selection", "a")
	assert.False(t, ok)
	_, ok = Get[int](loaded, "selection", "b")
	assert.True(t, ok)
	_, ok = Get[int](loaded, "selection", "c")
	assert.True(t, ok)
}

func TestStore_InstancesKeepTheChangesOfEachOther(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	clock := newClock()
	first := Load(path)
	first.saveDelay, first.now = time.Hour, clock
	second := Load(path)
	second.saveDelay, second.now = time.Hour, clock

	first.SetTableLayout("files", TableLayout{Columns: []string{"name"}})
	first.SetToggle("tree", true)
	second.SetToggle("hidden", true)
	require.NoError(t, first.Flush())
	// the second instance started before the first one saved, it must not drop its values
	require.NoError(t, second.Flush())

	loaded := Load(path)
	_, ok := loaded.TableLayout("files")
	assert.True(t, ok)
	assert.True(t, loaded.Toggle("tree", false))
	assert.True(t, loaded.Toggle("hidden", false))

	// the most recent change wins, even if it was saved first
	second.SetToggle("tree", false)
	first.SetToggle("tree", true)
	first.SetToggle("tree", false)
	first.SetToggle("tree", true)
	require.NoError(t, first.Flush())
	require.NoError(t, second.Flush())
	assert.True(t, Load(path).Toggle("tree", false))

	// a deletion is kept as well
	first.DeleteTableLayout("files")
	require.NoError(t, first.Flush())
	second.SetToggle("other", true)
	require.NoError(t, second.Flush())
	_, ok = Load(path).TableLayout("files")
	assert.False(t, ok)

	// a value saved by another instance can be deleted, once it is known
	third := Load(path)
	third.saveDelay, third.now = time.Hour, clock
	third.SetToggle("hidden", false)
	third.Delete(namespaceToggles, "other")
	require.NoError(t, third.Flush())
	loaded = Load(path)
	assert.False(t, loaded.Toggle("hidden", true))
	assert.False(t, loaded.Toggle("other", false))
}
