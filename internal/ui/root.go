package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"zfs-file-history/internal/disk"
	"zfs-file-history/internal/state"
	"zfs-file-history/internal/ui/dataset_browser"
	"zfs-file-history/internal/ui/dialog"
	"zfs-file-history/internal/ui/file_browser"
	"zfs-file-history/internal/ui/pool_activity"
	"zfs-file-history/internal/ui/pool_browser"
	"zfs-file-history/internal/ui/shortcut_helper"
	"zfs-file-history/internal/ui/status_message"
	"zfs-file-history/internal/ui/util"
	"zfs-file-history/internal/zfs"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

const (
	Main    util.Page = "main"
	Dataset util.Page = "dataset"
	Pool    util.Page = "pool"
	Disks   util.Page = "disks"
)

var (
	shortcutCycleFocus = shortcut_helper.ShortcutEntry{KeyCombo: []string{shortcut_helper.KeyTab, shortcut_helper.Shift(shortcut_helper.KeyTab)}, Name: "Cycle focus", Description: "Focus the next list of the page", Group: shortcut_helper.GroupNavigation}
	shortcutGoToPage   = shortcut_helper.ShortcutEntry{KeyCombo: []string{"1", "2", "3", "4"}, Name: "Go to page", Group: shortcut_helper.GroupGlobal}
	shortcutCommands   = shortcut_helper.ShortcutEntry{KeyCombo: []string{":"}, Name: "Commands", Group: shortcut_helper.GroupGlobal}
	shortcutQuit       = shortcut_helper.ShortcutEntry{KeyCombo: []string{shortcut_helper.Ctrl("q")}, Name: "Quit", Group: shortcut_helper.GroupGlobal}
)

// globalShortcuts are the shortcuts that work on all pages, shown after the ones of the focused component. The
// others are only listed in the command menu, see globalCommands.
// newTemperatureSampler and newSmartSource return what reads the temperatures and the SMART values of the disks,
// replaceable in tests.
var (
	newTemperatureSampler = func() disk.TemperatureSampler { return disk.NewSampler() }
	newSmartSource        = func() disk.SmartSource { return disk.NewSmartReader() }
)

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
var switchablePages = []util.Page{Main, Dataset, Pool, Disks}

