package status_message

import (
	"time"

	"github.com/rivo/tview"
)

const (
	// MaxHistory is the number of messages kept in the history, the oldest ones are dropped.
	MaxHistory = 200
	// MinKeyPressDisplayTime is how long a message is shown at least before a key press dismisses it, so a held key
	// (e.g. while scrolling) does not dismiss it before it can be read.
	MinKeyPressDisplayTime = 1 * time.Second
)

// Center is the one place messages of the whole application are shown: the current one in the status bar, all of
// them in the history. A message is shown until a newer one is, or until its lifetime ends (see
// StatusMessage.Duration). Warnings and errors are counted until the history was seen (see UnreadCount).
//
// All methods must be called on the UI thread.
type Center struct {
	application *tview.Application
	// now returns the current time, replaceable in tests
	now func() time.Time

	current *StatusMessage
	// currentSeq identifies the current message, so the timer of a former one does not clear a newer one (even if
	// the same message is shown again)
	currentSeq uint64
	// history is ordered from oldest to newest
	history []shownMessage
	// shownCount numbers the shown messages, readCount is the shownCount when the history was last seen
	shownCount uint64
	readCount  uint64

	listeners []func()
}

func NewCenter(application *tview.Application) *Center {
	return &Center{
		application: application,
		now:         time.Now,
	}
}

// OnChanged registers a listener that is called (on the UI thread) when the current message, the history or the
// number of unread messages changed.
func (center *Center) OnChanged(listener func()) {
	center.listeners = append(center.listeners, listener)
}

// shownMessage is a message in the history, numbered by Center.shownCount.
type shownMessage struct {
	*StatusMessage
	number uint64
}

// Show shows the message in the status bar and adds it to the history.
func (center *Center) Show(message *StatusMessage) {
	message.Time = center.now()
	center.shownCount++
	center.history = append(center.history, shownMessage{message, center.shownCount})
	if len(center.history) > MaxHistory {
		center.history = center.history[len(center.history)-MaxHistory:]
	}

	center.current = message
	center.currentSeq++
	if message.Duration > 0 {
		seq := center.currentSeq
		// the timer runs on its own goroutine, so it hands the clearing over to the UI thread
		time.AfterFunc(message.Duration, func() {
			center.application.QueueUpdateDraw(func() {
				if center.currentSeq == seq {
					center.Dismiss()
				}
			})
		})
	}
	center.notify()
}

// Current returns the message shown in the status bar, or nil.
func (center *Center) Current() *StatusMessage {
	return center.current
}

// Dismiss removes the current message from the status bar. It stays in the history.
func (center *Center) Dismiss() {
	if center.current == nil {
		return
	}
	center.current = nil
	center.currentSeq++
	center.notify()
}

// DismissOnKeyPress dismisses the current message if it is shown until a key is pressed, and has been shown for at
// least MinKeyPressDisplayTime. Call it for each key press, before the key is handled, so a message shown because
// of that key is not dismissed by it.
func (center *Center) DismissOnKeyPress() {
	if center.current == nil || center.current.Duration != UntilKeyPress {
		return
	}
	if center.now().Sub(center.current.Time) < MinKeyPressDisplayTime {
		return
	}
	center.Dismiss()
}

// History returns all kept messages, the newest first.
func (center *Center) History() []*StatusMessage {
	result := make([]*StatusMessage, len(center.history))
	for i, message := range center.history {
		result[len(center.history)-1-i] = message.StatusMessage
	}
	return result
}

// UnreadCount returns the number of warnings and errors shown since the history was seen (see MarkRead), and the
// highest level among them.
func (center *Center) UnreadCount() (count int, highest Level) {
	highest = LevelInfo
	for _, message := range center.history {
		if message.number > center.readCount && message.Level.isProblem() {
			count++
			highest = max(highest, message.Level)
		}
	}
	return count, highest
}

// MarkRead marks all messages as seen, e.g. once the history is shown.
func (center *Center) MarkRead() {
	if center.readCount == center.shownCount {
		return
	}
	center.readCount = center.shownCount
	center.notify()
}

func (center *Center) notify() {
	for _, listener := range center.listeners {
		listener()
	}
}
