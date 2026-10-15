package testutil

import (
	"strings"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"github.com/stretchr/testify/require"
)

// waitTick is how often the helpers that wait check again.
const waitTick = 10 * time.Millisecond

// WaitForText waits until text is shown on the screen of the running app (see ScreenText, which draws), failing the
// test after timeout.
func WaitForText(t testing.TB, app *tview.Application, screen tcell.SimulationScreen, text string, timeout time.Duration) {
	t.Helper()
	require.Eventually(t, func() bool { return strings.Contains(ScreenText(t, app, screen), text) }, timeout, waitTick,
		"text %q is shown", text)
}

// WaitForNoText waits until text is not shown on the screen of the running app anymore, see WaitForText.
func WaitForNoText(t testing.TB, app *tview.Application, screen tcell.SimulationScreen, text string, timeout time.Duration) {
	t.Helper()
	require.Eventually(t, func() bool { return !strings.Contains(ScreenText(t, app, screen), text) }, timeout, waitTick,
		"text %q is not shown", text)
}

// HasPage returns whether pages has the page (e.g. a dialog), read on the UI thread of the running app.
func HasPage(t testing.TB, app *tview.Application, pages *tview.Pages, name string) bool {
	t.Helper()
	shown := false
	OnUiThread(t, app, func() { shown = pages.HasPage(name) })
	return shown
}

// WaitForPage waits until pages has the page (e.g. a dialog shown in the background), failing the test after timeout.
func WaitForPage(t testing.TB, app *tview.Application, pages *tview.Pages, name string, timeout time.Duration) {
	t.Helper()
	require.Eventually(t, func() bool { return HasPage(t, app, pages, name) }, timeout, waitTick, "page %s", name)
}

// WaitForNoPage waits until pages does not have the page anymore (e.g. a closed dialog), see WaitForPage.
func WaitForNoPage(t testing.TB, app *tview.Application, pages *tview.Pages, name string, timeout time.Duration) {
	t.Helper()
	require.Eventually(t, func() bool { return !HasPage(t, app, pages, name) }, timeout, waitTick, "no page %s", name)
}
