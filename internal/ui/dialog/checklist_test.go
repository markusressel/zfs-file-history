package dialog

import (
	"testing"
	"time"
	"zfs-file-history/internal/testutil"

	"github.com/rivo/tview"
	"github.com/stretchr/testify/assert"
)

// Regression: a click on an item of the checklist checked it, also while it was not highlighted (e.g. while an
// option had the focus). Only a click on the highlighted item does, like Space; a click on another one selects it.
func TestChecklist_ClickChecksOnlyTheHighlighted(t *testing.T) {
	app, screen := testutil.NewApp()
	pages := tview.NewPages().AddPage("main", tview.NewBox(), true, true)
	testutil.StartApp(t, app, pages)
	items := []*ChecklistItem{{Name: "first item"}, {Name: "second item"}}
	d := NewSelectionDialogWithChecklist(app, "ChecklistTest", " Title ", "Description", items,
		[]*DialogOption{{Id: 1, Name: "Do it"}, {Id: DialogCloseActionId, Name: "Close"}}, nil, nil)
	testutil.OnUiThread(t, app, func() { ShowDialogOnPages(app, pages, d, nil) })
	testutil.WaitForText(t, app, screen, "second item", 2*time.Second)
	checked := func() (states []bool) {
		testutil.OnUiThread(t, app, func() {
			for _, item := range items {
				states = append(states, item.Checked)
			}
		})
		return states
	}

	// the options have the focus: the first item is selected, but not highlighted
	testutil.ClickText(t, app, screen, "first item")
	assert.Equal(t, []bool{false, false}, checked())
	testutil.ClickText(t, app, screen, "second item")
	assert.Equal(t, []bool{false, false}, checked(), "selected, not checked")

	testutil.ClickText(t, app, screen, "second item")
	assert.Equal(t, []bool{false, true}, checked())
	testutil.ClickText(t, app, screen, "first item")
	assert.Equal(t, []bool{false, true}, checked())
}
