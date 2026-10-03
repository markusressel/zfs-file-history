package file_browser

import (
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"
	"zfs-file-history/internal/configuration"
	"zfs-file-history/internal/data"
	"zfs-file-history/internal/testutil"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFormatFooter(t *testing.T) {
	assert.Equal(t, "42 entries", formatFooter(42, 42, false))
	assert.Equal(t, "", formatFooter(0, 0, false))
	assert.Equal(t, "5 of 42 entries", formatFooter(5, 42, true))
	assert.Equal(t, "0 of 42 entries", formatFooter(0, 42, true))
	assert.Equal(t, "1 entry", formatFooter(1, 1, false))
	assert.Equal(t, "0 of 1 entry", formatFooter(0, 1, true))
}

func TestFileMatchesFilter(t *testing.T) {
	entry := &data.FileBrowserEntry{Name: "Report-2026.TXT"}
	assert.True(t, fileMatchesFilter(entry, "report"))
	assert.True(t, fileMatchesFilter(entry, "*.txt"))
	assert.True(t, fileMatchesFilter(entry, "report-20??.*"))
	assert.False(t, fileMatchesFilter(entry, "*.log"))
}

// fileBrowserTest runs a file browser on a temporary directory with "a.txt", "b.txt", "c.log" and "sub/x.txt".
type fileBrowserTest struct {
	t           *testing.T
	app         *tview.Application
	screen      tcell.SimulationScreen
	fileBrowser *FileBrowserComponent
	dir         string
	sub         string
}

type fileBrowserState struct {
	visible []string
	footer  string
	filter  string
	path    string
}

func newFileBrowserTest(t *testing.T, filterOnDirectoryChange configuration.FileBrowserFilterOnDirectoryChange) *fileBrowserTest {
	originalConfig := configuration.CurrentConfig
	configuration.CurrentConfig.FileBrowser.FilterOnDirectoryChange = filterOnDirectoryChange
	t.Cleanup(func() { configuration.CurrentConfig = originalConfig })

	dir := t.TempDir()
	for _, name := range []string{"a.txt", "b.txt", "c.log"} {
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(name), 0o644))
	}
	sub := filepath.Join(dir, "sub")
	require.NoError(t, os.Mkdir(sub, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(sub, "x.txt"), []byte("x"), 0o644))

	app := tview.NewApplication()
	screen := tcell.NewSimulationScreen("UTF-8")
	app.SetScreen(screen)
	fileBrowser := NewFileBrowser(app)
	// set the root before the event loop starts, like CreateUi does
	app.SetRoot(fileBrowser.GetLayout(), true)
	go func() { _ = app.Run() }()
	t.Cleanup(app.Stop)

	return &fileBrowserTest{t: t, app: app, screen: screen, fileBrowser: fileBrowser, dir: dir, sub: sub}
}

func (ft *fileBrowserTest) state() (s fileBrowserState) {
	testutil.OnUiThread(ft.t, ft.app, func() {
		for _, entry := range ft.fileBrowser.tableContainer.GetEntries() {
			s.visible = append(s.visible, entry.Name)
		}
		sort.Strings(s.visible)
		s.footer = ft.fileBrowser.tableContainer.GetFooter()
		s.filter = ft.fileBrowser.tableContainer.GetFilterText()
		s.path = ft.fileBrowser.GetPath()
	})
	return s
}

func (ft *fileBrowserTest) waitFor(message string, condition func(s fileBrowserState) bool) fileBrowserState {
	var last fileBrowserState
	require.Eventually(ft.t, func() bool {
		last = ft.state()
		return condition(last)
	}, 5*time.Second, 20*time.Millisecond, message)
	return last
}

func (ft *fileBrowserTest) pressKey(key tcell.Key, r rune) {
	ft.screen.InjectKey(key, r, tcell.ModNone)
}

// openWithTxtFilter opens the temporary directory and filters it with "*.txt".
func (ft *fileBrowserTest) openWithTxtFilter() {
	testutil.OnUiThread(ft.t, ft.app, func() { ft.fileBrowser.SetPath(ft.dir, true) })
	s := ft.waitFor("directory loaded", func(s fileBrowserState) bool { return s.footer == "4 entries" })
	assert.Equal(ft.t, []string{"a.txt", "b.txt", "c.log", "sub"}, s.visible)

	ft.pressKey(tcell.KeyCtrlF, 0)
	for _, r := range "*.txt" {
		ft.pressKey(tcell.KeyRune, r)
	}
	s = ft.waitFor("filtered", func(s fileBrowserState) bool { return s.footer == "2 of 4 entries" })
	assert.Equal(ft.t, []string{"a.txt", "b.txt"}, s.visible)
}

