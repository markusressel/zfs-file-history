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
	TableComponentShortcutActions              = shortcut_helper.ShortcutEntry{KeyCombo: []string{shortcut_helper.KeyEnter}, Name: "Actions"}
	TableComponentShortcutDelete               = shortcut_helper.ShortcutEntry{KeyCombo: []string{shortcut_helper.KeyDelete}, Name: "Delete"}
	TableComponentShortcutColumns              = shortcut_helper.ShortcutEntry{KeyCombo: []string{"F2"}, Name: "Columns"}
	TableComponentShortcutFilter               = shortcut_helper.ShortcutEntry{KeyCombo: []string{shortcut_helper.Ctrl("f")}, Name: "Filter"}
	TableComponentShortcutUp                   = shortcut_helper.ShortcutEntry{KeyCombo: []string{"↑"}, Name: "Up"}
	TableComponentShortcutDown                 = shortcut_helper.ShortcutEntry{KeyCombo: []string{"↓"}, Name: "Down"}
	TableComponentShortcutPageUp               = shortcut_helper.ShortcutEntry{KeyCombo: []string{shortcut_helper.KeyPgUp}, Name: "Page up"}
	TableComponentShortcutPageDown             = shortcut_helper.ShortcutEntry{KeyCombo: []string{shortcut_helper.KeyPgDn}, Name: "Page down"}
	TableComponentShortcutFlipColumnDirection  = shortcut_helper.ShortcutEntry{KeyCombo: []string{shortcut_helper.KeyEnter}, Name: "Flip Direction"}
	TableComponentShortcutCycleSortColumnLeft  = shortcut_helper.ShortcutEntry{KeyCombo: []string{"←"}, Name: "Cycle Sort Column Left"}
	TableComponentShortcutCycleSortColumnRight = shortcut_helper.ShortcutEntry{KeyCombo: []string{"→"}, Name: "Cycle Sort Column Right"}
)
