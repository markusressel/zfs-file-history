package dialog

import (
	"testing"
	"zfs-file-history/internal/ui/table"

	"github.com/rivo/tview"
	"github.com/stretchr/testify/assert"
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
