package ui

import (
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

func CreateUi(path string, fullscreen bool) *tview.Application {
	application := tview.NewApplication()

	mainPage := NewMainPage(application, path)

	rootLayout := tview.NewPages().
		AddPage("main", mainPage.layout, true, true).
		//AddPage("modal", dialog, true, true)
		ShowPage("main")

	rootLayout.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		key := event.Key()
		if key == tcell.KeyTab || key == tcell.KeyBacktab {
			mainPage.ToggleFocus()
		}
		return event
	})

	rootLayout.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Rune() == 'q' || event.Key() == tcell.KeyCtrlC || event.Key() == tcell.KeyCtrlQ {
			application.Stop()
			return nil
		}
		return event
	})

	return application.SetRoot(rootLayout, fullscreen).SetFocus(mainPage.fileBrowser.fileTable)
}
