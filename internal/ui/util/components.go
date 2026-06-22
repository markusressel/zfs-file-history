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
	TableComponentShortcutActions              = shortcut_helper.ShortcutEntry{KeyCombo: []string{"Enter"}, Name: "Actions"}
	TableComponentShortcutDelete               = shortcut_helper.ShortcutEntry{KeyCombo: []string{"Delete"}, Name: "Delete"}
	TableComponentShortcutColumns              = shortcut_helper.ShortcutEntry{KeyCombo: []string{"F2"}, Name: "Columns"}
	TableComponentShortcutUp                   = shortcut_helper.ShortcutEntry{KeyCombo: []string{"↑"}, Name: "Up"}
	TableComponentShortcutDown                 = shortcut_helper.ShortcutEntry{KeyCombo: []string{"↓"}, Name: "Down"}
	TableComponentShortcutPageUp               = shortcut_helper.ShortcutEntry{KeyCombo: []string{"PgUp"}, Name: "Page up"}
	TableComponentShortcutPageDown             = shortcut_helper.ShortcutEntry{KeyCombo: []string{"PgDn"}, Name: "Page down"}
	TableComponentShortcutFlipColumnDirection  = shortcut_helper.ShortcutEntry{KeyCombo: []string{"Enter"}, Name: "Flip Direction"}
	TableComponentShortcutCycleSortColumnLeft  = shortcut_helper.ShortcutEntry{KeyCombo: []string{"←"}, Name: "Cycle Sort Column Left"}
	TableComponentShortcutCycleSortColumnRight = shortcut_helper.ShortcutEntry{KeyCombo: []string{"→"}, Name: "Cycle Sort Column Right"}
)
