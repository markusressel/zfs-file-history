package status_message

import (
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/stretchr/testify/assert"
)

func TestStatusMessageConstructors(t *testing.T) {
	msg := NewSuccessStatusMessage("success")
	assert.Equal(t, "success", msg.Message)
	assert.Equal(t, LevelSuccess, msg.Level)
	assert.Equal(t, tcell.ColorGreen, msg.Color())
	assert.Equal(t, DefaultDuration, msg.Duration)

	msg = NewInfoStatusMessage("info")
	assert.Equal(t, tcell.ColorLightGray, msg.Color())
	assert.Equal(t, DefaultDuration, msg.Duration)

	// shown until a key is pressed
	msg = NewWarningStatusMessage("warning")
	assert.Equal(t, tcell.ColorYellow, msg.Color())
	assert.Equal(t, UntilKeyPress, msg.Duration)

	msg = NewErrorStatusMessage("error")
	assert.Equal(t, tcell.ColorRed, msg.Color())
	assert.Equal(t, UntilKeyPress, msg.Duration)
}

func TestStatusMessageSetDuration(t *testing.T) {
	msg := NewInfoStatusMessage("info").SetDuration(5 * time.Second)
	assert.Equal(t, 5*time.Second, msg.Duration)
}
