package snapshot_browser

import (
	"strings"
	"testing"
	"time"
	"zfs-file-history/internal/data"
	"zfs-file-history/internal/testutil"

	"github.com/gdamore/tcell/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// historyRequests sets a history target and collects the RequestHistoryEvents of the browser.
func historyRequests(dt *destroyTest, target *data.FileBrowserEntry) chan RequestHistoryEvent {
	requests := make(chan RequestHistoryEvent, 10)
	testutil.OnUiThread(dt.t, dt.app, func() {
		dt.browser.SetHistoryTarget(func() *data.FileBrowserEntry { return target })
		dt.browser.Events.Subscribe(func(event Event) {
			if e, ok := event.(RequestHistoryEvent); ok {
				requests <- e
			}
		})
	})
	return requests
}

func receiveHistoryRequest(t *testing.T, requests chan RequestHistoryEvent) RequestHistoryEvent {
	select {
	case request := <-requests:
		return request
	case <-time.After(2 * time.Second):
		t.Fatal("no RequestHistoryEvent")
		return RequestHistoryEvent{}
	}
}

// The history of the file or folder of the file browser, at the selected snapshot: from the action dialog or with h.
func TestSnapshotBrowser_HistoryAtSnapshot(t *testing.T) {
	dt := newDestroyTest(t)
	target := &data.FileBrowserEntry{Name: "notes.txt", Type: data.File}
	requests := historyRequests(dt, target)

	// the first action of the dialog
	dt.press(tcell.KeyEnter, 0)
	dt.waitForDialog("SnapshotActionDialog")
	require.Eventually(t, func() bool {
		return strings.Contains(dt.screenText(), "History of 'notes.txt' at this snapshot")
	}, 2*time.Second, 10*time.Millisecond)
	dt.press(tcell.KeyRune, '1')
	dt.press(tcell.KeyEnter, 0)
	request := receiveHistoryRequest(t, requests)
	assert.Same(t, target, request.Entry)
	assert.Equal(t, "daily-1", request.Snapshot.Snapshot.Name)

	// h
	require.Eventually(t, func() bool { return !strings.Contains(dt.screenText(), "History of") }, 2*time.Second, 10*time.Millisecond)
	dt.press(tcell.KeyRune, 'h')
	request = receiveHistoryRequest(t, requests)
	assert.Same(t, target, request.Entry)
	assert.Equal(t, "daily-1", request.Snapshot.Snapshot.Name)

	var shortcuts []string
	testutil.OnUiThread(t, dt.app, func() {
		for _, shortcut := range dt.browser.GetShortcutMap() {
			shortcuts = append(shortcuts, shortcut.KeyCombo[0]+" "+shortcut.Name)
		}
	})
	assert.Contains(t, shortcuts, "h History at snapshot")
}

// Without a file browser (the dataset page), there is no history action.
func TestSnapshotBrowser_NoHistoryWithoutTarget(t *testing.T) {
	dt := newDestroyTest(t)

	dt.press(tcell.KeyEnter, 0)
	dt.waitForDialog("SnapshotActionDialog")
	time.Sleep(100 * time.Millisecond)
	assert.NotContains(t, dt.screenText(), "History of")

	var shortcuts []string
	testutil.OnUiThread(t, dt.app, func() {
		for _, shortcut := range dt.browser.GetShortcutMap() {
			shortcuts = append(shortcuts, shortcut.Name)
		}
	})
	assert.NotContains(t, shortcuts, "History at snapshot")
}
