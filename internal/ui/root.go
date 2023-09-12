package ui

import (
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

func CreateUi(path string, fullscreen bool) *tview.Application {
	application := tview.NewApplication()

	mainPage := NewMainPage(application, path)
	helpPage := NewHelpPage()

	pagesLayout := tview.NewPages().
		AddPage("main", mainPage.layout, true, true).
		AddPage("help", helpPage.layout, true, true)

	pagesLayout.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Rune() == 'q' || event.Key() == tcell.KeyCtrlC || event.Key() == tcell.KeyCtrlQ {
			application.Stop()
			return nil
		} else if event.Rune() == '?' {
			pagesLayout.ShowPage("help")
			return nil
		}
		return event
	})

	return application.SetRoot(pagesLayout, fullscreen).SetFocus(mainPage.fileBrowser.fileTable)
}
