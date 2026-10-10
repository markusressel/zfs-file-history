package status_message

import (
	"fmt"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newTestCenter returns a center of a running application, and a function that runs code on its UI thread.
func newTestCenter(t *testing.T) (*Center, func(func())) {
	app := tview.NewApplication()
	app.SetScreen(tcell.NewSimulationScreen("UTF-8"))
	app.SetRoot(tview.NewBox(), true)
	go func() { _ = app.Run() }()
	t.Cleanup(app.Stop)
	onUiThread := func(f func()) {
		done := make(chan struct{})
		app.QueueUpdate(func() {
			f()
			close(done)
		})
		<-done
	}
	return NewCenter(app), onUiThread
}

func currentText(center *Center, onUiThread func(func())) string {
	text := ""
	onUiThread(func() {
		if current := center.Current(); current != nil {
			text = current.Message
		}
	})
	return text
}

func TestCenter_TimedMessageIsDismissed(t *testing.T) {
	center, onUiThread := newTestCenter(t)
	onUiThread(func() { center.Show(NewSuccessStatusMessage("created").SetDuration(30 * time.Millisecond)) })
	assert.Equal(t, "created", currentText(center, onUiThread))
	assert.Eventually(t, func() bool { return currentText(center, onUiThread) == "" }, 2*time.Second, 10*time.Millisecond)
}

// several messages within the lifetime of one: the timer of a former message never dismisses a newer one
func TestCenter_TimerOfFormerMessageDoesNotDismissNewerOne(t *testing.T) {
	center, onUiThread := newTestCenter(t)

	short := NewInfoStatusMessage("first").SetDuration(30 * time.Millisecond)
	onUiThread(func() {
		center.Show(short)
		center.Show(NewInfoStatusMessage("second").SetDuration(time.Hour))
	})
	time.Sleep(100 * time.Millisecond)
	assert.Equal(t, "second", currentText(center, onUiThread))

	// also not if the same message is shown again
	onUiThread(func() { center.Show(short) })
	time.Sleep(10 * time.Millisecond)
	onUiThread(func() { center.Show(NewErrorStatusMessage("failed")) })
	time.Sleep(100 * time.Millisecond)
	assert.Equal(t, "failed", currentText(center, onUiThread))
}

func TestCenter_DismissOnKeyPress(t *testing.T) {
	center, onUiThread := newTestCenter(t)
	now := time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)
	center.now = func() time.Time { return now }

	onUiThread(func() { center.Show(NewErrorStatusMessage("failed")) })

	// e.g. a held key: not before it could be read
	now = now.Add(MinKeyPressDisplayTime / 2)
	onUiThread(center.DismissOnKeyPress)
	assert.Equal(t, "failed", currentText(center, onUiThread))

	now = now.Add(MinKeyPressDisplayTime)
	onUiThread(center.DismissOnKeyPress)
	assert.Equal(t, "", currentText(center, onUiThread))

	// timed messages are left to their timer
	onUiThread(func() { center.Show(NewInfoStatusMessage("info").SetDuration(time.Hour)) })
	now = now.Add(time.Minute)
	onUiThread(center.DismissOnKeyPress)
	assert.Equal(t, "info", currentText(center, onUiThread))
}

func TestCenter_History(t *testing.T) {
	center, onUiThread := newTestCenter(t)
	onUiThread(func() {
		for i := range MaxHistory + 5 {
			center.Show(NewInfoStatusMessage(fmt.Sprintf("message %d", i)))
		}
		// dismissed messages stay in the history
		center.Dismiss()
	})

	var history []*StatusMessage
	onUiThread(func() { history = center.History() })
	require.Len(t, history, MaxHistory, "the oldest ones are dropped")
	assert.Equal(t, fmt.Sprintf("message %d", MaxHistory+4), history[0].Message, "the newest first")
	assert.Equal(t, "message 5", history[len(history)-1].Message)
	assert.False(t, history[0].Time.IsZero())
}

func TestCenter_UnreadCount(t *testing.T) {
	center, onUiThread := newTestCenter(t)
	changes := 0
	center.OnChanged(func() { changes++ })

	unread := func() (count int, highest Level) {
		onUiThread(func() { count, highest = center.UnreadCount() })
		return count, highest
	}

	onUiThread(func() { center.Show(NewSuccessStatusMessage("created")) })
	count, _ := unread()
	assert.Zero(t, count, "only warnings and errors are counted")

	onUiThread(func() {
		center.Show(NewErrorStatusMessage("failed"))
		// infos in between do not hide former errors
		center.Show(NewInfoStatusMessage("info"))
		center.Show(NewWarningStatusMessage("careful"))
		center.Show(NewInfoStatusMessage("info"))
	})
	count, highest := unread()
	assert.Equal(t, 2, count)
	assert.Equal(t, LevelError, highest)

	onUiThread(center.MarkRead)
	count, _ = unread()
	assert.Zero(t, count)

	onUiThread(func() { center.Show(NewWarningStatusMessage("careful")) })
	count, highest = unread()
	assert.Equal(t, 1, count)
	assert.Equal(t, LevelWarning, highest)

	onUiThread(func() { assert.Equal(t, 7, changes) })
}
