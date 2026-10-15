package ui

import (
	"os"
	"path/filepath"
	"testing"
	"time"
	"zfs-file-history/internal/state"
	"zfs-file-history/internal/state/statetest"
	"zfs-file-history/internal/testutil"
	"zfs-file-history/internal/ui/util"

	"github.com/gdamore/tcell/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSessionToRestore(t *testing.T) {
	store := state.Load(filepath.Join(t.TempDir(), "state.json"))
	last := session{LaunchDir: "/home/user", Path: "/tank/data", Page: Dataset}
	start := Start{Path: "/home/user", LaunchDir: "/home/user", RestoreSession: true}

	assert.Nil(t, sessionToRestore(store, start), "nothing saved")
	assert.Nil(t, sessionToRestore(nil, start), "no state")

	state.Set(store, sessionNamespace, sessionKey, last)
	assert.Equal(t, &last, sessionToRestore(store, start))

	disabled := start
	disabled.RestoreSession = false
	assert.Nil(t, sessionToRestore(store, disabled))

	pathGiven := start
	pathGiven.PathGiven = true
	assert.Nil(t, sessionToRestore(store, pathGiven))

	elsewhere := start
	elsewhere.Path, elsewhere.LaunchDir = "/tmp", "/tmp"
	assert.Nil(t, sessionToRestore(store, elsewhere))
}

func TestStart_StartPath(t *testing.T) {
	dir := t.TempDir()
	existing := filepath.Join(dir, "existing")
	require.NoError(t, os.Mkdir(existing, 0o700))
	file := filepath.Join(existing, "file")
	require.NoError(t, os.WriteFile(file, nil, 0o600))
	start := Start{Path: "/start"}

	assert.Equal(t, "/start", start.startPath(nil))
	assert.Equal(t, "/start", start.startPath(&session{}))
	assert.Equal(t, existing, start.startPath(&session{Path: existing}))
	// the closest existing folder
	assert.Equal(t, existing, start.startPath(&session{Path: filepath.Join(existing, "removed", "deeper")}))
	assert.Equal(t, existing, start.startPath(&session{Path: file}))
}

func TestSessionRecorder(t *testing.T) {
	store := state.Load(filepath.Join(t.TempDir(), "state.json"))

	recorder := newSessionRecorder(store, "/home/user", nil)
	recorder.setPath("/tank")
	recorder.setFocus(Main, "Snapshots")
	recorder.setDataset("tank/data")
	recorder.setPool("tank")
	saved, _ := state.Get[session](store, sessionNamespace, sessionKey)
	assert.Equal(t, session{LaunchDir: "/home/user", Path: "/tank", Page: Main, Focus: "Snapshots", Dataset: "tank/data", Pool: "tank"}, saved)

	// the focus of the other page does not apply
	recorder.setPage(Dataset)
	saved, _ = state.Get[session](store, sessionNamespace, sessionKey)
	assert.Equal(t, Dataset, saved.Page)
	assert.Empty(t, saved.Focus)

	// the values of a restored session are kept
	restored := &session{LaunchDir: "/home/user", Path: "/tank", Dataset: "tank/data", Pool: "tank"}
	newSessionRecorder(store, "/home/user", restored).setPage(Main)
	saved, _ = state.Get[session](store, sessionNamespace, sessionKey)
	assert.Equal(t, session{LaunchDir: "/home/user", Path: "/tank", Page: Main, Dataset: "tank/data", Pool: "tank"}, saved)

	// without a store, nothing is recorded
	newSessionRecorder(nil, "/home/user", nil).setPath("/tank")
}

// The folder, the page and the focused component are restored when started in the same directory again.
func TestSessionIsRestoredOnTheNextStart(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "state.json")
	launchDir := t.TempDir()
	folder := filepath.Join(launchDir, "folder")
	require.NoError(t, os.Mkdir(folder, 0o700))
	start := Start{Path: launchDir, LaunchDir: launchDir, RestoreSession: true}

	store := statetest.UseFile(t, statePath)
	app, mainPage, datasetPage := newUiFor(t, start)
	screen := testutil.UseSimulationScreen(app, testutil.WithSize(150, 40))
	stop := testutil.RunApp(t, app)

	testutil.OnUiThread(t, app, func() { mainPage.fileBrowser.SetPath(folder, false) })
	testutil.Press(t, app, screen, tcell.KeyRune, '2')
	testutil.Press(t, app, screen, tcell.KeyTab, 0)
	assert.Eventually(t, func() bool {
		var focused bool
		testutil.OnUiThread(t, app, func() { focused = datasetPage.datasetInfo.HasFocus() })
		return focused
	}, 2*time.Second, 10*time.Millisecond)
	stop()
	require.NoError(t, store.Flush())

	// Returns the folder of the file browser, the page in front and the title of its focused component. The app of
	// the restart before is stopped first, like the application: it read state.Current while it was replaced.
	stopPrevious := func() {}
	restart := func(start Start) (string, util.Page, string) {
		stopPrevious()
		statetest.UseFile(t, statePath)
		app, mainPage, datasetPage := newUiFor(t, start)
		testutil.UseSimulationScreen(app)
		stopPrevious = testutil.RunApp(t, app)
		var path, focused string
		var front util.Page
		testutil.OnUiThread(t, app, func() {
			path = mainPage.fileBrowser.GetPath()
			name, _ := mainPage.pages.GetFrontPage()
			front = util.Page(name)
			page := map[util.Page]*basePage{Main: &mainPage.basePage, Dataset: &datasetPage.basePage}[front]
			focused = page.componentTitles[page.focusedComponent()]
		})
		return path, front, focused
	}

	path, front, focused := restart(start)
	assert.Equal(t, folder, path)
	assert.Equal(t, Dataset, front)
	assert.Equal(t, "Dataset info", focused)

	// started elsewhere: not restored, and the session of this start is the one remembered
	elsewhere := t.TempDir()
	path, front, focused = restart(Start{Path: elsewhere, LaunchDir: elsewhere, RestoreSession: true})
	assert.Equal(t, elsewhere, path)
	assert.Equal(t, Main, front)
	assert.Equal(t, "Files", focused)
	require.NoError(t, state.Current.Flush())
	path, _, _ = restart(start)
	assert.Equal(t, launchDir, path)
}

// A state file of a newer version is not overwritten, and the user is told to update.
func TestNewerStateVersionIsShown(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "state.json")
	require.NoError(t, os.WriteFile(statePath, []byte(`{"version": 7, "compatibleVersion": 7}`), 0o600))
	statetest.UseFile(t, statePath)

	app, mainPage, _ := newUi(t, t.TempDir())
	testutil.UseSimulationScreen(app)
	testutil.RunApp(t, app)
	var history []string
	testutil.OnUiThread(t, app, func() {
		for _, message := range mainPage.messages.History() {
			history = append(history, message.Message)
		}
	})
	assert.Contains(t, history, newerStateVersionMessage(7).Message)
}
