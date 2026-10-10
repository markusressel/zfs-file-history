package snapshot_browser

import (
	"os"
	"path/filepath"
	"testing"
	"time"
	"zfs-file-history/internal/testutil"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"github.com/stretchr/testify/require"
)

// a folder of the dataset that cannot be read (its dataset cannot be found) shows no snapshots, but must not keep
// the snapshots of the dataset from being shown again
func TestSnapshotBrowser_ShowsSnapshotsAgainAfterUnreadableFolder(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root can read any folder")
	}
	root := t.TempDir()
	for _, name := range []string{"s1", "s2"} {
		require.NoError(t, os.MkdirAll(filepath.Join(root, ".zfs", "snapshot", name), 0o755))
	}
	locked := filepath.Join(root, "locked")
	require.NoError(t, os.Mkdir(locked, 0o000))
	t.Cleanup(func() { _ = os.Chmod(locked, 0o755) })

	app := tview.NewApplication()
	app.SetScreen(tcell.NewSimulationScreen("UTF-8"))
	browser := NewSnapshotBrowser(app)
	app.SetRoot(browser.GetLayout(), true)
	go func() { _ = app.Run() }()
	t.Cleanup(app.Stop)

	snapshotCount := func() int {
		var count int
		testutil.OnUiThread(t, app, func() { count = len(browser.GetCurrentSnapshots()) })
		return count
	}
	setPath := func(path string, expectedCount int) {
		testutil.OnUiThread(t, app, func() { browser.SetPath(path, false) })
		require.Eventually(t, func() bool { return snapshotCount() == expectedCount }, 5*time.Second, 20*time.Millisecond,
			"%d snapshots for %s", expectedCount, path)
	}

	setPath(root, 2)
	setPath(locked, 0)
	setPath(root, 2)
}
