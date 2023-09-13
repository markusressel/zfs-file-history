package util

import (
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"zfs-file-history/internal/ui/theme"
)

type WindowTitle[T tview.Box] interface {
	SetTitle(title string) *T
	SetTitleColor(color tcell.Color) *T
	SetTitleAlign(align int) *T
}

func SetupWindowTitle[T WindowTitle[tview.Box]](window T, text string) T {
	window.SetTitle(theme.CreateTitleText(text))
	window.SetTitleColor(theme.GetTitleColor())
	window.SetTitleAlign(theme.GetTitleAlign())
	return window
}