func TestFileBrowser_Filter(t *testing.T) {
	ft := newFileBrowserTest(t, "")
	ft.openWithTxtFilter()

	// 'd' (delete) and 'h' (history) are typed into the filter, not executed
	ft.pressKey(tcell.KeyRune, 'd')
	ft.pressKey(tcell.KeyRune, 'h')
	ft.waitFor("typed into the filter", func(s fileBrowserState) bool { return s.filter == "*.txtdh" && s.footer == "0 of 4 entries" })
	ft.pressKey(tcell.KeyBackspace2, 0)
	ft.pressKey(tcell.KeyBackspace2, 0)
	ft.pressKey(tcell.KeyEnter, 0)
	ft.waitFor("filter kept", func(s fileBrowserState) bool { return s.filter == "*.txt" && s.footer == "2 of 4 entries" })
	for _, name := range []string{"a.txt", "b.txt", "c.log"} {
		assert.FileExists(t, filepath.Join(ft.dir, name))
	}

	// a reload of the same directory keeps the filter
	testutil.OnUiThread(t, ft.app, func() { ft.fileBrowser.Refresh(false) })
	time.Sleep(200 * time.Millisecond)
	s := ft.waitFor("filter kept after reload", func(s fileBrowserState) bool { return s.footer == "2 of 4 entries" })
	assert.Equal(t, "*.txt", s.filter)
}

func TestFileBrowser_FilterOnDirectoryChange(t *testing.T) {
	t.Run("keep (default)", func(t *testing.T) {
		ft := newFileBrowserTest(t, "")
		ft.openWithTxtFilter()
		ft.pressKey(tcell.KeyEnter, 0)

		testutil.OnUiThread(t, ft.app, func() { ft.fileBrowser.SetPath(ft.sub, true) })
		s := ft.waitFor("filter kept in the new directory", func(s fileBrowserState) bool {
			return s.path == ft.sub && s.footer == "1 of 1 entry"
		})
		assert.Equal(t, "*.txt", s.filter)
		assert.Equal(t, []string{"x.txt"}, s.visible)
	})

	t.Run("clear", func(t *testing.T) {
		ft := newFileBrowserTest(t, configuration.FileBrowserFilterOnDirectoryChangeClear)
		ft.openWithTxtFilter()
		ft.pressKey(tcell.KeyEnter, 0)

		testutil.OnUiThread(t, ft.app, func() { ft.fileBrowser.SetPath(ft.sub, true) })
		s := ft.waitFor("filter cleared in the new directory", func(s fileBrowserState) bool {
			return s.path == ft.sub && s.footer == "1 entry"
		})
		assert.Equal(t, "", s.filter)
		assert.Equal(t, []string{"x.txt"}, s.visible)
	})
}

// ctrl+Backspace and ctrl+Delete edit the filter word by word while typing, instead of triggering the file browser's
// shortcuts (Delete opens the delete dialog).
func TestFileBrowser_FilterDeleteWords(t *testing.T) {
	ft := newFileBrowserTest(t, "")
	ft.openWithTxtFilter()

	ft.screen.InjectKey(tcell.KeyBackspace2, 0, tcell.ModCtrl)
	ft.waitFor("word before the cursor deleted", func(s fileBrowserState) bool { return s.filter == "*." })

	ft.screen.InjectKey(tcell.KeyHome, 0, tcell.ModNone)
	for _, r := range "sub" {
		ft.pressKey(tcell.KeyRune, r)
	}
	ft.waitFor("typed at the start", func(s fileBrowserState) bool { return s.filter == "sub*." })
	ft.pressKey(tcell.KeyHome, 0)
	ft.screen.InjectKey(tcell.KeyDelete, 0, tcell.ModCtrl)
	s := ft.waitFor("word after the cursor deleted", func(s fileBrowserState) bool { return s.filter == "*." })
	assert.Equal(t, "0 of 4 entries", s.footer)
	for _, name := range []string{"a.txt", "b.txt", "c.log"} {
		assert.FileExists(t, filepath.Join(ft.dir, name))
	}
}
