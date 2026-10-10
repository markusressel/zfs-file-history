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
	TableComponentShortcutActions = shortcut_helper.ShortcutEntry{KeyCombo: []string{shortcut_helper.KeyEnter}, Name: "Actions"}
	TableComponentShortcutDelete  = shortcut_helper.ShortcutEntry{KeyCombo: []string{shortcut_helper.KeyDelete}, Name: "Delete"}
	TableComponentShortcutColumns = shortcut_helper.ShortcutEntry{KeyCombo: []string{"F2"}, Name: "Columns", Group: shortcut_helper.GroupView}
	TableComponentShortcutFilter  = shortcut_helper.ShortcutEntry{KeyCombo: []string{"/", shortcut_helper.Ctrl("f")}, Name: "Filter", Group: shortcut_helper.GroupView}
	// TableComponentShortcutMove moves the selection (j/k/g/G work as well, like in Vim)
	TableComponentShortcutMove                 = shortcut_helper.ShortcutEntry{KeyCombo: []string{"↑", "↓", shortcut_helper.KeyPgUp, shortcut_helper.KeyPgDn}, Name: "Move", Group: shortcut_helper.GroupNavigation}
	TableComponentShortcutFlipColumnDirection  = shortcut_helper.ShortcutEntry{KeyCombo: []string{shortcut_helper.KeyEnter}, Name: "Flip Direction", Group: shortcut_helper.GroupView}
	TableComponentShortcutCycleSortColumnLeft  = shortcut_helper.ShortcutEntry{KeyCombo: []string{"←"}, Name: "Cycle Sort Column Left", Group: shortcut_helper.GroupView}
	TableComponentShortcutCycleSortColumnRight = shortcut_helper.ShortcutEntry{KeyCombo: []string{"→"}, Name: "Cycle Sort Column Right", Group: shortcut_helper.GroupView}
)
