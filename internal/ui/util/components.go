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
	TableComponentShortcutEntries = []shortcut_helper.ShortcutEntry{
		{KeyCombo: []string{"Enter"}, Name: "Toggle Sort Direction"},
		{KeyCombo: []string{"Left"}, Name: "Cycle Sort Column Left"},
		{KeyCombo: []string{"Right"}, Name: "Cycle Sort Column Right"},
	}
)
