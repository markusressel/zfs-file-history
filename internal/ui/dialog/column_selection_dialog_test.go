package dialog

import (
	"slices"
	"testing"
	"time"
	"zfs-file-history/internal/testutil"
	"zfs-file-history/internal/ui/table"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestComputeAvailableColumns(t *testing.T) {
	all := []*table.Column{
		{Id: table.ColumnId(1), Title: "Col 1"},
		{Id: table.ColumnId(2), Title: "Col 2"},
		{Id: table.ColumnId(3), Title: "Col 3"},
	}
	active := []*table.Column{
		{Id: table.ColumnId(2), Title: "Col 2"},
	}

	available := computeAvailableColumns(all, active)
	assert.Len(t, available, 2)
	assert.Equal(t, table.ColumnId(1), available[0].Id)
	assert.Equal(t, table.ColumnId(3), available[1].Id)
}

func TestColumnSelectionDialog(t *testing.T) {
	app := tview.NewApplication()
	all := []*table.Column{
		{Id: table.ColumnId(1), Title: "Col 1"},
		{Id: table.ColumnId(2), Title: "Col 2"},
	}
	active := []*table.Column{
		{Id: table.ColumnId(1), Title: "Col 1"},
	}

	d := NewColumnSelectionDialog(app, "Column Selection", all, active, func(activeColumns []*table.Column) {})
	assert.Equal(t, "ColumnSelectionDialog", d.GetName())
	assert.NotNil(t, d.GetLayout())
	assert.NotNil(t, d.GetActionChannel())
}

func TestColumnSelectionDialog_Actions(t *testing.T) {
	app := tview.NewApplication()
	all := []*table.Column{
		{Id: table.ColumnId(1), Title: "Col 1"},
		{Id: table.ColumnId(2), Title: "Col 2"},
		{Id: table.ColumnId(3), Title: "Col 3"},
	}
	active := []*table.Column{
		{Id: table.ColumnId(1), Title: "Col 1"},
		{Id: table.ColumnId(2), Title: "Col 2"},
	}

	changeCalled := false
	d := NewColumnSelectionDialog(app, "Column Selection", all, active, func(activeColumns []*table.Column) {
		changeCalled = true
	})

	// Test Close
	go func() {
		d.Close()
	}()
	action := <-d.GetActionChannel()
	assert.Equal(t, DialogCloseActionId, action)

	// Test add selected available column
	d.addSelectedAvailableColumn()
	assert.True(t, changeCalled)
	assert.Len(t, d.activeColumns, 3)

	// Reset changeCalled
	changeCalled = false

	// Test remove selected active column
	d.activeTable.Select(1, 0)
	d.removeSelectedActiveColumn()
	assert.True(t, changeCalled)
	assert.Len(t, d.activeColumns, 2)

	// Reset changeCalled
	changeCalled = false

	// Test move active column up/down
	d.activeTable.Select(1, 0)
	d.moveActiveColumnUp()
	assert.True(t, changeCalled)

	changeCalled = false
	d.moveActiveColumnDown()
	assert.True(t, changeCalled)
}

func TestColumnSelectionDialog_Reset(t *testing.T) {
	app := tview.NewApplication()
	all := []*table.Column{
		{Id: table.ColumnId(1), Title: "Col 1"},
		{Id: table.ColumnId(2), Title: "Col 2"},
		{Id: table.ColumnId(3), Title: "Col 3"},
	}
	pressR := func(d *ColumnSelectionDialog) *tcell.EventKey {
		return d.captureInput(tcell.NewEventKey(tcell.KeyRune, 'r', tcell.ModNone))
	}

	// without a reset function, "r" is not handled and not shown
	d := NewColumnSelectionDialog(app, "Columns", all, all[:1], nil)
	assert.NotNil(t, pressR(d))
	assert.NotContains(t, shortcutNames(d), "Reset")

	resetCalls := 0
	d.SetResetFunc(func() []*table.Column {
		resetCalls++
		return []*table.Column{all[2], all[0]}
	})
	assert.Contains(t, shortcutNames(d), "Reset")

	assert.Nil(t, pressR(d))
	assert.Equal(t, 1, resetCalls)
	assert.Equal(t, []*table.Column{all[2], all[0]}, d.activeColumns)
	assert.Equal(t, []*table.Column{all[1]}, d.availableColumns)
	assert.Equal(t, "Col 3", d.activeTable.GetCell(0, 0).Text)
}

func shortcutNames(d *ColumnSelectionDialog) []string {
	var names []string
	for _, entry := range d.shortcutMap.ShortCutEntries {
		names = append(names, entry.Name)
	}
	return names
}

func TestColumnSelectionDialog_ShortcutsFit(t *testing.T) {
	app := tview.NewApplication()
	all := []*table.Column{{Id: table.ColumnId(1), Title: "Col 1"}}
	d := NewColumnSelectionDialog(app, "Columns", all, all, nil)
	d.SetResetFunc(func() []*table.Column { return all })

	// the content width of the dialog, see CalculateDialogSize: the dialog frame has a border,
	// and the content is as wide as columnSelectionMinContentWidth
	width := columnSelectionMinContentWidth + 6 - 2
	for _, focusActive := range []bool{true, false} {
		d.focusActive = focusActive
		d.updateShortcutMap()
		assert.LessOrEqual(t, d.shortcutMap.CalculateHeightForWidth(width), columnSelectionShortcutLines,
			"shortcuts with focus on the active side: %v", focusActive)
	}
}

// Enter moves the selected column to the other side, on both sides; ←/→, ⭾ and shift+⭾ switch the side
func TestColumnSelectionDialog_Keys(t *testing.T) {
	app := tview.NewApplication()
	screen := tcell.NewSimulationScreen("UTF-8")
	app.SetScreen(screen)
	screen.SetSize(100, 30)
	pages := tview.NewPages().AddPage("main", tview.NewBox(), true, true)
	app.SetRoot(pages, true)
	go func() { _ = app.Run() }()
	t.Cleanup(app.Stop)

	all := []*table.Column{{Id: 1, Title: "Col 1"}, {Id: 2, Title: "Col 2"}, {Id: 3, Title: "Col 3"}}
	var d *ColumnSelectionDialog
	testutil.OnUiThread(t, app, func() {
		d = NewColumnSelectionDialog(app, "Column Selection", all, all[:2], func([]*table.Column) {})
		ShowDialogOnPages(app, pages, d, nil)
	})
	state := func() (active []*table.Column, focusActive bool) {
		testutil.OnUiThread(t, app, func() { active, focusActive = slices.Clone(d.activeColumns), d.focusActive })
		return active, focusActive
	}
	press := func(key tcell.Key) {
		screen.InjectKey(key, 0, tcell.ModNone)
		testutil.OnUiThread(t, app, func() {})
	}

	// the first active column moves to the available ones
	press(tcell.KeyEnter)
	require.EventuallyWithT(t, func(c *assert.CollectT) {
		active, _ := state()
		assert.Equal(c, []*table.Column{all[1]}, active)
	}, 3*time.Second, 10*time.Millisecond)

	press(tcell.KeyTab)
	require.EventuallyWithT(t, func(c *assert.CollectT) {
		_, focusActive := state()
		assert.False(c, focusActive)
	}, 3*time.Second, 10*time.Millisecond)
	// the first available column (Col 1) moves back
	press(tcell.KeyEnter)
	require.EventuallyWithT(t, func(c *assert.CollectT) {
		active, _ := state()
		assert.Equal(c, []*table.Column{all[1], all[0]}, active)
	}, 3*time.Second, 10*time.Millisecond)

	press(tcell.KeyBacktab)
	require.EventuallyWithT(t, func(c *assert.CollectT) {
		_, focusActive := state()
		assert.True(c, focusActive)
	}, 3*time.Second, 10*time.Millisecond)
	press(tcell.KeyTab)
	press(tcell.KeyLeft)
	require.EventuallyWithT(t, func(c *assert.CollectT) {
		_, focusActive := state()
		assert.True(c, focusActive)
	}, 3*time.Second, 10*time.Millisecond)

	// Delete does nothing anymore
	press(tcell.KeyDelete)
	active, _ := state()
	assert.Equal(t, []*table.Column{all[1], all[0]}, active)
}
