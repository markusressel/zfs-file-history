package file_browser

import (
	"os"
	"testing"
	"zfs-file-history/internal/configuration"
	"zfs-file-history/internal/testutil"

	"github.com/stretchr/testify/assert"
)

// a folder that cannot be read is entered, and shows why its entries are missing in their place, until a folder
// that can be read is shown
func TestFileBrowser_ShowsErrorOfUnreadableFolderInPlaceOfEntries(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root can read any folder")
	}
	ft := newFileBrowserTest(t, configuration.FileBrowserFilterOnDirectoryChangeClear)
	if err := os.Chmod(ft.sub, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(ft.sub, 0o755) })
	placeholder := func() (text string) {
		testutil.OnUiThread(t, ft.app, func() { text = ft.fileBrowser.tableContainer.GetPlaceholder() })
		return text
	}

	testutil.OnUiThread(t, ft.app, func() { ft.fileBrowser.SetPath(ft.dir, true) })
	ft.waitFor("directory loaded", func(s fileBrowserState) bool { return s.footer == "4 entries" })
	assert.Empty(t, placeholder())

	testutil.OnUiThread(t, ft.app, func() { ft.fileBrowser.SetPath(ft.sub, true) })
	s := ft.waitFor("unreadable folder entered", func(s fileBrowserState) bool { return s.path == ft.sub && len(s.visible) == 0 })
	assert.Empty(t, s.visible, "the entries of the former folder are not shown")
	assert.Contains(t, placeholder(), "permission denied")

	testutil.OnUiThread(t, ft.app, func() { ft.fileBrowser.goUp() })
	ft.waitFor("parent loaded", func(s fileBrowserState) bool { return s.path == ft.dir && s.footer == "4 entries" })
	assert.Empty(t, placeholder())
}
