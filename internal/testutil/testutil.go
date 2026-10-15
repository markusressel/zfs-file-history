// Package testutil contains helpers shared by the tests of several packages.
package testutil

import (
	"io/fs"
	"sync"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

func init() {
	// Like CreateUi: tview's double clicks are disabled in the application, otherwise a second click within the
	// interval (e.g. the next click of a test) is a double click, which most primitives ignore.
	tview.DoubleClickInterval = 0
}

// appConfig is the configuration of NewApp, changed by its options.
type appConfig struct {
	width, height int
}

// AppOption changes how NewApp creates the application, e.g. WithSize.
type AppOption func(*appConfig)

// WithSize sets the size of the screen of NewApp (80x25 without).
func WithSize(width int, height int) AppOption {
	return func(config *appConfig) { config.width, config.height = width, height }
}

// NewApp returns an application for a test, set up like CreateUi sets up the application: on a simulation screen
// (initialized, 80x25 unless WithSize), with the mouse enabled, and without double clicks (see init). Tests create
// their applications with it (TestAppsAreCreatedWithNewApp), so they do not differ from the application, or from
// each other, in what they forgot to set up. Run it with StartApp (or RunApp).
func NewApp(options ...AppOption) (*tview.Application, tcell.SimulationScreen) {
	app := tview.NewApplication().EnableMouse(true)
	return app, UseSimulationScreen(app, options...)
}

// UseSimulationScreen gives the app a simulation screen (80x25 unless WithSize) and returns it, e.g. for the
// application of CreateUi, which is set up like the one of NewApp otherwise. Must be called before the app runs.
func UseSimulationScreen(app *tview.Application, options ...AppOption) tcell.SimulationScreen {
	config := appConfig{width: 80, height: 25}
	for _, option := range options {
		option(&config)
	}
	screen := tcell.NewSimulationScreen("UTF-8")
	// initializes the screen, which resets its size
	app.SetScreen(screen)
	screen.SetSize(config.width, config.height)
	return screen
}

// App returns the application of NewApp without its screen, for a test that only creates a component with it (e.g.
// passes it to a constructor).
func App(options ...AppOption) *tview.Application {
	app, _ := NewApp(options...)
	return app
}

// StartApp sets the root of the app and runs it until the test ends (see RunApp). The root is set before the event
// loop starts, like CreateUi does: set later, it raced with the first draw.
func StartApp(t testing.TB, app *tview.Application, root tview.Primitive) (stop func()) {
	t.Helper()
	app.SetRoot(root, true)
	return RunApp(t, app)
}

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
	stop = runningApps.add(t, stop)
	t.Cleanup(stop)
	return stop
}

// runningApps are the stop functions of the apps of each test (see RunApp), which Replace calls before it restores a
// variable: the app could still read it otherwise.
var runningApps = &appStops{stops: map[testing.TB]map[*func()]bool{}}

type appStops struct {
	mutex sync.Mutex
	// stops are guarded by mutex
	stops map[testing.TB]map[*func()]bool
}

// add adds the stop function of an app of the test, and returns it, removing itself once called: every app is
// stopped when its test ends, so nothing is left.
func (apps *appStops) add(t testing.TB, stop func()) func() {
	apps.mutex.Lock()
	defer apps.mutex.Unlock()
	var remove func()
	remove = func() {
		stop()
		apps.mutex.Lock()
		defer apps.mutex.Unlock()
		delete(apps.stops[t], &remove)
		if len(apps.stops[t]) == 0 {
			delete(apps.stops, t)
		}
	}
	if apps.stops[t] == nil {
		apps.stops[t] = map[*func()]bool{}
	}
	apps.stops[t][&remove] = true
	return remove
}

// stopAll stops the apps of the test that still run, and waits until they stopped.
func (apps *appStops) stopAll(t testing.TB) {
	apps.mutex.Lock()
	var stops []func()
	for stop := range apps.stops[t] {
		stops = append(stops, *stop)
	}
	apps.mutex.Unlock()
	for _, stop := range stops {
		stop()
	}
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
