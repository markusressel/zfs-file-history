package ui

import (
	"fmt"
	"testing"
	"zfs-file-history/internal/testutil"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"github.com/stretchr/testify/assert"
)

// the shortcuts at the bottom differ by the focused component: cycling the focus must not resize the components
// above them, e.g. move the snapshot list
func TestDatasetPage_CyclingFocusKeepsTheLayout(t *testing.T) {
	for _, width := range []int{80, 100, 120, 160, 200} {
		t.Run(fmt.Sprintf("width %d", width), func(t *testing.T) {
			app := tview.NewApplication()
			screen := tcell.NewSimulationScreen("UTF-8")
			app.SetScreen(screen)
			screen.SetSize(width, 50)
			page := NewDatasetPage(app, "/")
			app.SetRoot(page.layout, true)
			go func() { _ = app.Run() }()
			defer app.Stop()

			snapshotsRect := func() [4]int {
				var rect [4]int
				testutil.OnUiThread(t, app, func() {
					app.ForceDraw()
					x, y, w, h := page.snapshotBrowser.GetLayout().GetRect()
					rect = [4]int{x, y, w, h}
				})
				return rect
			}

			testutil.OnUiThread(t, app, func() {
				page.datasetBrowser.Focus()
				page.refreshShortcutMap()
			})
			initial := snapshotsRect()
			for range page.focusableComponents() {
				testutil.OnUiThread(t, app, func() { page.CycleFocus(false) })
				assert.Equal(t, initial, snapshotsRect())
			}
		})
	}
}
