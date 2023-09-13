package util

import (
	"fmt"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

type Page string

func CreateAttentionText(text string) *tview.TextView {
	abortText := fmt.Sprintf("  %s  ", text)
	return tview.NewTextView().SetText(abortText).SetTextColor(tcell.ColorYellow).SetTextAlign(tview.AlignRight)
}
