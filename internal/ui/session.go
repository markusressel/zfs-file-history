package ui

import (
	"os"
	"path/filepath"
	"slices"
	"zfs-file-history/internal/state"
	"zfs-file-history/internal/ui/util"
)

// Where the session is remembered in state.Current.
const (
	sessionNamespace = "session"
	sessionKey       = "last"
)

// Start is how the application is started.
type Start struct {
	// Path is the folder to show: the one given on the command line, or else the working directory
	Path string
	// LaunchDir is the working directory the application was started in
	LaunchDir string
	// PathGiven is true if Path was given on the command line, which always wins over the last session
	PathGiven bool
	// RestoreSession restores the last session (see session), if it was started in the same LaunchDir
	RestoreSession bool
}

// session is what the user was looking at when the application was used the last time, restored on the next start
// (see Start.RestoreSession). Of several running instances, the session of the last one used is remembered.
type session struct {
	// LaunchDir is the working directory the session was started in. The session is only restored when started in
	// the same one again, e.g. after quitting to run something else in the same shell.
	LaunchDir string `json:"launchDir"`
	// Path is the folder shown in the file browser
	Path string `json:"path,omitempty"`
	// Page is the page shown
	Page util.Page `json:"page,omitempty"`
	// Focus is the title of the focused component of Page, see basePage.componentTitles
	Focus string `json:"focus,omitempty"`
	// Dataset is the name of the dataset selected on the dataset page
	Dataset string `json:"dataset,omitempty"`
}

// sessionToRestore returns the last session saved in store, if it is to be restored for start, otherwise nil.
func sessionToRestore(store *state.Store, start Start) *session {
	if !start.RestoreSession || start.PathGiven {
		return nil
	}
	last, ok := state.Get[session](store, sessionNamespace, sessionKey)
	if !ok || last.LaunchDir != start.LaunchDir {
		return nil
	}
	return &last
}

// startPath returns the folder to show at start: the one of the restored session, or its closest existing parent,
// or else start.Path.
func (start Start) startPath(restored *session) string {
	if restored == nil || restored.Path == "" {
		return start.Path
	}
	for path := filepath.Clean(restored.Path); ; path = filepath.Dir(path) {
		if info, err := os.Stat(path); err == nil && info.IsDir() {
			return path
		}
		if path == filepath.Dir(path) {
			return start.Path
		}
	}
}

// sessionRecorder saves the session in a store whenever it changes, see session.
type sessionRecorder struct {
	store   *state.Store
	current session
}

// newSessionRecorder starts recording the session in store (nothing is recorded if it is nil): the restored one,
// if any, so its values not changed in this run are kept.
func newSessionRecorder(store *state.Store, launchDir string, restored *session) *sessionRecorder {
	recorder := &sessionRecorder{store: store, current: session{LaunchDir: launchDir}}
	if restored != nil {
		recorder.current = *restored
	}
	return recorder
}

func (recorder *sessionRecorder) update(change func(current *session)) {
	change(&recorder.current)
	state.Set(recorder.store, sessionNamespace, sessionKey, recorder.current)
}

func (recorder *sessionRecorder) setPath(path string) {
	recorder.update(func(current *session) { current.Path = path })
}

func (recorder *sessionRecorder) setPage(page util.Page) {
	recorder.update(func(current *session) {
		if current.Page != page {
			// the focus of the other page does not apply
			current.Focus = ""
		}
		current.Page = page
	})
}

func (recorder *sessionRecorder) setFocus(page util.Page, title string) {
	recorder.update(func(current *session) {
		current.Page = page
		current.Focus = title
	})
}

func (recorder *sessionRecorder) setDataset(name string) {
	recorder.update(func(current *session) { current.Dataset = name })
}

// restoreSession shows the page of the restored session and focuses its component. Must be called on the UI
// thread, after the root of the application is set.
func restoreSession(restored *session, pages map[util.Page]switchablePage, switchTo func(page util.Page)) {
	if restored == nil || !slices.Contains(switchablePages, restored.Page) {
		return
	}
	switchTo(restored.Page)
	pages[restored.Page].focusComponent(restored.Focus)
}
