package ui

import (
	"slices"
	"zfs-file-history/internal/ui/dialog"
	"zfs-file-history/internal/ui/shortcut_helper"
	"zfs-file-history/internal/ui/status_message"
	"zfs-file-history/internal/ui/util"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

const (
	Main    util.Page = "main"
	Dataset util.Page = "dataset"
)

// globalShortcuts are the shortcuts that work on all pages, shown after the ones of the focused component.
func globalShortcuts() []shortcut_helper.ShortcutEntry {
	return []shortcut_helper.ShortcutEntry{
		{KeyCombo: []string{shortcut_helper.KeyTab, shortcut_helper.Shift(shortcut_helper.KeyTab)}, Name: "Cycle focus", Group: shortcut_helper.GroupNavigation},
		{KeyCombo: []string{"1", "2"}, Name: "Go to page", Group: shortcut_helper.GroupGlobal},
		{KeyCombo: []string{shortcut_helper.Alt("←"), shortcut_helper.Alt("→")}, Name: "Switch page", Group: shortcut_helper.GroupGlobal},
		shortcut_helper.ShortcutTimeFormat,
		{KeyCombo: []string{"F5"}, Name: "Refresh", Group: shortcut_helper.GroupGlobal},
		{KeyCombo: []string{"m"}, Name: "Messages", Group: shortcut_helper.GroupGlobal},
		shortcut_helper.ShortcutHide,
		{KeyCombo: []string{shortcut_helper.Ctrl("q")}, Name: "Quit", Group: shortcut_helper.GroupGlobal},
	}
}

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
	util.InitTimeFormat()
	shortcut_helper.InitShortcutVisibility()

	// one for all pages, so a message is shown on whichever page is shown
	messages := status_message.NewCenter(application)
	mainPage := NewMainPage(application, messages, path)
	datasetPage := NewDatasetPage(application, messages, path)

	pagesLayout := tview.NewPages().
		AddPage(string(Main), mainPage.layout, true, true).
		AddPage(string(Dataset), datasetPage.layout, true, false)

	mainPage.SetPages(pagesLayout)
	datasetPage.SetPages(pagesLayout)

	headers := map[util.Page]*ApplicationHeaderComponent{Main: mainPage.header, Dataset: datasetPage.header}
	for index, page := range switchablePages {
		headers[page].SetPage(pageTitles[page], pageTitleWidth(), index+1, len(switchablePages))
	}

	pagesLayout.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		// before the key is handled, so a message shown because of it stays
		messages.DismissOnKeyPress()

		// the time format also applies to the tables of dialogs and overlays (e.g. the histories), which are pages
		// of their own
		if event.Key() == tcell.KeyRune && event.Rune() == 'T' && !util.IsTextInputActive(application.GetFocus()) {
			// tables show the new format when they are drawn, which happens after every key
			util.ToggleRelativeTimes()
			return nil
		}
		// hides or shows the shortcuts at the bottom of the pages and overlays, to make room in small terminals
		if (event.Key() == tcell.KeyRune && event.Rune() == '?' && !util.IsTextInputActive(application.GetFocus())) ||
			event.Key() == tcell.KeyF1 {
			shortcut_helper.ToggleShortcuts()
			for _, header := range headers {
				header.UpdateShortcutHint()
			}
			return nil
		}

		// ignore events, if some other page is open
		name, _ := pagesLayout.GetFrontPage()

		if name != string(Main) && name != string(Dataset) {
			return event
		}

		if event.Key() == tcell.KeyCtrlC || event.Key() == tcell.KeyCtrlQ {
			application.Stop()
			return nil
		} else if event.Key() == tcell.KeyRune && event.Rune() == 'm' && !util.IsTextInputActive(application.GetFocus()) {
			showMessageHistory(application, pagesLayout, messages)
			return nil
		} else if (event.Key() == tcell.KeyLeft || event.Key() == tcell.KeyRight) && event.Modifiers()&tcell.ModAlt != 0 && !util.IsTextInputActive(application.GetFocus()) {
			nextPage := adjacentPage(switchablePages, util.Page(name), event.Key() == tcell.KeyLeft)
			pagesLayout.SwitchToPage(string(nextPage))
			switch nextPage {
			case Main:
				mainPage.refreshShortcutMap()
			case Dataset:
				datasetPage.refreshShortcutMap()
			}
			return nil
		} else if event.Key() == tcell.KeyRune && (event.Rune() == '1' || event.Rune() == '2') && !util.IsTextInputActive(application.GetFocus()) {
			nextPage := Main
			if event.Rune() == '2' {
				nextPage = Dataset
			}
			pagesLayout.SwitchToPage(string(nextPage))
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

	mainPage.Init(path)
	datasetPage.Init(path)

	application.SetRoot(pagesLayout, fullscreen).
		SetFocus(mainPage.fileBrowser.GetLayout())
	mainPage.updateShortcutMap(mainPage.fileBrowser)

	return application, mainPage, datasetPage
}

// showMessageHistory shows all messages, and marks them as seen. Must be called on the UI thread.
func showMessageHistory(application *tview.Application, pages *tview.Pages, messages *status_message.Center) {
	dialog.ShowDialogOnPages(application, pages, dialog.NewMessageHistoryDialog(messages.History()), nil)
	messages.MarkRead()
}
