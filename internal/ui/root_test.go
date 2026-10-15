package ui

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
	"zfs-file-history/internal/state"
	"zfs-file-history/internal/testutil"
	"zfs-file-history/internal/ui/dialog"
	"zfs-file-history/internal/ui/path_overview"
	"zfs-file-history/internal/ui/shortcut_helper"
	"zfs-file-history/internal/ui/theme"
	"zfs-file-history/internal/ui/util"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAdjacentPage(t *testing.T) {
	two := []util.Page{Main, Dataset}
	assert.Equal(t, Dataset, adjacentPage(two, Main, false))
	assert.Equal(t, Main, adjacentPage(two, Dataset, false))
	assert.Equal(t, Dataset, adjacentPage(two, Main, true))
	assert.Equal(t, Main, adjacentPage(two, Dataset, true))

	three := []util.Page{"a", "b", "c"}
	assert.Equal(t, util.Page("b"), adjacentPage(three, "a", false))
	assert.Equal(t, util.Page("a"), adjacentPage(three, "c", false))
	assert.Equal(t, util.Page("c"), adjacentPage(three, "a", true))
	assert.Equal(t, util.Page("b"), adjacentPage(three, "c", true))

	// unknown pages fall back to the first page
	assert.Equal(t, Main, adjacentPage(two, "other", false))
}

// screenText returns the text currently drawn on the screen, with non-breaking spaces
// (used within shortcut entries) replaced by regular spaces.
// It reads the screen on the UI thread, which is where the application draws to it.
func screenText(t *testing.T, app *tview.Application, screen tcell.SimulationScreen) string {
	var cells []tcell.SimCell
	var width, height int
	// drawn first, see testutil.ScreenText
	testutil.OnUiThread(t, app, func() {
		app.ForceDraw()
		cells, width, height = screen.GetContents()
	})

	var text strings.Builder
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			if runes := cells[y*width+x].Runes; len(runes) > 0 && runes[0] != '\u00a0' {
				text.WriteRune(runes[0])
			} else {
				text.WriteRune(' ')
			}
		}
		text.WriteRune('\n')
	}
	return text.String()
}

func TestSwitchingPagesShowsShortcutsOfThePage(t *testing.T) {
	startPath := t.TempDir()
	app, _, datasetPage := createUi(startPath, true)
	screen := tcell.NewSimulationScreen("UTF-8")
	app.SetScreen(screen)
	screen.SetSize(200, 30)
	testutil.RunApp(t, app)

	// Like in real usage, let the hidden dataset page finish loading (and selecting) its datasets
	// before switching to it. Otherwise the selection while it is visible would update its shortcuts anyway.
	// Once a dataset is selected, the path changes from the start path to the dataset's mount path.
	// Tests have no ZFS (see testSystem): loading fails and nothing is selected, which is just as fine for this test.
	datasetsSelected := func() bool {
		selected := false
		testutil.OnUiThread(t, app, func() { selected = datasetPage.datasetBrowser.GetPath() != startPath })
		return selected
	}
	deadline := time.Now().Add(10 * time.Second)
	for !datasetsSelected() && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}

	waitForText := func(text string) {
		assert.Eventually(t, func() bool {
			return strings.Contains(screenText(t, app, screen), text)
		}, 3*time.Second, 20*time.Millisecond, "expected %q on the screen", text)
	}

	// shown by the file browser in the empty start folder, but not by the dataset browser
	const mainPageShortcut = "[h]: Folder history"
	waitForTextGone := func(text string) {
		assert.Eventually(t, func() bool {
			return !strings.Contains(screenText(t, app, screen), text)
		}, 3*time.Second, 20*time.Millisecond, "expected %q to be gone from the screen", text)
	}

	waitForText(mainPageShortcut)

	// dataset page, without any further key press
	screen.InjectKey(tcell.KeyRune, '2', tcell.ModNone)
	waitForTextGone(mainPageShortcut)
	waitForText("[/ǀctrl+f]: Filter")

	// and back
	screen.InjectKey(tcell.KeyRune, '1', tcell.ModNone)
	waitForText(mainPageShortcut)
}

