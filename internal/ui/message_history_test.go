package ui

import (
	"slices"
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
	app, mainPage, _ := newUi(t, t.TempDir())
	screen := testutil.UseSimulationScreen(app, testutil.WithSize(200, 30))
	testutil.RunApp(t, app)

	messages := mainPage.messages
	// Tests have no ZFS (see testSystem): loading the datasets and the pools fails with an error message each, which
	// would replace the one of this test and be counted as well. It waits for both, how long they take depends on the
	// load of the machine. It does not wait for a while without new messages: that took a second in every run.
	failedLoads := []string{"Could not load datasets", "Could not load pools"}
	assert.Eventually(t, func() bool {
		var history []*status_message.StatusMessage
		testutil.OnUiThread(t, app, func() { history = messages.History() })
		for _, failedLoad := range failedLoads {
			if !slices.ContainsFunc(history, func(message *status_message.StatusMessage) bool {
				return strings.HasPrefix(message.Message, failedLoad)
			}) {
				return false
			}
		}
		return true
	}, 15*time.Second, 20*time.Millisecond, "the loads failed")
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
	testutil.Press(t, app, screen, tcell.KeyRune, 'm')
	assert.Eventually(t, func() bool { return frontPage() == string(dialog.MessageHistoryDialogPage) },
		3*time.Second, 20*time.Millisecond)
	assert.Eventually(t, func() bool { return !strings.Contains(screenText(t, app, screen), "⚠ 1") },
		3*time.Second, 20*time.Millisecond, "the badge is gone once the messages were seen")

	testutil.Press(t, app, screen, tcell.KeyEscape, 0)
	assert.Eventually(t, func() bool { return frontPage() == string(Main) }, 3*time.Second, 20*time.Millisecond)

	// The key presses came too early to dismiss the error, the next one after a while does. The error is made older
	// instead of waiting for that while.
	testutil.OnUiThread(t, app, func() {
		current := messages.History()[0]
		current.Time = current.Time.Add(-status_message.MinKeyPressDisplayTime)
	})
	testutil.Press(t, app, screen, tcell.KeyDown, 0)
	testutil.WaitForNoText(t, app, screen, "something failed", 3*time.Second)
}
