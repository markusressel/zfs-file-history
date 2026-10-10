package ui

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"zfs-file-history/internal/state"
	"zfs-file-history/internal/ui/dataset_browser"
	"zfs-file-history/internal/ui/dialog"
	"zfs-file-history/internal/ui/file_browser"
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

var (
	shortcutCycleFocus = shortcut_helper.ShortcutEntry{KeyCombo: []string{shortcut_helper.KeyTab, shortcut_helper.Shift(shortcut_helper.KeyTab)}, Name: "Cycle focus", Description: "Focus the next list of the page", Group: shortcut_helper.GroupNavigation}
	shortcutGoToPage   = shortcut_helper.ShortcutEntry{KeyCombo: []string{"1", "2"}, Name: "Go to page", Group: shortcut_helper.GroupGlobal}
	shortcutCommands   = shortcut_helper.ShortcutEntry{KeyCombo: []string{":"}, Name: "Commands", Group: shortcut_helper.GroupGlobal}
	shortcutQuit       = shortcut_helper.ShortcutEntry{KeyCombo: []string{shortcut_helper.Ctrl("q")}, Name: "Quit", Group: shortcut_helper.GroupGlobal}
)

// globalShortcuts are the shortcuts that work on all pages, shown after the ones of the focused component. The
// others are only listed in the command menu, see globalCommands.
func globalShortcuts() []shortcut_helper.ShortcutEntry {
	return []shortcut_helper.ShortcutEntry{
		shortcutCycleFocus,
		shortcutGoToPage,
		shortcutCommands,
		shortcut_helper.ShortcutHide,
		shortcutQuit,
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

// switchablePage is what root needs of the switchable pages.
type switchablePage interface {
	refreshShortcutMap()
	CycleFocus(reversed bool)
	// commandSections returns the commands of the command menu for the focused component and the page
	commandSections() []dialog.CommandSection
	// refresh reloads the data shown (F5)
	refresh()
	// focusComponent and setOnFocusChanged restore and record the focused component, see session
	focusComponent(title string)
	setOnFocusChanged(onFocusChanged func(title string))
}

type FocusableUiComponent interface {
	Focus()
	HasFocus() bool
}

// CreateUi creates the application, showing what start says, and records the session in state.Current.
func CreateUi(start Start, fullscreen bool) *tview.Application {
	application, _, _ := createUiFor(start, fullscreen)
	return application
}

// createUi creates the application showing path, and also returns its pages, for tests.
func createUi(path string, fullscreen bool) (*tview.Application, *MainPage, *DatasetPage) {
	return createUiFor(Start{Path: path, LaunchDir: path, PathGiven: true}, fullscreen)
}

func createUiFor(start Start, fullscreen bool) (*tview.Application, *MainPage, *DatasetPage) {
	restored := sessionToRestore(state.Current, start)
	path := start.startPath(restored)
	recorder := newSessionRecorder(state.Current, start.LaunchDir, restored)

	// completely disable double click interval to avoid unnecessary delays
	tview.DoubleClickInterval = 0

	application := tview.NewApplication()
	application.EnableMouse(true)
	util.InitTimeFormat()
	shortcut_helper.InitShortcutVisibility()

	// one for all pages, so a message is shown on whichever page is shown
	messages := status_message.NewCenter(application)
	if version, newer := state.Current.NewerVersion(); newer {
		messages.Show(newerStateVersionMessage(version))
	}
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

	pages := map[util.Page]switchablePage{Main: mainPage, Dataset: datasetPage}
	switchTo := func(page util.Page) {
		pagesLayout.SwitchToPage(string(page))
		recorder.setPage(page)
		pages[page].refreshShortcutMap()
	}

	// the session is restored on the next start, see Start.RestoreSession
	for page, switchable := range pages {
		switchable.setOnFocusChanged(func(title string) { recorder.setFocus(page, title) })
	}
	mainPage.fileBrowser.Events.Subscribe(func(event file_browser.Event) {
		if e, ok := event.(file_browser.PathChangedEvent); ok {
			recorder.setPath(e.NewPath)
		}
	})
	datasetPage.datasetBrowser.Events.Subscribe(func(event dataset_browser.Event) {
		if e, ok := event.(dataset_browser.SelectedDatasetChangedEvent); ok && e.Dataset != nil {
			recorder.setDataset(e.Dataset.Name)
		}
	})
	if restored != nil && restored.Dataset != "" {
		datasetPage.datasetBrowser.SelectOnLoad(restored.Dataset)
	}
	toggleShortcuts := func() {
		shortcut_helper.ToggleShortcuts()
		for _, header := range headers {
			header.UpdateShortcutHint()
		}
	}

	// globalCommands are the commands of the command menu that work on all pages, see globalShortcuts
	globalCommands := func(front util.Page) []shortcut_helper.ShortcutEntry {
		commands := []shortcut_helper.ShortcutEntry{
			shortcutCycleFocus.WithRun(func() { pages[front].CycleFocus(false) }),
		}
		for index, page := range switchablePages {
			commands = append(commands, shortcut_helper.ShortcutEntry{
				KeyCombo:    []string{strconv.Itoa(index + 1)},
				Name:        "Go to " + pageTitles[page],
				Description: "Show the " + strings.ToLower(pageTitles[page]) + " page",
				Group:       shortcut_helper.GroupGlobal,
				Run:         func() { switchTo(page) },
			})
		}
		hideShortcuts := shortcut_helper.ShortcutHide
		if shortcut_helper.ShortcutsHidden() {
			hideShortcuts.Name = "Show shortcuts"
			hideShortcuts.Description = "Show the keys at the bottom again"
		}
		return append(commands,
			shortcut_helper.ShortcutEntry{KeyCombo: []string{shortcut_helper.Alt("→")}, Name: "Next page", Description: "Show the page on the right", Group: shortcut_helper.GroupGlobal,
				Run: func() { switchTo(adjacentPage(switchablePages, front, false)) }},
			shortcut_helper.ShortcutEntry{KeyCombo: []string{shortcut_helper.Alt("←")}, Name: "Previous page", Description: "Show the page on the left", Group: shortcut_helper.GroupGlobal,
				Run: func() { switchTo(adjacentPage(switchablePages, front, true)) }},
			shortcut_helper.ShortcutTimeFormat.WithRun(util.ToggleRelativeTimes),
			shortcut_helper.ShortcutEntry{KeyCombo: []string{"F5"}, Name: "Refresh", Description: "Reload the datasets, snapshots and files",
				Group: shortcut_helper.GroupGlobal, Run: pages[front].refresh},
			shortcut_helper.ShortcutEntry{KeyCombo: []string{"m"}, Name: "Messages", Description: "Show all messages, e.g. errors", Group: shortcut_helper.GroupGlobal,
				Run: func() { showMessageHistory(application, pagesLayout, messages) }},
			hideShortcuts.WithRun(toggleShortcuts),
			shortcutQuit.WithRun(application.Stop).WithDescription("Exit the application"),
		)
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
			toggleShortcuts()
			return nil
		}

		// ignore events, if some other page is open
		name, _ := pagesLayout.GetFrontPage()
		front := util.Page(name)
		if front != Main && front != Dataset {
			return event
		}

		textInput := util.IsTextInputActive(application.GetFocus())
		switch {
		case event.Key() == tcell.KeyCtrlC || event.Key() == tcell.KeyCtrlQ:
			application.Stop()
		case event.Key() == tcell.KeyRune && event.Rune() == ':' && !textInput:
			dialog.ShowCommandMenu(application, pagesLayout,
				append(pages[front].commandSections(), dialog.CommandSection{Title: "Global", Commands: globalCommands(front)}))
		case event.Key() == tcell.KeyRune && event.Rune() == 'm' && !textInput:
			showMessageHistory(application, pagesLayout, messages)
		case (event.Key() == tcell.KeyLeft || event.Key() == tcell.KeyRight) && event.Modifiers()&tcell.ModAlt != 0 && !textInput:
			switchTo(adjacentPage(switchablePages, front, event.Key() == tcell.KeyLeft))
		case event.Key() == tcell.KeyRune && (event.Rune() == '1' || event.Rune() == '2') && !textInput:
			switchTo(switchablePages[event.Rune()-'1'])
		default:
			return event
		}
		return nil
	})

	mainPage.Init(path)
	datasetPage.Init(path)

	application.SetRoot(pagesLayout, fullscreen).
		SetFocus(mainPage.fileBrowser.GetLayout())
	recorder.setPath(path)
	mainPage.updateShortcutMap(mainPage.fileBrowser)
	restoreSession(restored, pages, switchTo)

	return application, mainPage, datasetPage
}

// showMessageHistory shows all messages, and marks them as seen. Must be called on the UI thread.
func showMessageHistory(application *tview.Application, pages *tview.Pages, messages *status_message.Center) {
	dialog.ShowDialogOnPages(application, pages, dialog.NewMessageHistoryDialog(messages.History()), nil)
	messages.MarkRead()
}

// newerStateVersionMessage is the warning shown when the state file was written by a newer version of the
// application (format version), see state.Store.NewerVersion.
func newerStateVersionMessage(version int) *status_message.StatusMessage {
	return status_message.NewWarningStatusMessage(fmt.Sprintf(
		"The remembered settings were saved by a newer version of zfs-file-history (format %d). "+
			"Changes are not saved, to keep them. Please update zfs-file-history to the latest version.", version))
}
