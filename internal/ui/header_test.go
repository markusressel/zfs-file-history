package ui

import (
	"strings"
	"testing"
	"time"
	"zfs-file-history/internal/ui/shortcut_helper"
	"zfs-file-history/internal/ui/status_message"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestApplicationHeader_TimedStatusIsCleared(t *testing.T) {
	app := tview.NewApplication()
	app.SetScreen(tcell.NewSimulationScreen("UTF-8"))
	header := NewApplicationHeader(app)
	app.SetRoot(header.layout, true)
	go func() { _ = app.Run() }()
	t.Cleanup(app.Stop)

	statusText := func() string {
		var text string
		onUiThreadT(t, app, func() { text = header.statusTextView.GetText(true) })
		return text
	}

	timed := status_message.NewSuccessStatusMessage("created")
	timed.Duration = 30 * time.Millisecond
	onUiThreadT(t, app, func() { header.SetStatus(timed) })
	assert.Equal(t, "created", statusText())
	assert.Eventually(t, func() bool { return statusText() == "" }, 2*time.Second, 10*time.Millisecond)

	// a newer message is not cleared by the timer of an older one
	onUiThreadT(t, app, func() { header.SetStatus(timed) })
	permanent := status_message.NewErrorStatusMessage("failed")
	permanent.Duration = 0
	onUiThreadT(t, app, func() { header.SetStatus(permanent) })
	time.Sleep(100 * time.Millisecond)
	assert.Equal(t, "failed", statusText())
}

// The hint how to show the shortcuts again is only shown while they are hidden.
func TestApplicationHeader_ShortcutHint(t *testing.T) {
	header := NewApplicationHeader(tview.NewApplication())
	t.Cleanup(func() {
		if shortcut_helper.ShortcutsHidden() {
			shortcut_helper.ToggleShortcuts()
		}
	})
	screen := tcell.NewSimulationScreen("UTF-8")
	require.NoError(t, screen.Init())
	screen.SetSize(120, 3)
	draw := func() string {
		screen.Clear()
		header.layout.SetRect(0, 0, 120, 1)
		header.layout.Draw(screen)
		screen.Show()
		cells, width, _ := screen.GetContents()
		var line strings.Builder
		for _, cell := range cells[:width] {
			if len(cell.Runes) > 0 {
				line.WriteRune(cell.Runes[0])
			}
		}
		return line.String()
	}

	assert.NotContains(t, draw(), shortcutHintText)
	shortcut_helper.ToggleShortcuts()
	header.UpdateShortcutHint()
	assert.Contains(t, draw(), shortcutHintText)
	shortcut_helper.ToggleShortcuts()
	header.UpdateShortcutHint()
	assert.NotContains(t, draw(), shortcutHintText)
}