func TestHelpKeyIsTypedIntoAFilter(t *testing.T) {
	app, mainPage, _ := createUi(t.TempDir(), true)
	screen := tcell.NewSimulationScreen("UTF-8")
	app.SetScreen(screen)
	screen.SetSize(200, 30)
	testutil.RunApp(t, app)

	// Key events and queued updates are processed from different channels, so instead of waiting for the
	// event loop, each step waits for its visible effect.
	press := func(key tcell.Key, r rune) {
		screen.InjectKey(key, r, tcell.ModNone)
	}
	frontPage := func() (name string) {
		testutil.OnUiThread(t, app, func() { name, _ = mainPage.pages.GetFrontPage() })
		return name
	}
	waitFor := func(message string, condition func() bool) {
		require.Eventually(t, condition, 10*time.Second, 20*time.Millisecond, message)
	}
	screenContains := func(text string) func() bool {
		return func() bool { return strings.Contains(screenText(t, app, screen), text) }
	}

	// While loading, the snapshot browser shows a loading view, which has the focus instead of the table.
	// Tests have no ZFS (see testSystem): loading fails quickly.
	waitFor("snapshots loaded", func() bool {
		loaded := false
		testutil.OnUiThread(t, app, func() {
			name, _ := mainPage.snapshotBrowser.GetLayout().GetFrontPage()
			loaded = name == util.LoadingContainerContentPage
		})
		return loaded
	})

	// focus the snapshot browser (next to the file browser) and start typing a filter
	press(tcell.KeyTab, 0)
	waitFor("snapshot browser focused", func() bool {
		focused := false
		testutil.OnUiThread(t, app, func() { focused = mainPage.snapshotBrowser.HasFocus() })
		return focused
	})
	press(tcell.KeyCtrlF, 0)
	waitFor("typing a filter", screenContains("Filter:"))

	// on the UI thread, while the app still runs (deferred after app.Stop, so it runs before it)
	defer testutil.OnUiThread(t, app, func() {
		if shortcut_helper.ShortcutsHidden() {
			shortcut_helper.ToggleShortcuts()
		}
	})

	// the '?' is typed into the filter (shown in the footer) instead of hiding the shortcuts
	press(tcell.KeyRune, '?')
	waitFor("'?' typed into the filter", screenContains("Filter: ?"))
	assert.False(t, shortcut_helper.ShortcutsHidden(), "'?' must not hide the shortcuts while typing a filter")
	assert.Equal(t, string(Main), frontPage())

	// after clearing the filter, '?' hides the shortcuts
	press(tcell.KeyEscape, 0)
	waitFor("filter cleared", func() bool { return !screenContains("Filter:")() })
	press(tcell.KeyRune, '?')
	waitFor("shortcuts hidden", shortcut_helper.ShortcutsHidden)
}

func TestPageTabs(t *testing.T) {
	app, _, _ := createUi(t.TempDir(), true)
	screen := tcell.NewSimulationScreen("UTF-8")
	app.SetScreen(screen)
	screen.SetSize(200, 30)
	testutil.RunApp(t, app)

	headerLine := func() string { return strings.SplitN(screenText(t, app, screen), "\n", 2)[0] }
	// column returns the column of the last text in the line: screenText has a rune per cell, and the header may
	// show a message with other characters than ASCII (e.g. the error of loading without ZFS)
	column := func(line string, text string) int {
		return utf8.RuneCountInString(line[:strings.LastIndex(line, text)])
	}
	shows := func(text string) func() bool {
		return func() bool { return strings.HasSuffix(headerLine(), text) }
	}
	styleAt := func(x int) (style tcell.Style) {
		testutil.OnUiThread(t, app, func() {
			app.ForceDraw()
			cells, _, _ := screen.GetContents()
			style = cells[x].Style
		})
		return style
	}

	// the title of the shown page padded to the longest one, so the tabs have the same width on all pages
	require.Eventually(t, shows(" 1 FILES     2  3  4 "), 3*time.Second, 20*time.Millisecond)
	line := headerLine()
	foreground, background, attributes := styleAt(column(line, "FILES")).Decompose()
	assert.Equal(t, theme.Colors.Header.PageIndicator, foreground)
	assert.Equal(t, theme.Colors.Header.PageIndicatorBackground, background)
	assert.NotZero(t, attributes&tcell.AttrBold, "bold")
	numberX := column(line, "3")
	_, background, _ = styleAt(numberX).Decompose()
	assert.Equal(t, theme.Colors.Header.PageNumberBackground, background)

	// the keys work as before
	screen.InjectKey(tcell.KeyRune, '2', tcell.ModNone)
	assert.Eventually(t, shows(" 1  2 DATASETS  3  4 "), 3*time.Second, 20*time.Millisecond)
	screen.InjectKey(tcell.KeyRune, '4', tcell.ModNone)
	assert.Eventually(t, shows(" 1  2  3  4 DISKS    "), 3*time.Second, 20*time.Millisecond)
	// there is no fifth page
	screen.InjectKey(tcell.KeyRune, '5', tcell.ModNone)
	testutil.WaitForEvents(t, app, screen)
	assert.True(t, shows(" 1  2  3  4 DISKS    ")())

	// the numbers can be clicked, the one below the mouse is highlighted
	line = headerLine()
	numberX = column(line, "3")
	screen.InjectMouse(numberX, 0, tcell.ButtonNone, tcell.ModNone)
	assert.Eventually(t, func() bool {
		_, background, _ := styleAt(numberX).Decompose()
		return background == theme.Colors.Header.PageNumberHoverBackground
	}, 3*time.Second, 20*time.Millisecond)
	screen.InjectMouse(numberX, 0, tcell.Button1, tcell.ModNone)
	screen.InjectMouse(numberX, 0, tcell.ButtonNone, tcell.ModNone)
	assert.Eventually(t, shows(" 1  2  3 POOLS     4 "), 3*time.Second, 20*time.Millisecond)
	assert.Equal(t, utf8.RuneCountInString(line), utf8.RuneCountInString(headerLine()), "the numbers behind the shown page stay where they are")
}

