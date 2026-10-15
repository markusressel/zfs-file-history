package ui

import (
	"testing"
	"zfs-file-history/internal/testutil"
	"zfs-file-history/internal/ui/shortcut_helper"
	"zfs-file-history/internal/ui/status_message"

	"github.com/stretchr/testify/assert"
)

func TestApplicationHeader_ShowsMessages(t *testing.T) {
	app, _ := testutil.NewApp()
	messages := status_message.NewCenter(app)
	header := NewApplicationHeader(app, messages)
	testutil.StartApp(t, app, header.layout)

	texts := func() (status string, badge string) {
		testutil.OnUiThread(t, app, func() {
			status = header.statusTextView.GetText(true)
			badge = header.unreadBadge.GetText(true)
		})
		return status, badge
	}

	testutil.OnUiThread(t, app, func() { messages.Show(status_message.NewSuccessStatusMessage("created")) })
	status, badge := texts()
	assert.Equal(t, "created", status)
	assert.Empty(t, badge, "only warnings and errors are counted")

	testutil.OnUiThread(t, app, func() {
		messages.Show(status_message.NewWarningStatusMessage("careful"))
		messages.Show(status_message.NewErrorStatusMessage("failed"))
	})
	status, badge = texts()
	assert.Equal(t, "failed", status)
	assert.Equal(t, "⚠ 2", badge)

	testutil.OnUiThread(t, app, func() {
		messages.Dismiss()
		messages.MarkRead()
	})
	status, badge = texts()
	assert.Empty(t, status)
	assert.Empty(t, badge)
}

// The hint how to show the shortcuts again is only shown while they are hidden.
func TestApplicationHeader_ShortcutHint(t *testing.T) {
	header := NewApplicationHeader(testutil.App(), status_message.NewCenter(testutil.App()))
	t.Cleanup(func() {
		if shortcut_helper.ShortcutsHidden() {
			shortcut_helper.ToggleShortcuts()
		}
	})
	screen := testutil.NewScreen(t, 120, 3)
	draw := func() string {
		screen.Clear()
		header.layout.SetRect(0, 0, 120, 1)
		header.layout.Draw(screen)
		return testutil.ScreenLines(screen)[0]
	}

	assert.NotContains(t, draw(), shortcutHintText)
	shortcut_helper.ToggleShortcuts()
	header.UpdateShortcutHint()
	assert.Contains(t, draw(), shortcutHintText)
	shortcut_helper.ToggleShortcuts()
	header.UpdateShortcutHint()
	assert.NotContains(t, draw(), shortcutHintText)
}
