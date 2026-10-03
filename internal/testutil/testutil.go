// Package testutil contains helpers shared by the tests of several packages.
package testutil

import (
	"io/fs"
	"testing"
	"time"

	"github.com/rivo/tview"
)

// OnUiThread runs f on the UI thread of the running app and waits for it, failing the test instead of hanging on
// a deadlock. Widgets and the simulation screen are written by the event loop, so tests read them this way.
func OnUiThread(t *testing.T, app *tview.Application, f func()) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		app.QueueUpdate(f)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for the UI thread (deadlock?)")
	}
}

// IsOnUiThread returns whether it is called on the UI thread of the running app: an update queued from here does
// not run while the event loop is busy with the caller. Takes a moment if it is.
func IsOnUiThread(app *tview.Application) bool {
	done := make(chan struct{})
	go func() {
		app.QueueUpdate(func() {})
		close(done)
	}()
	select {
	case <-done:
		return false
	case <-time.After(200 * time.Millisecond):
		return true
	}
}

// StripTags returns text without its tview style tags, as it is shown.
func StripTags(text string) string {
	return tview.NewTextView().SetDynamicColors(true).SetText(text).GetText(true)
}

// File returns the info of a file as returned by os.Lstat.
func File(size int64, modTime time.Time) fs.FileInfo {
	return fileInfo{size: size, modTime: modTime}
}

// Folder returns the info of a folder with the given number of items as returned by os.Lstat on ZFS, where the size
// of a directory is its number of entries (plus "." and "..").
func Folder(items int, modTime time.Time) fs.FileInfo {
	return fileInfo{size: int64(items) + 2, modTime: modTime, dir: true}
}

type fileInfo struct {
	size    int64
	modTime time.Time
	dir     bool
}

func (f fileInfo) Name() string       { return "x" }
func (f fileInfo) Size() int64        { return f.size }
func (f fileInfo) ModTime() time.Time { return f.modTime }
func (f fileInfo) IsDir() bool        { return f.dir }
func (f fileInfo) Sys() any           { return nil }
func (f fileInfo) Mode() fs.FileMode {
	if f.dir {
		return fs.ModeDir | 0o755
	}
	return 0o644
}