func TestRelativeTimesKey(t *testing.T) {
	app, mainPage, _ := createUi(t.TempDir(), true)
	screen := tcell.NewSimulationScreen("UTF-8")
	app.SetScreen(screen)
	screen.SetSize(150, 30)
	testutil.RunApp(t, app)
	t.Cleanup(func() {
		if util.IsRelativeTimes() {
			util.ToggleRelativeTimes()
		}
	})

	screen.InjectKey(tcell.KeyRune, 'T', tcell.ModNone)
	assert.Eventually(t, util.IsRelativeTimes, 2*time.Second, 10*time.Millisecond)
	screen.InjectKey(tcell.KeyRune, 'T', tcell.ModNone)
	assert.Eventually(t, func() bool { return !util.IsRelativeTimes() }, 2*time.Second, 10*time.Millisecond)

	// typed into a filter instead
	testutil.OnUiThread(t, app, func() { app.SetFocus(mainPage.fileBrowser.GetLayout()) })
	screen.InjectKey(tcell.KeyCtrlF, 0, tcell.ModNone)
	screen.InjectKey(tcell.KeyRune, 'T', tcell.ModNone)
	testutil.WaitForEvents(t, app, screen)
	assert.False(t, util.IsRelativeTimes())
	screen.InjectKey(tcell.KeyEscape, 0, tcell.ModNone)
}

// Dialogs and overlays (e.g. the histories) are pages of their own: T applies to their tables as well.
func TestRelativeTimesKeyInDialogs(t *testing.T) {
	app, mainPage, _ := createUi(t.TempDir(), true)
	screen := tcell.NewSimulationScreen("UTF-8")
	app.SetScreen(screen)
	screen.SetSize(150, 30)
	testutil.RunApp(t, app)
	t.Cleanup(func() {
		if util.IsRelativeTimes() {
			util.ToggleRelativeTimes()
		}
	})

	testutil.OnUiThread(t, app, func() {
		dialog.ShowDialogOnPages(app, mainPage.pages, dialog.NewSuccessDialog(app, "Done", "done"), nil)
	})
	screen.InjectKey(tcell.KeyRune, 'T', tcell.ModNone)
	assert.Eventually(t, util.IsRelativeTimes, 2*time.Second, 10*time.Millisecond)

	// typed into an input instead
	testutil.OnUiThread(t, app, func() {
		dialog.ShowDialogOnPages(app, mainPage.pages, dialog.NewTextInputDialog(app, "Input", "Input", "", "", nil), nil)
	})
	screen.InjectKey(tcell.KeyRune, 'T', tcell.ModNone)
	testutil.WaitForEvents(t, app, screen)
	assert.True(t, util.IsRelativeTimes())
}

// The dataset info is only shown on the dataset page: on the main page, Tab goes from the file browser straight
// to the snapshots, which use the whole height of the right column.
func TestMainPageHasNoDatasetInfo(t *testing.T) {
	app, mainPage, _ := createUi(t.TempDir(), true)
	assert.Equal(t, []FocusableUiComponent{mainPage.fileBrowser, mainPage.snapshotBrowser}, mainPage.focusableComponents())
	assert.Equal(t, 2, mainPage.split.GetItemCount())
	assert.Same(t, mainPage.snapshotBrowser.GetLayout(), mainPage.split.GetItem(1))
	// the overview is below the file browser
	assert.Same(t, mainPage.leftLayout, mainPage.split.GetItem(0))
	assert.Same(t, mainPage.fileBrowser.GetLayout(), mainPage.leftLayout.GetItem(0))
	assert.Same(t, mainPage.pathOverview.GetLayout(), mainPage.leftLayout.GetItem(1))
	app.Stop()
}

