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
	screen := tcell.NewSimulationScreen("UTF-8")
	app.SetScreen(screen)
	screen.SetSize(200, 30)
	go func() { _ = app.Run() }()
	defer app.Stop()

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
	waitForText("Folder history")
	waitForText("Hide overview")
	typeText("go")
	waitForText("Go to Datasets")
	screen.InjectKey(tcell.KeyBackspace2, 0, tcell.ModNone)
	screen.InjectKey(tcell.KeyBackspace2, 0, tcell.ModNone)

	relative := util.IsRelativeTimes()
	defer testutil.OnUiThread(t, app, func() {
		if util.IsRelativeTimes() != relative {
			util.ToggleRelativeTimes()
		}
	})
	typeText("time f")
	waitForText("Time format")
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
	screen := tcell.NewSimulationScreen("UTF-8")
	app.SetScreen(screen)
	screen.SetSize(200, 30)
	go func() { _ = app.Run() }()
	defer app.Stop()

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
