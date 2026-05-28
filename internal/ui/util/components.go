package util

import (
	"fmt"
	"zfs-file-history/internal/ui/shortcut_helper"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

type Page string

func CreateAttentionText(text string) string {
	return fmt.Sprintf("  %s  ", text)
}

func CreateAttentionTextView(text string) *tview.TextView {
	abortText := CreateAttentionText(text)
	return tview.NewTextView().SetText(abortText).SetTextColor(tcell.ColorYellow).SetTextAlign(tview.AlignRight)
}

var (
	TableComponentShortcutDown = shortcut_helper.ShortcutEntry{KeyCombo: []string{"↓"}, Name: "Down"}
	TableComponentShortcutUp   = shortcut_helper.ShortcutEntry{KeyCombo: []string{"↑"}, Name: "Up"}

	TableComponentShortcutEntries = []shortcut_helper.ShortcutEntry{
		TableComponentShortcutUp,
		TableComponentShortcutDown,
		{KeyCombo: []string{"Enter"}, Name: "Flip Direction"},
		{KeyCombo: []string{"←"}, Name: "Cycle Sort Column Left"},
		{KeyCombo: []string{"→"}, Name: "Cycle Sort Column Right"},
	}
)