// o hides the overview below the file browser (making room for the files) and shows it again; the setting is
// remembered. While typing a filter, it is typed.
func TestOverviewKey(t *testing.T) {
	store := state.Load(filepath.Join(t.TempDir(), "state.json"))
	state.Current = store
	t.Cleanup(func() {
		_ = store.Flush()
		state.Current = nil
	})
	app, mainPage, _ := createUi(t.TempDir(), true)
	screen := tcell.NewSimulationScreen("UTF-8")
	app.SetScreen(screen)
	screen.SetSize(150, 40)
	testutil.RunApp(t, app)

	overviewHeight := func() int {
		var height int
		testutil.OnUiThread(t, app, func() {
			app.ForceDraw()
			_, _, _, height = mainPage.pathOverview.GetLayout().GetRect()
		})
		return height
	}
	shortcut := func() string {
		var name string
		testutil.OnUiThread(t, app, func() { name = mainPage.overviewShortcut().Name })
		return name
	}
	require.Equal(t, path_overview.Height, overviewHeight())
	assert.Equal(t, "Hide overview", shortcut())

	screen.InjectKey(tcell.KeyRune, 'o', tcell.ModNone)
	assert.Eventually(t, func() bool { return overviewHeight() == 0 }, 2*time.Second, 10*time.Millisecond)
	assert.True(t, store.Toggle(toggleHideOverview, false), "remembered")
	assert.Equal(t, "Show overview", shortcut())

	screen.InjectKey(tcell.KeyRune, 'o', tcell.ModNone)
	assert.Eventually(t, func() bool { return overviewHeight() == path_overview.Height }, 2*time.Second, 10*time.Millisecond)

	// typed into a filter instead
	testutil.OnUiThread(t, app, func() { app.SetFocus(mainPage.fileBrowser.GetLayout()) })
	screen.InjectKey(tcell.KeyCtrlF, 0, tcell.ModNone)
	screen.InjectKey(tcell.KeyRune, 'o', tcell.ModNone)
	testutil.WaitForEvents(t, app, screen)
	assert.Equal(t, path_overview.Height, overviewHeight())
	screen.InjectKey(tcell.KeyEscape, 0, tcell.ModNone)
}

// The boundaries between the panes of both pages can be dragged with the mouse (see uiutil.ResizableSplit).
func TestPageBoundariesCanBeDragged(t *testing.T) {
	app, mainPage, datasetPage := createUi(t.TempDir(), true)
	screen := tcell.NewSimulationScreen("UTF-8")
	app.SetScreen(screen)
	screen.SetSize(150, 40)
	app.EnableMouse(true)
	testutil.RunApp(t, app)

	rect := func(primitive tview.Primitive) (x, y, width, height int) {
		testutil.OnUiThread(t, app, func() {
			app.ForceDraw()
			x, y, width, height = primitive.GetRect()
		})
		return x, y, width, height
	}
	drag := func(fromX, fromY, toX, toY int) {
		screen.InjectMouse(fromX, fromY, tcell.Button1, tcell.ModNone)
		time.Sleep(50 * time.Millisecond)
		screen.InjectMouse(toX, toY, tcell.Button1, tcell.ModNone)
		time.Sleep(50 * time.Millisecond)
		screen.InjectMouse(toX, toY, tcell.ButtonNone, tcell.ModNone)
	}

	// files page: the file browser and the snapshots, side by side
	snapshotsX, _, _, _ := rect(mainPage.snapshotBrowser.GetLayout())
	drag(snapshotsX, 20, snapshotsX-30, 20)
	assert.Eventually(t, func() bool {
		x, _, _, _ := rect(mainPage.snapshotBrowser.GetLayout())
		return x == snapshotsX-30
	}, 2*time.Second, 20*time.Millisecond, "dragged to the left")

	// dataset page: the datasets next to the info above the snapshots
	screen.InjectKey(tcell.KeyRune, '2', tcell.ModNone)
	// Regression: the test waited for a width of the split, which it has before the page was ever drawn (the default
	// size of tview boxes), and measured where the boundaries are before the page was shown. And the info is sized by
	// its content, which is loaded after the page is shown: until then, the boundary below it moves.
	panes := func() [2][4]int {
		var rects [2][4]int
		testutil.OnUiThread(t, app, func() {
			app.ForceDraw()
			if !datasetPage.isInFront() {
				return
			}
			for i, primitive := range []tview.Primitive{datasetPage.datasetInfo.GetLayout(), datasetPage.snapshotBrowser.GetLayout()} {
				x, y, width, height := primitive.GetRect()
				rects[i] = [4]int{x, y, width, height}
			}
		})
		return rects
	}
	settledSince, last := time.Now(), panes()
	require.Eventually(t, func() bool {
		current := panes()
		if current != last || current == [2][4]int{} {
			settledSince, last = time.Now(), current
		}
		return time.Since(settledSince) >= 300*time.Millisecond
	}, 5*time.Second, 20*time.Millisecond, "the dataset page is shown and its panes stay in place")
	infoX, _, _, _ := rect(datasetPage.datasetInfo.GetLayout())
	_, snapshotsY, _, _ := rect(datasetPage.snapshotBrowser.GetLayout())
	drag(infoX+10, snapshotsY, infoX+10, snapshotsY+5)
	assert.Eventually(t, func() bool {
		_, y, _, _ := rect(datasetPage.snapshotBrowser.GetLayout())
		return y == snapshotsY+5
	}, 2*time.Second, 20*time.Millisecond, "the info above the snapshots dragged down")
	drag(infoX, 20, infoX+10, 20)
	assert.Eventually(t, func() bool {
		x, _, _, _ := rect(datasetPage.datasetInfo.GetLayout())
		return x == infoX+10
	}, 2*time.Second, 20*time.Millisecond, "the datasets dragged to the right")
}

