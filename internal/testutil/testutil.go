// Package testutil contains helpers shared by the tests of several packages.
package testutil

import (
	"io/fs"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// RunApp runs the app in the background until the test ends, or until stop is called. Both stop it and wait until
// its event loop returned: tview.Application.Stop does not wait, so the handler of an event could still run while
// the cleanups of the test (or the next test) changed what it reads, a data race (e.g. on state.Current).
func RunApp(t testing.TB, app *tview.Application) (stop func()) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = app.Run()
	}()
	var once sync.Once
	stop = func() {
		once.Do(func() {
			app.Stop()
			select {
			case <-done:
			case <-time.After(5 * time.Second):
				t.Error("timed out waiting for the app to stop")
			}
		})
	}
	t.Cleanup(stop)
	return stop
}

// OnUiThread runs f on the UI thread of the running app and waits for it, failing the test instead of hanging on
// a deadlock. Widgets and the simulation screen are written by the event loop, so tests read them this way. It does
// not draw (tests poll with it, and a draw each time would slow down the events of the test): read the screen with
// ScreenText, which does.
func OnUiThread(t testing.TB, app *tview.Application, f func()) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		app.QueueUpdate(f)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(uiTimeout):
		t.Fatal("timed out waiting for the UI thread (deadlock?)")
	}
}

// uiTimeout is how long a test waits for the UI thread before it fails as a deadlock: far longer than it takes, also
// with -race on a busy machine (e.g. in CI), where the 2 seconds of before were not enough.
const uiTimeout = 10 * time.Second

// barrierKey and barrierModifiers are the key event of WaitForEvents, which no test sends otherwise.
const (
	barrierKey       = tcell.KeyF64
	barrierModifiers = tcell.ModShift | tcell.ModCtrl | tcell.ModAlt | tcell.ModMeta
)

// WaitForEvents waits until the app handled the events injected into its screen before (keys, mouse). The app handles
// them in its event loop, in order, but not in order with OnUiThread (another queue): a test that read the state
// right after injecting (or after a fixed time) read it before the event was handled on a slow machine. It sends a
// key event of its own after them and waits until the app handled it.
func WaitForEvents(t testing.TB, app *tview.Application, screen tcell.SimulationScreen) {
	t.Helper()
	handled := make(chan struct{})
	OnUiThread(t, app, func() {
		previous := app.GetInputCapture()
		app.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
			if event.Key() == barrierKey && event.Modifiers() == barrierModifiers {
				app.SetInputCapture(previous)
				close(handled)
				return nil
			}
			if previous != nil {
				return previous(event)
			}
			return event
		})
	})
	screen.InjectKey(barrierKey, 0, barrierModifiers)
	select {
	case <-handled:
	case <-time.After(uiTimeout):
		t.Fatal("timed out waiting for the app to handle the events (deadlock?)")
	}
}

// ScreenText draws the running app and returns the text on its screen, a line per row (a rune per cell, wide
// characters once). It draws first: a change made on the UI thread (e.g. with OnUiThread) is only shown once
// something draws, and a test must not depend on something else doing so in time (see TestScreenIsDrawnBeforeRead).
func ScreenText(t testing.TB, app *tview.Application, screen tcell.SimulationScreen) string {
	t.Helper()
	var text strings.Builder
	OnUiThread(t, app, func() {
		app.ForceDraw()
		cells, width, height := screen.GetContents()
		for y := 0; y < height; y++ {
			for x := 0; x < width; x++ {
				if runes := cells[y*width+x].Runes; len(runes) > 0 {
					text.WriteRune(runes[0])
				}
			}
			text.WriteRune('\n')
		}
	})
	return text.String()
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
