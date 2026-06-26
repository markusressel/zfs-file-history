package ui

import (
	"zfs-file-history/internal/ui/dialog"
	"zfs-file-history/internal/ui/util"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

const (
	Main       util.Page = "main"
	HelpDialog util.Page = "help"
)

type FocusableUiComponent interface {
	Focus()
	HasFocus() bool
}

func CreateUi(path string, fullscreen bool) *tview.Application {
	// completely disable double click interval to avoid unnecessary delays
	tview.DoubleClickInterval = 0

	application := tview.NewApplication()
	application.EnableMouse(true)

	mainPage := NewMainPage(application, path)
	helpPage := dialog.NewHelpPage()

	pagesLayout := tview.NewPages().
		AddPage(string(Main), mainPage.layout, true, true).
		AddPage(string(HelpDialog), helpPage.GetLayout(), true, false)

	mainPage.SetPages(pagesLayout)

	pagesLayout.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		// ignore events, if some other page is open
		name, _ := pagesLayout.GetFrontPage()

		if name != string(Main) {
			return event
		}

		if event.Key() == tcell.KeyCtrlC || event.Key() == tcell.KeyCtrlQ {
			application.Stop()
			return nil
		} else if event.Rune() == '?' || event.Key() == tcell.KeyF1 {
			pagesLayout.ShowPage(string(HelpDialog))
			return nil
		}
		return event
	})

	helpPage.GetLayout().SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyEscape {
			pagesLayout.HidePage(string(HelpDialog))
			return nil
		}
		return event
	})

	mainPage.Init(path)

	application.SetRoot(pagesLayout, fullscreen).
		SetFocus(mainPage.fileBrowser.GetLayout())
	mainPage.updateShortcutMap(mainPage.fileBrowser)

	return application
}
