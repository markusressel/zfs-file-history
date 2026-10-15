package ui

import (
	"strings"
	"testing"
	"time"
	"zfs-file-history/internal/testutil"
	"zfs-file-history/internal/ui/dialog"
	"zfs-file-history/internal/ui/util"

	"github.com/gdamore/tcell/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ':' opens the command menu with the commands of the focused component, the page and the global ones; Enter runs
// the selected one
func TestCommandMenuKey(t *testing.T) {
	app, mainPage, _ := createUi(t.TempDir(), true)
	screen := testutil.UseSimulationScreen(app, testutil.WithSize(200, 30))
	testutil.RunApp(t, app)

	frontPage := func() (name string) {
		testutil.OnUiThread(t, app, func() { name, _ = mainPage.pages.GetFrontPage() })
		return name
	}
	waitForText := func(text string) {
		require.Eventually(t, func() bool { return strings.Contains(screenText(t, app, screen), text) },
			3*time.Second, 20*time.Millisecond, "expected %q on the screen", text)
	}

	screen.InjectKey(tcell.KeyRune, ':', tcell.ModNone)
	require.Eventually(t, func() bool { return frontPage() == string(dialog.CommandMenuPage) }, 3*time.Second, 20*time.Millisecond)
	typeText := func(text string) {
		for _, r := range text {
			screen.InjectKey(tcell.KeyRune, r, tcell.ModNone)
		}
	}
	// of the file browser (in the empty folder: the folder), of the page and global (filtered, as they are listed
	// last)
	clear := func(runes int) {
		for range runes {
			screen.InjectKey(tcell.KeyBackspace2, 0, tcell.ModNone)
		}
	}
	waitForText("Files")
	waitForText("Folder history")
	typeText("overv")
	waitForText("Files page")
	// a switch, named by what it switches
	waitForText("● Show overview")
	clear(5)
	typeText("go to")
	waitForText("Global")
	waitForText("Go to Datasets")
	clear(5)

	relative := util.IsRelativeTimes()
	defer testutil.OnUiThread(t, app, func() {
		if util.IsRelativeTimes() != relative {
			util.ToggleRelativeTimes()
		}
	})
	typeText("relative t")
	waitForText("Relative times")
	screen.InjectKey(tcell.KeyEnter, 0, tcell.ModNone)

	assert.Eventually(t, func() bool { return frontPage() == string(Main) }, 3*time.Second, 20*time.Millisecond)
	assert.Eventually(t, func() bool {
		toggled := false
		testutil.OnUiThread(t, app, func() { toggled = util.IsRelativeTimes() != relative })
		return toggled
	}, 3*time.Second, 20*time.Millisecond, "the time format was toggled")
}

// ':' is typed into a filter instead of opening the command menu
func TestCommandMenuKeyIsTypedIntoAFilter(t *testing.T) {
	app, mainPage, _ := createUi(t.TempDir(), true)
	screen := testutil.UseSimulationScreen(app, testutil.WithSize(200, 30))
	testutil.RunApp(t, app)

	screen.InjectKey(tcell.KeyCtrlF, 0, tcell.ModNone)
	require.Eventually(t, func() bool { return strings.Contains(screenText(t, app, screen), "Filter:") },
		3*time.Second, 20*time.Millisecond)
	screen.InjectKey(tcell.KeyRune, ':', tcell.ModNone)
	require.Eventually(t, func() bool { return strings.Contains(screenText(t, app, screen), "Filter: :") },
		3*time.Second, 20*time.Millisecond)
	testutil.OnUiThread(t, app, func() {
		name, _ := mainPage.pages.GetFrontPage()
		assert.Equal(t, string(Main), name)
	})
}

// The commands of the components without the focus are listed as well, after the page; running one focuses its
// component first, like after pressing its key there.
func TestCommandSections_OtherComponents(t *testing.T) {
	app, mainPage, _ := createUi(t.TempDir(), true)
	testutil.UseSimulationScreen(app)
	testutil.RunApp(t, app)

	var titles []string
	var unfocused []bool
	var run func()
	testutil.OnUiThread(t, app, func() {
		mainPage.focusComponent("Files")
		for _, section := range mainPage.commandSections() {
			titles = append(titles, section.Title)
			unfocused = append(unfocused, section.Unfocused)
			if section.Title != "Snapshots" {
				continue
			}
			for _, command := range section.Commands {
				// not one that opens a dialog, which would take the focus
				if command.Name == "Filter" {
					run = command.Run
				}
			}
		}
	})
	assert.Equal(t, []string{"Files", "Files page", "Snapshots"}, titles)
	assert.Equal(t, []bool{false, false, true}, unfocused)
	require.NotNil(t, run, "the filter of the snapshots")

	var focused bool
	testutil.OnUiThread(t, app, func() {
		run()
		focused = mainPage.snapshotBrowser.HasFocus()
	})
	assert.True(t, focused, "the snapshots got the focus")
}
