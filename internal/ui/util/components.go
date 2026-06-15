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
	TableComponentShortcutDown                 = shortcut_helper.ShortcutEntry{KeyCombo: []string{"↓"}, Name: "Down"}
	TableComponentShortcutUp                   = shortcut_helper.ShortcutEntry{KeyCombo: []string{"↑"}, Name: "Up"}
	TableComponentShortcutFlipColumnDirection  = shortcut_helper.ShortcutEntry{KeyCombo: []string{"Enter"}, Name: "Flip Direction"}
	TableComponentShortcutCycleSortColumnLeft  = shortcut_helper.ShortcutEntry{KeyCombo: []string{"←"}, Name: "Cycle Sort Column Left"}
	TableComponentShortcutCycleSortColumnRight = shortcut_helper.ShortcutEntry{KeyCombo: []string{"→"}, Name: "Cycle Sort Column Right"}
)

func DrawScrollbarLine(screen tcell.Screen, x, y, width, height int, fg tcell.Color, isHorizontal bool, isBar bool) {
	style := tcell.StyleDefault.Foreground(fg)
	var char rune
	if isHorizontal {
		if isBar {
			char = '━' // Heavy horizontal
		} else {
			char = '─' // Light horizontal
		}
	} else {
		if isBar {
			char = '┃' // Heavy vertical
		} else {
			char = '│' // Light vertical
		}
	}
	for iy := 0; iy < height; iy++ {
		for ix := 0; ix < width; ix++ {
			screen.SetContent(x+ix, y+iy, char, nil, style)
		}
	}
}
