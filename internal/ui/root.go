package ui

import (
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

func CreateUi(path string, fullscreen bool) *tview.Application {
	application := tview.NewApplication()

	rootLayout := createRootLayout()

	header := NewApplicationHeader()
	fileBrowser := NewFileBrowser(application, path)

	rootLayout.AddItem(header.layout, 1, 0, false)
	rootLayout.AddItem(fileBrowser.page, 0, 1, true)

	rootLayout.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Rune() == 'q' || event.Key() == tcell.KeyCtrlC || event.Key() == tcell.KeyCtrlQ {
			application.Stop()
			return nil
		}
		return event
	})

	return application.SetRoot(rootLayout, fullscreen).SetFocus(fileBrowser.table)
}

func createRootLayout() *tview.Flex {
	rootLayout := tview.NewFlex().SetDirection(tview.FlexRow)
	return rootLayout
}