// pageTitles are the titles of the switchable pages, shown in the page tabs of the header.
var pageTitles = map[util.Page]string{
	Main:    "Files",
	Dataset: "Datasets",
	Pool:    "Pools",
	Disks:   "Disks",
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
	// one for all pages, like the messages: the operations running on the pools are shown on every page
	activities := pool_activity.NewMonitor(application)
	sampler := newTemperatureSampler()
	activities.SetTemperatureSampler(sampler)
	smart := newSmartSource()
	activities.SetSmartSource(smart)
	mainPage := NewMainPage(application, messages, activities, path)
	datasetPage := NewDatasetPage(application, messages, activities, path)
	poolPage := NewPoolPage(application, messages, activities, sampler, smart)
	diskPage := NewDiskPage(application, messages, activities, sampler, smart)

	pagesLayout := tview.NewPages().
		AddPage(string(Main), mainPage.layout, true, true).
		AddPage(string(Dataset), datasetPage.layout, true, false).
		AddPage(string(Pool), poolPage.layout, true, false).
		AddPage(string(Disks), diskPage.layout, true, false)

	mainPage.SetPages(pagesLayout)
	datasetPage.SetPages(pagesLayout)
	poolPage.SetPages(pagesLayout)
	diskPage.SetPages(pagesLayout)

	headers := map[util.Page]*ApplicationHeaderComponent{Main: mainPage.header, Dataset: datasetPage.header, Pool: poolPage.header, Disks: diskPage.header}
	pages := map[util.Page]switchablePage{Main: mainPage, Dataset: datasetPage, Pool: poolPage, Disks: diskPage}
	switchTo := func(page util.Page) {
		pagesLayout.SwitchToPage(string(page))
		recorder.setPage(page)
		pages[page].refreshShortcutMap()
	}
	titles := make([]string, len(switchablePages))
	for index, page := range switchablePages {
		titles[index] = pageTitles[page]
	}
	for index, page := range switchablePages {
		// a click on a number does what its key does
		headers[page].SetPages(titles, index, func(selected int) { switchTo(switchablePages[selected]) }, func() bool {
			front, _ := pagesLayout.GetFrontPage()
			return front == string(page)
		})
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
		switch e := event.(type) {
		case dataset_browser.SelectedDatasetChangedEvent:
			if e.Dataset != nil {
				recorder.setDataset(e.Dataset.Name)
			}
		case dataset_browser.OpenInFileBrowserEvent:
			switchTo(Main)
			mainPage.focusComponent(mainPage.componentTitles[mainPage.fileBrowser])
			mainPage.fileBrowser.SetPath(e.Path, true)
		}
	})
	poolPage.poolBrowser.Events.Subscribe(func(event pool_browser.Event) {
		if e, ok := event.(pool_browser.SelectedPoolChangedEvent); ok && e.Pool != nil {
			recorder.setPool(e.Pool.Name)
		}
	})
	// a click on a running operation or a problem shows its pool
	showPool := func(pool string) {
		switchTo(Pool)
		poolPage.poolBrowser.SelectPool(pool)
	}
	// the disks of a device (device "": of the pool) with their SMART values, from the devices pane or an alert
	showDisk := func(pool string, device string) {
		switchTo(Disks)
		diskPage.focusComponent(diskPage.componentTitles[diskPage.diskBrowser])
		diskPage.diskBrowser.SelectDevice(pool, device)
	}
	poolPage.poolDevices.SetOnShowDisk(showDisk)
	// showSetupPermissions offers to allow the commands that need root (see zfs.SudoersRules), from the command
	// menu, or by itself (automatic) at the start if not all that only read are allowed: then only if nothing else
	// is in front, and with the option to never ask again.
	showSetupPermissions := func(automatic bool) {
		neverAsk := state.Current.Toggle(setupPermissionsNeverAskToggle, false)
		if automatic && neverAsk {
			return
		}
		go func() {
			allowed := allowedSudoersRules()
			if automatic && !slices.ContainsFunc(zfs.SudoersRules, func(rule *zfs.SudoersRule) bool { return rule.ReadOnly && !allowed[rule.Id] && rule.IsAvailable() }) {
				return
			}
			application.QueueUpdateDraw(func() {
				var onNeverAsk func()
				if automatic {
					if name, _ := pagesLayout.GetFrontPage(); !slices.Contains(switchablePages, util.Page(name)) {
						return
					}
					onNeverAsk = func() { state.Current.SetToggle(setupPermissionsNeverAskToggle, true) }
				}
				dialog.ShowDialogOnPages(application, pagesLayout, dialog.NewSetupPermissionsDialog(application, allowed, onNeverAsk), nil)
			})
		}()
	}
	// the damaged files, from the alert of the data errors or the command menu; a file is shown in the file browser
	var dataErrorsDialog *dialog.DataErrorsDialog
	// shownDataErrorsDialog returns the dialog of the damaged files while it is shown, nil otherwise
	shownDataErrorsDialog := func() *dialog.DataErrorsDialog {
		if dataErrorsDialog == nil || !pagesLayout.HasPage(string(dialog.DataErrorsDialogPage)) {
			return nil
		}
		return dataErrorsDialog
	}
	showObjectSearch := func(d *dialog.DataErrorsDialog) {
		if search := activities.ObjectSearch(); search != nil {
			d.SetObjectSearch(search.Progress, search.Running, search.Err)
		}
	}
	showDataErrors := func() {
		index := activities.DataErrors()
		var onFind func()
		if index.HasUnresolvedObjects() {
			onFind = activities.FindObjects
		}
		dataErrorsDialog = dialog.NewDataErrorsDialog(application, index, func(dataError zfs.DataError) {
			switchTo(Main)
			mainPage.focusComponent(mainPage.componentTitles[mainPage.fileBrowser])
			mainPage.fileBrowser.SetPathWithSelection(existingFolderOf(dataError.LivePath))
			// the damaged version, e.g. of a file that only exists in snapshots
			if dataError.Snapshot != "" {
				mainPage.snapshotBrowser.SelectSnapshot(dataError.SnapshotFullName())
			}
		}, onFind, activities.CancelObjectSearch)
		showObjectSearch(dataErrorsDialog)
		dataErrorsDialog.SetOnSetup(func() { showSetupPermissions(false) })
		dialog.ShowDialogOnPages(application, pagesLayout, dataErrorsDialog, nil)
	}
	activities.OnDataErrorsChanged(func() {
		if d := shownDataErrorsDialog(); d != nil {
			d.SetIndex(activities.DataErrors())
		}
	})
	activities.OnObjectSearchChanged(func() {
		if d := shownDataErrorsDialog(); d != nil {
			showObjectSearch(d)
		}
	})
	for _, page := range []*basePage{&mainPage.basePage, &datasetPage.basePage, &poolPage.basePage, &diskPage.basePage} {
		page.activityBar.SetOnClick(func(activity zfs.PoolActivity) { showPool(activity.Pool) })
		page.alertBar.SetOnClick(func(alert zfs.PoolAlert) {
			if alert.Kind == zfs.PoolAlertDataErrors && activities.DataErrors() != nil {
				showDataErrors()
				return
			}
			if alert.Kind == zfs.PoolAlertTemperature || alert.Kind == zfs.PoolAlertSmart {
				showDisk(alert.Pool, "")
				return
			}
			showPool(alert.Pool)
		})
	}
	// e.g. after F5, or a change reported by the ZFS event watcher (see zfs.WatchZpoolEvents). After F5 and changes
	// of the snapshots (e.g. one holding damaged files was destroyed), the damaged files are listed again.
	util.SubscribeUI(zfs.DatasetsLoaded, application, func(_ struct{}) { activities.RefreshDataErrors() })
	util.SubscribeUI(zfs.PoolsChanged, application, func(_ struct{}) { activities.Refresh() })
	// e.g. the damaged files may be listed now
	util.SubscribeUI(zfs.SudoersChanged, application, func(_ struct{}) {
		activities.ReloadDataErrors()
		// e.g. smartctl may run with sudo now
		activities.RefreshSmart()
		poolPage.poolDevices.ReadSmart()
		diskPage.diskBrowser.ReadSmart()
	})
	poolPage.poolDevices.SetOnSetup(func() { showSetupPermissions(false) })
	diskPage.diskBrowser.SetOnSetup(func() { showSetupPermissions(false) })

	if restored != nil && restored.Dataset != "" {
		datasetPage.datasetBrowser.SelectOnLoad(restored.Dataset)
	}
	if restored != nil && restored.Pool != "" {
		poolPage.poolBrowser.SelectOnLoad(restored.Pool)
	}
	toggleShortcuts := func() {
		shortcut_helper.ToggleShortcuts()
		for _, header := range headers {
			header.UpdateShortcutHint()
		}
	}

	// the command selected when the command menu was closed, selected again when it is opened
	commandMenuMemory := &dialog.CommandMenuMemory{}

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
				// the keys are at the bottom, the pages in the header
				SearchOnly: true,
			})
		}
		hideShortcuts := shortcut_helper.ShortcutHide
		if shortcut_helper.ShortcutsHidden() {
			hideShortcuts.Name = "Show shortcuts"
			hideShortcuts.Description = "Show the keys at the bottom again"
		}
		return append(commands,
			shortcut_helper.ShortcutEntry{KeyCombo: []string{shortcut_helper.Alt("→")}, Name: "Next page", Description: "Show the page on the right", Group: shortcut_helper.GroupGlobal,
				Run: func() { switchTo(adjacentPage(switchablePages, front, false)) }, SearchOnly: true},
			shortcut_helper.ShortcutEntry{KeyCombo: []string{shortcut_helper.Alt("←")}, Name: "Previous page", Description: "Show the page on the left", Group: shortcut_helper.GroupGlobal,
				Run: func() { switchTo(adjacentPage(switchablePages, front, true)) }, SearchOnly: true},
			shortcut_helper.ShortcutTimeFormat.WithRun(util.ToggleRelativeTimes).
				AsSwitch("Relative times", "Show times like \"3 minutes ago\" instead of dates", util.IsRelativeTimes),
			shortcut_helper.ShortcutEntry{KeyCombo: []string{"F5"}, Name: "Refresh", Description: "Reload the pools, datasets, snapshots and files",
				Group: shortcut_helper.GroupGlobal, Run: pages[front].refresh},
			shortcut_helper.ShortcutEntry{KeyCombo: []string{"m"}, Name: "Messages", Description: "Show all messages, e.g. errors", Group: shortcut_helper.GroupGlobal,
				Run: func() { showMessageHistory(application, pagesLayout, messages) }},
			shortcut_helper.ShortcutEntry{Name: "Activities: show " + activities.TimeLeftMode().Next().Name(),
				Description: "Switch between the time left of the activities, when they end (ETA), and both (also a click on it)",
				Group:       shortcut_helper.GroupView, Run: activities.CycleTimeLeftMode},
			shortcut_helper.ShortcutEntry{Name: "Damaged files", Description: "Show the files that ZFS reported as damaged (data errors)",
				Group: shortcut_helper.GroupGlobal, Run: showDataErrors},
			shortcut_helper.ShortcutEntry{Name: "Set up permissions", Description: "Allow your user to run the zpool commands that need root (live updates, damaged files, pool actions)",
				Group: shortcut_helper.GroupGlobal, Run: func() { showSetupPermissions(false) }},
			hideShortcuts.WithRun(toggleShortcuts).
				AsSwitch("Show shortcuts", "The keys at the bottom", func() bool { return !shortcut_helper.ShortcutsHidden() }),
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
		if !slices.Contains(switchablePages, front) {
			return event
		}

		textInput := util.IsTextInputActive(application.GetFocus())
		switch {
		case event.Key() == tcell.KeyCtrlC || event.Key() == tcell.KeyCtrlQ:
			application.Stop()
		case event.Key() == tcell.KeyRune && event.Rune() == ':' && !textInput:
			dialog.ShowCommandMenu(application, pagesLayout,
				append(pages[front].commandSections(), dialog.CommandSection{Title: "Global", Commands: globalCommands(front)}),
				commandMenuMemory)
		case event.Key() == tcell.KeyRune && event.Rune() == 'm' && !textInput:
			showMessageHistory(application, pagesLayout, messages)
		case (event.Key() == tcell.KeyLeft || event.Key() == tcell.KeyRight) && event.Modifiers()&tcell.ModAlt != 0 && !textInput:
			switchTo(adjacentPage(switchablePages, front, event.Key() == tcell.KeyLeft))
		case event.Key() == tcell.KeyRune && event.Rune() >= '1' && int(event.Rune()-'1') < len(switchablePages) && !textInput:
			switchTo(switchablePages[event.Rune()-'1'])
		default:
			return event
		}
		return nil
	})

	mainPage.Init(path)
	datasetPage.Init(path)
	poolPage.Init()
	activities.Refresh()
	showSetupPermissions(true)

	application.SetRoot(pagesLayout, fullscreen).
		SetFocus(mainPage.fileBrowser.GetLayout())
	util.InstallTextSelection(application, pagesLayout, func(text string) {
		messages.Show(copiedMessage(text))
	})
	recorder.setPath(path)
	mainPage.updateShortcutMap(mainPage.fileBrowser)
	restoreSession(restored, pages, switchTo)

	return application, mainPage, datasetPage
}

