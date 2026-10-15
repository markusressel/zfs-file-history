package ui

import (
	"fmt"
	"testing"
	"zfs-file-history/internal/testutil"
	"zfs-file-history/internal/ui/pool_activity"
	"zfs-file-history/internal/ui/status_message"

	"github.com/stretchr/testify/assert"
)

// the shortcuts at the bottom differ by the focused component: cycling the focus must not change the height of the
// shortcut map, which would resize the components above it, e.g. move the snapshot list
func TestDatasetPage_CyclingFocusKeepsTheLayout(t *testing.T) {
	for _, width := range []int{80, 100, 120, 160, 200} {
		t.Run(fmt.Sprintf("width %d", width), func(t *testing.T) {
			app, _ := testutil.NewApp(testutil.WithSize(width, 50))
			page := NewDatasetPage(app, status_message.NewCenter(app), pool_activity.NewMonitor(app), "/")
			testutil.StartApp(t, app, page.layout)

			// each step on the UI thread at once, so loading the datasets (a failed load, too), which changes the
			// shortcuts of the dataset browser, cannot happen in between
			testutil.OnUiThread(t, app, func() {
				app.ForceDraw()
				page.datasetBrowser.Focus()
				page.refreshShortcutMap()
				for range page.focusableComponents() {
					before := page.shortcutMap.CalculateHeightFromTerminal()
					page.CycleFocus(false)
					assert.Equal(t, before, page.shortcutMap.CalculateHeightFromTerminal())
				}
			})
		})
	}
}
