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
	assert.Equal(t, tcell.ColorGreen, msg.Color)
	assert.Equal(t, StatusMessageDurationInfinite, msg.Duration)

	msg = NewErrorStatusMessage("error")
	assert.Equal(t, tcell.ColorRed, msg.Color)

	msg = NewWarningStatusMessage("warning")
	assert.Equal(t, tcell.ColorYellow, msg.Color)

	msg = NewInfoStatusMessage("info")
	assert.Equal(t, tcell.ColorLightGray, msg.Color)
}

func TestStatusMessageSetters(t *testing.T) {
	msg := NewInfoStatusMessage("info")
	msg.SetDuration(5 * time.Second).SetColor(tcell.ColorBlue)

	assert.Equal(t, 5*time.Second, msg.Duration)
	assert.Equal(t, tcell.ColorBlue, msg.Color)
}
