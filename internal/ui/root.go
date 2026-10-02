package ui

import (
	"slices"
	"zfs-file-history/internal/ui/dialog"
	"zfs-file-history/internal/ui/util"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

const (
	Main       util.Page = "main"
	HelpDialog util.Page = "help"
	Dataset    util.Page = "dataset"
)

// switchablePages are the pages cycled through with tab / shift+tab, in order.
var switchablePages = []util.Page{Main, Dataset}

// pageTitles are the titles of the switchable pages, shown in the page indicator of the header.
var pageTitles = map[util.Page]string{
	Main:    "Files",
	Dataset: "Datasets",
}

// pageTitleWidth is the length of the longest page title, so the page indicator has the same width on all pages.
func pageTitleWidth() int {
	width := 0
	for _, page := range switchablePages {
		width = max(width, len(pageTitles[page]))
	}
	return width
}

// adjacentPage returns the page after (or before, if reversed) current, wrapping around.
func adjacentPage(pages []util.Page, current util.Page, reversed bool) util.Page {
	index := slices.Index(pages, current)
	if index < 0 {
		return pages[0]
	}
	offset := 1
	if reversed {
		offset = len(pages) - 1
	}
	return pages[(index+offset)%len(pages)]
}

type FocusableUiComponent interface {
	Focus()
	HasFocus() bool
}

func CreateUi(path string, fullscreen bool) *tview.Application {
	application, _, _ := createUi(path, fullscreen)
	return application
}

// createUi creates the application and also returns its pages, for tests.
func createUi(path string, fullscreen bool) (*tview.Application, *MainPage, *DatasetPage) {
	// completely disable double click interval to avoid unnecessary delays
	tview.DoubleClickInterval = 0

	application := tview.NewApplication()
	application.EnableMouse(true)

	mainPage := NewMainPage(application, path)
	helpPage := dialog.NewHelpPage()
	datasetPage := NewDatasetPage(application, path)

	pagesLayout := tview.NewPages().
		AddPage(string(Main), mainPage.layout, true, true).
		AddPage(string(Dataset), datasetPage.layout, true, false).
		AddPage(string(HelpDialog), helpPage.GetLayout(), true, false)

	mainPage.SetPages(pagesLayout)
	datasetPage.SetPages(pagesLayout)

	headers := map[util.Page]*ApplicationHeaderComponent{Main: mainPage.header, Dataset: datasetPage.header}
	for index, page := range switchablePages {
		headers[page].SetPage(pageTitles[page], pageTitleWidth(), index+1, len(switchablePages))
	}

	pagesLayout.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		// ignore events, if some other page is open
		name, _ := pagesLayout.GetFrontPage()

		if name != string(Main) && name != string(Dataset) {
			return event
		}

		if event.Key() == tcell.KeyCtrlC || event.Key() == tcell.KeyCtrlQ {
			application.Stop()
			return nil
		} else if (event.Key() == tcell.KeyRune && event.Rune() == '?' && !util.IsTextInputActive(application.GetFocus())) ||
			event.Key() == tcell.KeyF1 {
			pagesLayout.ShowPage(string(HelpDialog))
			return nil
		} else if event.Key() == tcell.KeyTab || event.Key() == tcell.KeyBacktab {
			nextPage := adjacentPage(switchablePages, util.Page(name), event.Key() == tcell.KeyBacktab)
			pagesLayout.SwitchToPage(string(nextPage))
			// the shortcut map of a page is only updated while it is visible, so refresh it now
			switch nextPage {
			case Main:
				mainPage.refreshShortcutMap()
			case Dataset:
				datasetPage.refreshShortcutMap()
			}
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

	// The application only supports a single after draw func, so dispatch to the page in front.
	application.SetAfterDrawFunc(func(screen tcell.Screen) {
		name, _ := pagesLayout.GetFrontPage()
		switch name {
		case string(Main):
			mainPage.drawBoundaryHighlights(screen)
		case string(Dataset):
			datasetPage.drawBoundaryHighlights(screen)
		}
	})

	mainPage.Init(path)
	datasetPage.Init(path)

	application.SetRoot(pagesLayout, fullscreen).
		SetFocus(mainPage.fileBrowser.GetLayout())
	mainPage.updateShortcutMap(mainPage.fileBrowser)

	return application, mainPage, datasetPage
}
