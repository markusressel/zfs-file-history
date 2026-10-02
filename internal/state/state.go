// Package state persists what the application remembers between runs (e.g. table column layouts), as opposed to
// the user-written configuration in internal/configuration. The state is a small JSON file in $XDG_STATE_HOME,
// written by the application only.
package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"time"
	"zfs-file-history/internal/logging"

	"github.com/mitchellh/go-homedir"
)

// currentVersion is the version of the file format written by this version of the application.
const currentVersion = 1

// defaultSaveDelay debounces saving, so fast changes (e.g. cycling the sort column) are written once.
const defaultSaveDelay = 500 * time.Millisecond

// Current is the state of the running application, set up at startup by Init.
// While it is nil (e.g. in tests), nothing is restored or saved.
var Current *Store

// TableLayout is the user-configurable layout of a table.
type TableLayout struct {
	// Columns are the keys of the displayed columns, in display order.
	Columns []string `json:"columns"`
	// SortColumn is the key of the column the table is sorted by.
	SortColumn   string `json:"sortColumn"`
	SortInverted bool   `json:"sortInverted"`
}

// fileContent is the content of the state file.
type fileContent struct {
	Version int                    `json:"version"`
	Tables  map[string]TableLayout `json:"tables,omitempty"`
}

// Store holds the state in memory and saves it to its file in the background after every change.
// All methods are safe for concurrent use and never block on file access, except Flush.
// The methods of a nil *Store do nothing.
type Store struct {
	path      string
	saveDelay time.Duration

	mutex     sync.Mutex
	content   fileContent
	saveTimer *time.Timer
	dirty     bool

	// saveMutex serializes writing the file
	saveMutex sync.Mutex
}

// Init loads the state from the default path into Current. Errors are logged, the application then starts
// with an empty state.
func Init() {
	path, err := DefaultPath()
	if err != nil {
		logging.Error("Cannot determine the state file path, the state is not saved: %v", err)
		return
	}
	Current = Load(path)
}

// DefaultPath returns the path of the state file: $XDG_STATE_HOME/zfs-file-history/state.json,
// with $XDG_STATE_HOME defaulting to ~/.local/state.
func DefaultPath() (string, error) {
	stateHome := os.Getenv("XDG_STATE_HOME")
	if !filepath.IsAbs(stateHome) {
		// relative paths are invalid according to the XDG spec
		home, err := homedir.Dir()
		if err != nil {
			return "", err
		}
		stateHome = filepath.Join(home, ".local", "state")
	}
	return filepath.Join(stateHome, "zfs-file-history", "state.json"), nil
}

// Load reads the state file at path. A missing file results in an empty state. An unreadable file is logged and
// results in an empty state as well; it is overwritten with the next change.
func Load(path string) *Store {
	store := &Store{
		path:      path,
		saveDelay: defaultSaveDelay,
		content:   fileContent{Version: currentVersion},
	}

	content, err := readFile(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
	case err != nil:
		logging.Error("Cannot read state file, starting with an empty state: %v", err)
	default:
		store.content = content
	}
	if store.content.Tables == nil {
		store.content.Tables = map[string]TableLayout{}
	}
	return store
}

func readFile(path string) (fileContent, error) {
	var content fileContent
	data, err := os.ReadFile(path)
	if err != nil {
		return content, err
	}
	if err := json.Unmarshal(data, &content); err != nil {
		return content, fmt.Errorf("%s: %w", path, err)
	}
	if content.Version > currentVersion {
		logging.Warning("State file %s was written by a newer version (%d), unknown values are lost when saving",
			path, content.Version)
	}
	content.Version = currentVersion
	return content, nil
}

// TableLayout returns the saved layout of the table with the given key.
func (store *Store) TableLayout(key string) (TableLayout, bool) {
	if store == nil {
		return TableLayout{}, false
	}
	store.mutex.Lock()
	defer store.mutex.Unlock()
	layout, ok := store.content.Tables[key]
	if ok {
		layout.Columns = append([]string(nil), layout.Columns...)
	}
	return layout, ok
}

// SetTableLayout saves the layout of the table with the given key.
func (store *Store) SetTableLayout(key string, layout TableLayout) {
	if store == nil {
		return
	}
	store.mutex.Lock()
	defer store.mutex.Unlock()
	layout.Columns = append([]string(nil), layout.Columns...)
	store.content.Tables[key] = layout
	store.scheduleSave()
}

// DeleteTableLayout removes the saved layout of the table with the given key, so its default layout is used.
func (store *Store) DeleteTableLayout(key string) {
	if store == nil {
		return
	}
	store.mutex.Lock()
	defer store.mutex.Unlock()
	if _, ok := store.content.Tables[key]; !ok {
		return
	}
	delete(store.content.Tables, key)
	store.scheduleSave()
}

// scheduleSave saves the state in the background after saveDelay, restarting the delay on every change.
// Must be called with mutex held.
func (store *Store) scheduleSave() {
	store.dirty = true
	if store.saveTimer != nil {
		store.saveTimer.Stop()
	}
	store.saveTimer = time.AfterFunc(store.saveDelay, func() {
		if err := store.Flush(); err != nil {
			logging.Error("Cannot save state: %v", err)
		}
	})
}

// Flush writes pending changes to the file immediately. Called on shutdown, so the last changes are not lost.
// This accesses the file system, so it must not be called on the UI thread.
func (store *Store) Flush() error {
	if store == nil {
		return nil
	}
	store.saveMutex.Lock()
	defer store.saveMutex.Unlock()

	store.mutex.Lock()
	if store.saveTimer != nil {
		store.saveTimer.Stop()
		store.saveTimer = nil
	}
	if !store.dirty {
		store.mutex.Unlock()
		return nil
	}
	data, err := json.MarshalIndent(store.content, "", "  ")
	store.dirty = false
	store.mutex.Unlock()
	if err != nil {
		return err
	}

	err = writeFileAtomically(store.path, append(data, '\n'))
	if err != nil {
		// try again with the next change or flush
		store.mutex.Lock()
		store.dirty = true
		store.mutex.Unlock()
	}
	return err
}

// writeFileAtomically replaces the file at path with data, so readers (e.g. another running instance) never see
// a partially written file.
func writeFileAtomically(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tempFile, err := os.CreateTemp(dir, filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	tempPath := tempFile.Name()
	defer func() { _ = os.Remove(tempPath) }()

	if _, err := tempFile.Write(data); err != nil {
		_ = tempFile.Close()
		return err
	}
	if err := tempFile.Sync(); err != nil {
		_ = tempFile.Close()
		return err
	}
	if err := tempFile.Close(); err != nil {
		return err
	}
	return os.Rename(tempPath, path)
}
