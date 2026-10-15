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
	screen := testutil.UseSimulationScreen(app, testutil.WithSize(200, 30))
	testutil.RunApp(t, app)

	messages := mainPage.messages
	// once the datasets are loaded: tests have no ZFS (see testSystem), so loading them fails with an error message,
	// which would replace the one of this test and be counted as well
	assert.Eventually(t, func() bool {
		loading := true
		testutil.OnUiThread(t, app, func() { loading = datasetPage.datasetBrowser.IsLoading() })
		return !loading
	}, 10*time.Second, 20*time.Millisecond, "datasets loaded")
	// Other loads that started with the UI can fail later than the datasets (the pools, the file browser, ...), how
	// much later depends on the load of the machine. Their messages would replace the one of this test, so it waits
	// until no new one came for a while.
	newest := func() (count int, message *status_message.StatusMessage) {
		testutil.OnUiThread(t, app, func() {
			history := messages.History()
			count = len(history)
			if count > 0 {
				message = history[0]
			}
		})
		return count, message
	}
	lastCount, lastMessage := newest()
	lastChange := time.Now()
	assert.Eventually(t, func() bool {
		if count, message := newest(); count != lastCount || message != lastMessage {
			lastCount, lastMessage, lastChange = count, message, time.Now()
		}
		return time.Since(lastChange) >= time.Second
	}, 15*time.Second, 20*time.Millisecond, "no new messages")
	testutil.OnUiThread(t, app, func() {
		messages.MarkRead()
		messages.Dismiss()
	})
	testutil.OnUiThread(t, app, func() { messages.Show(status_message.NewErrorStatusMessage("something failed")) })
	if !assert.Eventually(t, func() bool {
		text := screenText(t, app, screen)
		return strings.Contains(text, "something failed") && strings.Contains(text, "⚠ 1")
	}, 3*time.Second, 20*time.Millisecond, "the error and the badge are shown") {
		// It logs the messages and the screen, to see what came in between.
		testutil.OnUiThread(t, app, func() {
			for _, message := range messages.History() {
				t.Logf("message: %s", message.Message)
			}
		})
		t.Logf("screen:\n%s", screenText(t, app, screen))
	}

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