// setupPermissionsNeverAskToggle is the state toggle set when the user chose not to be offered the permissions at the
// start again, see dialog.NewSetupPermissionsDialog.
const setupPermissionsNeverAskToggle = "setupPermissionsNeverAsk"

// allowedSudoersRules is replaceable in tests, so they never run sudo.
var allowedSudoersRules = zfs.AllowedSudoersRules

// existingFolderOf returns the nearest folder above the path that exists, and the name of the entry in it that
// leads to the path, e.g. for a file that only exists in snapshots, in a folder that was deleted as well. This reads
// the file system, but only up to a few folders, which are usually cached.
func existingFolderOf(filePath string) (folder string, name string) {
	folder, name = filepath.Dir(filePath), filepath.Base(filePath)
	for folder != "/" && folder != "." {
		if stat, err := os.Stat(folder); err == nil && stat.IsDir() {
			break
		}
		folder, name = filepath.Dir(folder), filepath.Base(folder)
	}
	return folder, name
}

// showMessageHistory shows all messages, and marks them as seen. Must be called on the UI thread.
func showMessageHistory(application *tview.Application, pages *tview.Pages, messages *status_message.Center) {
	dialog.ShowDialogOnPages(application, pages, dialog.NewMessageHistoryDialog(messages.History()), nil)
	messages.MarkRead()
}

// copiedMessageLength is how much of a copied line is shown in the message that says it was copied.
const copiedMessageLength = 60

// copiedMessage says that the text was copied to the clipboard: the text itself if it is one line (shortened), else
// how many lines. The terminal may not support it (OSC 52), which cannot be found out.
func copiedMessage(text string) *status_message.StatusMessage {
	if lines := strings.Count(text, "\n") + 1; lines > 1 {
		return status_message.NewSuccessStatusMessage(fmt.Sprintf("Copied %d lines", lines))
	}
	if runes := []rune(text); len(runes) > copiedMessageLength {
		text = string(runes[:copiedMessageLength-1]) + "…"
	}
	return status_message.NewSuccessStatusMessage(fmt.Sprintf("Copied: %s", text))
}

// newerStateVersionMessage is the warning shown when the state file was written by a newer version of the
// application (format version), see state.Store.NewerVersion.
func newerStateVersionMessage(version int) *status_message.StatusMessage {
	return status_message.NewWarningStatusMessage(fmt.Sprintf(
		"The remembered settings were saved by a newer version of zfs-file-history (format %d). "+
			"Changes are not saved, to keep them. Please update zfs-file-history to the latest version.", version))
}