// Regression: dialogs of components (e.g. the action dialog of the file browser) were shown on the component's own
// pages, covering only its area: the snapshot list next to it could still be clicked, and keys of the page (e.g.
// ⭾) still worked. They are shown on the pages of the application now, and are modal for the whole screen.
func TestComponentDialogsAreModal(t *testing.T) {
	app, mainPage, _ := createUi(t.TempDir(), true)
	screen := tcell.NewSimulationScreen("UTF-8")
	app.SetScreen(screen)
	screen.SetSize(150, 40)
	app.EnableMouse(true)
	testutil.RunApp(t, app)

	frontPage := func() string {
		var name string
		testutil.OnUiThread(t, app, func() { name, _ = mainPage.pages.GetFrontPage() })
		return name
	}
	snapshotsFocused := func() bool {
		var focused bool
		testutil.OnUiThread(t, app, func() { focused = mainPage.snapshotBrowser.HasFocus() })
		return focused
	}

	// a dialog of the file browser (the column dialog, like its action dialog)
	testutil.OnUiThread(t, app, func() { app.SetFocus(mainPage.fileBrowser.GetLayout()) })
	screen.InjectKey(tcell.KeyF2, 0, tcell.ModNone)
	require.Eventually(t, func() bool { return frontPage() != string(Main) }, 2*time.Second, 10*time.Millisecond,
		"shown on the pages of the application")

	// ⭾ does not move the focus to the snapshot list behind the dialog
	screen.InjectKey(tcell.KeyTab, 0, tcell.ModNone)
	testutil.WaitForEvents(t, app, screen)
	assert.False(t, snapshotsFocused())

	// neither does a click on it
	var x, y, width, height int
	testutil.OnUiThread(t, app, func() { x, y, width, height = mainPage.snapshotBrowser.GetLayout().GetRect() })
	screen.InjectMouse(x+width-3, y+height-3, tcell.Button1, tcell.ModNone)
	screen.InjectMouse(x+width-3, y+height-3, tcell.ButtonNone, tcell.ModNone)
	testutil.WaitForEvents(t, app, screen)
	assert.False(t, snapshotsFocused())
	assert.NotEqual(t, string(Main), frontPage(), "the dialog is still open")

	// closed, the focus is back in the file browser
	screen.InjectKey(tcell.KeyEscape, 0, tcell.ModNone)
	require.Eventually(t, func() bool { return frontPage() == string(Main) }, 2*time.Second, 10*time.Millisecond)
	var fileBrowserFocused bool
	testutil.OnUiThread(t, app, func() { fileBrowserFocused = mainPage.fileBrowser.HasFocus() })
	assert.True(t, fileBrowserFocused)
}

func TestCopiedMessage(t *testing.T) {
	assert.Equal(t, "Copied: /tank/data", copiedMessage("/tank/data").Message)
	assert.Equal(t, "Copied 2 lines", copiedMessage("a\nb").Message)
	long := copiedMessage(strings.Repeat("x", 100)).Message
	assert.Equal(t, "Copied: "+strings.Repeat("x", copiedMessageLength-1)+"…", long, "shortened")
}
