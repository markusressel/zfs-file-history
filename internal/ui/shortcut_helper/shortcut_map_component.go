package shortcut_helper

import (
	"fmt"
	"zfs-file-history/internal/ui/txwidgets"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

type ShortcutEntry struct {
	KeyCombo []string
	Name     string
}

func NewShortcutEntry(keyCombo []string, name string) *ShortcutEntry {
	return &ShortcutEntry{
		KeyCombo: keyCombo,
		Name:     name,
	}
}

type ShortcutMapComponent struct {
	application *tview.Application

	layout                  *tview.Flex
	shortcutEntriesTextView *tview.TextView

	ShortCutEntries []ShortcutEntry
}

func NewShortcutMap(application *tview.Application) *ShortcutMapComponent {
	shortcutMap := &ShortcutMapComponent{
		application: application,
	}

	shortcutMap.createLayout()

	return shortcutMap
}

func (sm *ShortcutMapComponent) createLayout() {
	layout := tview.NewFlex().SetDirection(tview.FlexColumn)

	shortcutEntriesTextView := tview.NewTextView().
		SetDynamicColors(true)
	shortcutEntriesTextView.SetBorderPadding(0, 0, 1, 1)
	shortcutEntriesTextView.SetTextAlign(tview.AlignLeft)

	layout.AddItem(shortcutEntriesTextView, 0, 1, false)

	sm.shortcutEntriesTextView = shortcutEntriesTextView
	sm.layout = layout
}

func (sm *ShortcutMapComponent) SetEntries(entries []ShortcutEntry) {
	sm.ShortCutEntries = entries
	var statusText string
	for _, entry := range entries {
		shortcuts := txwidgets.Span(tcell.ColorYellow, "%s", entry.KeyCombo)
		name := txwidgets.Span(tcell.ColorWhite, entry.Name)
		statusText += fmt.Sprintf("%s: %s  ", shortcuts, name)
	}
	sm.shortcutEntriesTextView.SetText(statusText)
	sm.application.ForceDraw()
}

func (sm *ShortcutMapComponent) Clear() {
	sm.shortcutEntriesTextView.SetText("")
	sm.application.ForceDraw()
}

func (sm *ShortcutMapComponent) GetLayout() *tview.Flex {
	return sm.layout
}
