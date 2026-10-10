package status_message

import (
	"time"

	"github.com/gdamore/tcell/v2"
)

// Level is the severity of a message. It decides its color and how long it is shown in the status bar.
type Level int

const (
	LevelInfo Level = iota
	LevelSuccess
	LevelWarning
	LevelError
)

const (
	// DefaultDuration is how long info and success messages are shown in the status bar.
	DefaultDuration = 4 * time.Second
	// UntilKeyPress shows a message in the status bar until the next key press (warnings and errors), see
	// Center.DismissOnKeyPress.
	UntilKeyPress time.Duration = 0
)

type StatusMessage struct {
	Message string
	Level   Level
	// Duration is how long the message is shown in the status bar, or UntilKeyPress
	Duration time.Duration
	// Time is when the message was shown, set by Center.Show
	Time time.Time
}

func newStatusMessage(message string, level Level, duration time.Duration) *StatusMessage {
	return &StatusMessage{
		Message:  message,
		Level:    level,
		Duration: duration,
	}
}

func (statusMessage *StatusMessage) SetDuration(duration time.Duration) *StatusMessage {
	statusMessage.Duration = duration
	return statusMessage
}

// Color returns the color of the message, by its level.
func (statusMessage *StatusMessage) Color() tcell.Color {
	return statusMessage.Level.Color()
}

// Color returns the color of messages of the level.
func (level Level) Color() tcell.Color {
	switch level {
	case LevelSuccess:
		return tcell.ColorGreen
	case LevelWarning:
		return tcell.ColorYellow
	case LevelError:
		return tcell.ColorRed
	default:
		return tcell.ColorLightGray
	}
}

// Icon returns a symbol for messages of the level, e.g. for lists of messages.
func (level Level) Icon() string {
	switch level {
	case LevelSuccess:
		return "✔"
	case LevelWarning:
		return "⚠"
	case LevelError:
		return "✖"
	default:
		return "ℹ"
	}
}

// isProblem returns whether messages of the level are warnings or errors, which are counted until they were seen in
// the history, see Center.UnreadCount.
func (level Level) isProblem() bool {
	return level >= LevelWarning
}

func NewSuccessStatusMessage(message string) *StatusMessage {
	return newStatusMessage(message, LevelSuccess, DefaultDuration)
}

func NewErrorStatusMessage(message string) *StatusMessage {
	return newStatusMessage(message, LevelError, UntilKeyPress)
}

func NewWarningStatusMessage(message string) *StatusMessage {
	return newStatusMessage(message, LevelWarning, UntilKeyPress)
}

func NewInfoStatusMessage(message string) *StatusMessage {
	return newStatusMessage(message, LevelInfo, DefaultDuration)
}
