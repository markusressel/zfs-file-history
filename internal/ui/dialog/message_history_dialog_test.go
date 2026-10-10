package dialog

import (
	"strings"
	"testing"
	"time"
	"zfs-file-history/internal/ui/status_message"

	"github.com/stretchr/testify/assert"
)

func TestFormatMessageHistory(t *testing.T) {
	assert.Equal(t, "No messages yet.", formatMessageHistory(nil))

	failed := status_message.NewErrorStatusMessage("open /x: permission [denied]")
	failed.Time = time.Now()
	created := status_message.NewSuccessStatusMessage("created")
	created.Time = time.Now()

	lines := strings.Split(formatMessageHistory([]*status_message.StatusMessage{failed, created}), "\n")
	assert.Len(t, lines, 2, "one line per message, in the given order")
	assert.Contains(t, lines[0], "✖ open /x: permission [denied[]", "the text is escaped")
	assert.Contains(t, lines[1], "✔ created")
}
