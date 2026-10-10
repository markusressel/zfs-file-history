package file_browser

import (
	"os"
	"testing"
	"time"
	"zfs-file-history/internal/configuration"
	"zfs-file-history/internal/testutil"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// a folder that cannot be read is not entered: its entries could not be shown, but the path would change
func TestFileBrowser_DoesNotEnterUnreadableFolder(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root can read any folder")
	}
	ft := newFileBrowserTest(t, configuration.FileBrowserFilterOnDirectoryChangeClear)
	require.NoError(t, os.Chmod(ft.sub, 0o000))
	t.Cleanup(func() { _ = os.Chmod(ft.sub, 0o755) })

	testutil.OnUiThread(t, ft.app, func() { ft.fileBrowser.SetPath(ft.dir, true) })
	ft.waitFor("directory loaded", func(s fileBrowserState) bool { return s.footer == "4 entries" })

	testutil.OnUiThread(t, ft.app, func() { ft.fileBrowser.SetPath(ft.sub, true) })
	time.Sleep(200 * time.Millisecond)
	s := ft.state()
	assert.Equal(t, ft.dir, s.path)
	assert.Equal(t, []string{"a.txt", "b.txt", "c.log", "sub"}, s.visible)
}
