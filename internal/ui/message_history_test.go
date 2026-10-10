package ui

import (
	"strings"
	"testing"
	"time"
	"zfs-file-history/internal/testutil"
	"zfs-file-history/internal/ui/dialog"
	"zfs-file-history/internal/ui/status_message"

	"github.com/gdamore/tcell/v2"
	"github.com/stretchr/testify/assert"
)

// m shows the messages and marks them as seen; a later key press dismisses an error from the status bar
func TestMessageHistoryKey(t *testing.T) {
	app, mainPage, datasetPage := createUi(t.TempDir(), true)
	screen := tcell.NewSimulationScreen("UTF-8")
	app.SetScreen(screen)
	screen.SetSize(200, 30)
	go func() { _ = app.Run() }()
	defer app.Stop()

	messages := mainPage.messages
	// once the datasets are loaded: without ZFS (e.g. in CI), loading them fails with an error message, which would
	// replace the one of this test and be counted as well
	assert.Eventually(t, func() bool {
		loading := true
		testutil.OnUiThread(t, app, func() { loading = datasetPage.datasetBrowser.IsLoading() })
		return !loading
	}, 10*time.Second, 20*time.Millisecond, "datasets loaded")
	testutil.OnUiThread(t, app, func() {
		messages.MarkRead()
		messages.Dismiss()
	})
	testutil.OnUiThread(t, app, func() { messages.Show(status_message.NewErrorStatusMessage("something failed")) })
	assert.Eventually(t, func() bool {
		text := screenText(t, app, screen)
		return strings.Contains(text, "something failed") && strings.Contains(text, "⚠ 1")
	}, 3*time.Second, 20*time.Millisecond, "the error and the badge are shown")

	frontPage := func() (name string) {
		testutil.OnUiThread(t, app, func() { name, _ = mainPage.pages.GetFrontPage() })
		return name
	}
	screen.InjectKey(tcell.KeyRune, 'm', tcell.ModNone)
	assert.Eventually(t, func() bool { return frontPage() == string(dialog.MessageHistoryDialogPage) },
		3*time.Second, 20*time.Millisecond)
	assert.Eventually(t, func() bool { return !strings.Contains(screenText(t, app, screen), "⚠ 1") },
		3*time.Second, 20*time.Millisecond, "the badge is gone once the messages were seen")

	screen.InjectKey(tcell.KeyEscape, 0, tcell.ModNone)
	assert.Eventually(t, func() bool { return frontPage() == string(Main) }, 3*time.Second, 20*time.Millisecond)

	// the key presses came too early to dismiss the error, the next one after a while does
	time.Sleep(status_message.MinKeyPressDisplayTime)
	screen.InjectKey(tcell.KeyDown, 0, tcell.ModNone)
	assert.Eventually(t, func() bool { return !strings.Contains(screenText(t, app, screen), "something failed") },
		3*time.Second, 20*time.Millisecond)
}
