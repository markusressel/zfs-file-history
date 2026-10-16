package dialog

import (
	"testing"
	"time"
	"zfs-file-history/internal/testutil"
	"zfs-file-history/internal/ui/theme"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newPreviewDialogTest shows a PreviewDialog on pages of a running app.
func newPreviewDialogTest(t *testing.T) (*tview.Application, tcell.SimulationScreen, *tview.Pages, *PreviewDialog) {
	app, screen := testutil.NewApp()
	pages := tview.NewPages().AddPage("main", tview.NewBox(), true, true)
	testutil.StartApp(t, app, pages)
	var d *PreviewDialog
	testutil.OnUiThread(t, app, func() {
		d = NewPreviewDialog(app, " 💥 Destroy Dataset ", "Checking what destroying tank/data would destroy. Nothing is destroyed yet.")
		ShowDialogOnPages(app, pages, d, nil)
	})
	return app, screen, pages, d
}

func TestPreviewDialog_ShowsWhatIsChecked(t *testing.T) {
	app, screen, _, d := newPreviewDialogTest(t)

	text := testutil.ScreenText(t, app, screen)
	assert.Contains(t, text, "Destroy Dataset")
	assert.Contains(t, text, "Checking what destroying tank/data would destroy.")
	assert.Contains(t, text, "Cancel")
	testutil.OnUiThread(t, app, func() {
		assert.True(t, d.GetLayout().HasFocus())
	})
	// the red border of the confirmation that follows
	x, y, found := testutil.FindText(t, app, screen, "Destroy Dataset ")
	require.True(t, found)
	testutil.OnUiThread(t, app, func() {
		// the border right of the title
		column := x + len("Destroy Dataset")
		for border, _, _ := screen.Get(column, y); border == " "; border, _, _ = screen.Get(column, y) {
			column++
		}
		border, style, _ := screen.Get(column, y)
		foreground, _, _ := style.Decompose()
		assert.Equal(t, "═", border, "focused")
		assert.Equal(t, theme.Colors.Dialog.DangerBorder, foreground)
	})
}

func TestPreviewDialog_Done(t *testing.T) {
	app, _, pages, d := newPreviewDialogTest(t)

	testutil.OnUiThread(t, app, func() {
		assert.True(t, d.Done(), "not cancelled")
		assert.False(t, d.Done(), "closed already")
	})

	testutil.WaitForNoPage(t, app, pages, string(PreviewDialogPage), 2*time.Second)
}

// Cancelled, the result of the check is dropped: Done returns false.
func TestPreviewDialog_Cancel(t *testing.T) {
	for name, key := range map[string]tcell.Key{"Esc": tcell.KeyEscape, "Enter on Cancel": tcell.KeyEnter} {
		t.Run(name, func(t *testing.T) {
			app, screen, pages, d := newPreviewDialogTest(t)

			testutil.Press(t, app, screen, key, 0)

			testutil.WaitForNoPage(t, app, pages, string(PreviewDialogPage), 2*time.Second)
			testutil.OnUiThread(t, app, func() { assert.False(t, d.Done()) })
		})
	}
}
