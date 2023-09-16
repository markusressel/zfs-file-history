package ui

import (
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"zfs-file-history/internal/ui/dialog"
	"zfs-file-history/internal/ui/util"
)

const (
	Main       util.Page = "main"
	HelpDialog util.Page = "help"
)

func CreateUi(path string, fullscreen bool) *tview.Application {
	application := tview.NewApplication()
	application.EnableMouse(true)

	mainPage := NewMainPage(application, path)
	helpPage := dialog.NewHelpPage()

	pagesLayout := tview.NewPages().
		AddPage(string(Main), mainPage.layout, true, true).
		AddPage(string(HelpDialog), helpPage.GetLayout(), true, false)

	pagesLayout.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		// ignore events, if some other page is open
		name, _ := pagesLayout.GetFrontPage()
		fileBrowserPage, _ := mainPage.fileBrowser.layout.GetFrontPage()
		if name != string(Main) || fileBrowserPage != string(FileBrowserPage) {
			return event
		}

		if event.Rune() == 'q' || event.Key() == tcell.KeyCtrlC || event.Key() == tcell.KeyCtrlQ {
			application.Stop()
			return nil
		} else if event.Rune() == '?' {
			pagesLayout.ShowPage(string(HelpDialog))
			return nil
		}
		return event
	})

	helpPage.GetLayout().SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Rune() == 'q' || event.Key() == tcell.KeyEscape {
			pagesLayout.HidePage(string(HelpDialog))
			return nil
		}
		return event
	})

	return application.SetRoot(pagesLayout, fullscreen).SetFocus(mainPage.fileBrowser.tableContainer.GetLayout())
}
