package file_browser

import (
	"testing"
	"time"
	"zfs-file-history/internal/data"
	"zfs-file-history/internal/testutil"

	"github.com/gdamore/tcell/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// h opens the history of the selected entry, or of the folder that is shown while the header row is selected.
// There is no separate key for the current folder (it used to be H).
func TestFileBrowser_HistoryKey(t *testing.T) {
	ft := newFileBrowserTest(t, "")
	testutil.OnUiThread(t, ft.app, func() { ft.fileBrowser.SetPath(ft.dir, true) })
	ft.waitFor("directory loaded", func(s fileBrowserState) bool { return s.footer == "4 entries" })

	requested := make(chan *data.FileBrowserEntry, 10)
	ft.fileBrowser.Events.Subscribe(func(event Event) {
		if e, ok := event.(RequestFileHistoryEvent); ok {
			requested <- e.FileEntry
		}
	})
	historyOf := func(key rune) *data.FileBrowserEntry {
		ft.pressKey(tcell.KeyRune, key)
		select {
		case entry := <-requested:
			return entry
		case <-time.After(time.Second):
			return nil
		}
	}

	// a data row: the selected entry
	var selected string
	testutil.OnUiThread(t, ft.app, func() {
		ft.fileBrowser.tableContainer.SelectFirstIfExists()
		selected = ft.fileBrowser.GetSelection().GetRealPath()
	})
	entry := historyOf('h')
	require.NotNil(t, entry)
	assert.Equal(t, selected, entry.GetRealPath())

	// the header row: the folder that is shown
	testutil.OnUiThread(t, ft.app, func() { ft.fileBrowser.tableContainer.SelectHeader() })
	entry = historyOf('h')
	require.NotNil(t, entry)
	assert.Equal(t, data.Directory, entry.Type)
	assert.Equal(t, ft.dir, entry.GetRealPath())

	var shortcuts []string
	testutil.OnUiThread(t, ft.app, func() {
		for _, shortcut := range ft.fileBrowser.GetShortcutMap() {
			shortcuts = append(shortcuts, shortcut.KeyCombo[0]+" "+shortcut.Name)
		}
	})
	assert.Contains(t, shortcuts, "h Folder history")

	assert.Nil(t, historyOf('H'), "H is not a shortcut anymore")
}
