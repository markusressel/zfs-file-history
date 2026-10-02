package ui

import (
	"strings"
	"testing"
	"time"
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
	assert.Equal(t, Main, adjacentPage(two, HelpDialog, false))
}

// onUiThreadT runs f on the UI thread and waits for it, failing the test instead of hanging on a deadlock.
func onUiThreadT(t *testing.T, app *tview.Application, f func()) {
	done := make(chan struct{})
	go func() {
		app.QueueUpdate(f)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for the UI thread (deadlock?)")
	}
}

// screenText returns the text currently drawn on the screen, with non-breaking spaces
// (used within shortcut entries) replaced by regular spaces.
// It reads the screen on the UI thread, which is where the application draws to it.
func screenText(t *testing.T, app *tview.Application, screen tcell.SimulationScreen) string {
	var cells []tcell.SimCell
	var width, height int
	onUiThreadT(t, app, func() { cells, width, height = screen.GetContents() })

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
	go func() { _ = app.Run() }()
	defer app.Stop()

	// Like in real usage, let the hidden dataset page finish loading (and selecting) its datasets
	// before switching to it. Otherwise the selection while it is visible would update its shortcuts anyway.
	// Once a dataset is selected, the path changes from the start path to the dataset's mount path.
	// Without ZFS, loading fails and nothing is selected, which is just as fine for this test.
	datasetsSelected := func() bool {
		selected := false
		onUiThreadT(t, app, func() { selected = datasetPage.datasetBrowser.GetPath() != startPath })
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

	// shown by the file browser for both the header and data rows, but not by the dataset browser
	const mainPageShortcut = "[PgUp]: Page up"
	const datasetPageShortcut = "[u]: Show unmounted"

	waitForText(mainPageShortcut)

	// dataset page, without any further key press
	screen.InjectKey(tcell.KeyTab, 0, tcell.ModNone)
	waitForText(datasetPageShortcut)
	assert.NotContains(t, screenText(t, app, screen), mainPageShortcut)

	// and back, in reverse
	screen.InjectKey(tcell.KeyBacktab, 0, tcell.ModNone)
	waitForText(mainPageShortcut)
	assert.NotContains(t, screenText(t, app, screen), datasetPageShortcut)
}

func TestHelpKeyIsTypedIntoAFilter(t *testing.T) {
	app, mainPage, _ := createUi(t.TempDir(), true)
	screen := tcell.NewSimulationScreen("UTF-8")
	app.SetScreen(screen)
	screen.SetSize(200, 30)
	go func() { _ = app.Run() }()
	defer app.Stop()

	// Key events and queued updates are processed from different channels, so instead of waiting for the
	// event loop, each step waits for its visible effect.
	press := func(key tcell.Key, r rune) {
		screen.InjectKey(key, r, tcell.ModNone)
	}
	frontPage := func() (name string) {
		onUiThreadT(t, app, func() { name, _ = mainPage.pages.GetFrontPage() })
		return name
	}
	waitFor := func(message string, condition func() bool) {
		require.Eventually(t, condition, 10*time.Second, 20*time.Millisecond, message)
	}
	screenContains := func(text string) func() bool {
		return func() bool { return strings.Contains(screenText(t, app, screen), text) }
	}

	// While loading, the snapshot browser shows a loading view, which has the focus instead of the table.
	// Without ZFS, loading fails quickly.
	waitFor("snapshots loaded", func() bool {
		loaded := false
		onUiThreadT(t, app, func() {
			name, _ := mainPage.snapshotBrowser.GetLayout().GetFrontPage()
			loaded = name == util.LoadingContainerContentPage
		})
		return loaded
	})

	// focus the snapshot browser and start typing a filter
	press(tcell.KeyCtrlN, 0)
	press(tcell.KeyCtrlN, 0)
	waitFor("snapshot browser focused", func() bool {
		focused := false
		onUiThreadT(t, app, func() { focused = mainPage.snapshotBrowser.HasFocus() })
		return focused
	})
	press(tcell.KeyCtrlF, 0)
	waitFor("typing a filter", screenContains("Filter:"))

	// the '?' is typed into the filter (shown in the footer) instead of opening the help
	press(tcell.KeyRune, '?')
	waitFor("'?' typed into the filter", screenContains("Filter: ?"))
	assert.Equal(t, string(Main), frontPage(), "'?' must not open the help while typing a filter")

	// after clearing the filter, '?' opens the help again
	press(tcell.KeyEscape, 0)
	waitFor("filter cleared", func() bool { return !screenContains("Filter:")() })
	press(tcell.KeyRune, '?')
	waitFor("help opened", func() bool { return frontPage() == string(HelpDialog) })
}

func TestPageIndicator(t *testing.T) {
	app, _, _ := createUi(t.TempDir(), true)
	screen := tcell.NewSimulationScreen("UTF-8")
	app.SetScreen(screen)
	screen.SetSize(200, 30)
	go func() { _ = app.Run() }()
	defer app.Stop()

	shows := func(text string) func() bool {
		return func() bool { return strings.Contains(screenText(t, app, screen), text) }
	}

	assert.Eventually(t, shows("FILES    1/2"), 3*time.Second, 20*time.Millisecond)

	// bold, in the theme's colors
	var style tcell.Style
	onUiThreadT(t, app, func() {
		cells, width, _ := screen.GetContents()
		for x := 0; x < width; x++ {
			if runes := cells[x].Runes; len(runes) > 0 && runes[0] == 'F' && x+1 < width && cells[x+1].Runes[0] == 'I' {
				style = cells[x].Style
			}
		}
	})
	foreground, background, attributes := style.Decompose()
	assert.Equal(t, theme.Colors.Header.PageIndicator, foreground)
	assert.Equal(t, theme.Colors.Header.PageIndicatorBackground, background)
	assert.NotZero(t, attributes&tcell.AttrBold, "bold")

	screen.InjectKey(tcell.KeyTab, 0, tcell.ModNone)
	assert.Eventually(t, shows("DATASETS 2/2"), 3*time.Second, 20*time.Millisecond)
	assert.False(t, shows("FILES    1/2")())
	screen.InjectKey(tcell.KeyBacktab, 0, tcell.ModNone)
	assert.Eventually(t, shows("FILES    1/2"), 3*time.Second, 20*time.Millisecond)
}

func TestPageIndicatorWidth(t *testing.T) {
	assert.Equal(t, "FILES    1/2", formatPageIndicator("Files", 8, 1, 2), "the position is right-aligned")
	assert.Equal(t, "DATASETS 2/2", formatPageIndicator("Datasets", 8, 2, 2))
	assert.Equal(t, len("Datasets"), pageTitleWidth())
}
