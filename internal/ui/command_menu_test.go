package ui

import (
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
	app, mainPage, _ := newUi(t, t.TempDir())
	screen := testutil.UseSimulationScreen(app, testutil.WithSize(200, 30))
	testutil.RunApp(t, app)

	frontPage := func() (name string) {
		testutil.OnUiThread(t, app, func() { name, _ = mainPage.pages.GetFrontPage() })
		return name
	}
	waitForText := func(text string) { testutil.WaitForText(t, app, screen, text, 3*time.Second) }

	testutil.Press(t, app, screen, tcell.KeyRune, ':')
	require.Eventually(t, func() bool { return frontPage() == string(dialog.CommandMenuPage) }, 3*time.Second, 20*time.Millisecond)
	typeText := func(text string) {
		testutil.Type(t, app, screen, text)
	}
	// of the file browser (in the empty folder: the folder), of the page and global (filtered, as they are listed
	// last)
	clear := func(runes int) {
		for range runes {
			testutil.Press(t, app, screen, tcell.KeyBackspace2, 0)
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
	testutil.Press(t, app, screen, tcell.KeyEnter, 0)

	assert.Eventually(t, func() bool { return frontPage() == string(Main) }, 3*time.Second, 20*time.Millisecond)
	assert.Eventually(t, func() bool {
		toggled := false
		testutil.OnUiThread(t, app, func() { toggled = util.IsRelativeTimes() != relative })
		return toggled
	}, 3*time.Second, 20*time.Millisecond, "the time format was toggled")
}

// ':' is typed into a filter instead of opening the command menu
func TestCommandMenuKeyIsTypedIntoAFilter(t *testing.T) {
	app, mainPage, _ := newUi(t, t.TempDir())
	screen := testutil.UseSimulationScreen(app, testutil.WithSize(200, 30))
	testutil.RunApp(t, app)

	testutil.Press(t, app, screen, tcell.KeyCtrlF, 0)
	testutil.WaitForText(t, app, screen, "Filter:", 3*time.Second)
	testutil.Press(t, app, screen, tcell.KeyRune, ':')
	testutil.WaitForText(t, app, screen, "Filter: :", 3*time.Second)
	testutil.OnUiThread(t, app, func() {
		name, _ := mainPage.pages.GetFrontPage()
		assert.Equal(t, string(Main), name)
	})
}

// The commands of the components without the focus are listed as well, after the page; running one focuses its
// component first, like after pressing its key there.
func TestCommandSections_OtherComponents(t *testing.T) {
	app, mainPage, _ := newUi(t, t.TempDir())
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
