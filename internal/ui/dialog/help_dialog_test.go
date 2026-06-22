package dialog

import (
	"testing"
	"zfs-file-history/internal/ui/theme"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"github.com/stretchr/testify/assert"
)

func TestSetHelpTableRow_WithEntry_FormatsKeyCellAndValueCell(t *testing.T) {
	t.Parallel()

	table := tview.NewTable()
	entry := &TableEntry{Key: "F1", Value: "Help"}

	setHelpTableRow(table, 0, entry)

	keyCell := table.GetCell(0, 0)
	valueCell := table.GetCell(0, 1)
	keyFg, _, _ := keyCell.Style.Decompose()
	valueFg, _, _ := valueCell.Style.Decompose()

	assert.Equal(t, "F1:", keyCell.Text)
	assert.Equal(t, tview.AlignRight, keyCell.Align)
	assert.Equal(t, theme.Colors.Layout.Table.Header, keyFg)

	assert.Equal(t, "Help", valueCell.Text)
	assert.Equal(t, tview.AlignLeft, valueCell.Align)
	assert.Equal(t, tcell.ColorWhite, valueFg)
}

func TestSetHelpTableRow_EmptyEntry_LeavesKeyEmpty(t *testing.T) {
	t.Parallel()

	table := tview.NewTable()

	setHelpTableRow(table, 0, emptyEntry)

	keyCell := table.GetCell(0, 0)
	valueCell := table.GetCell(0, 1)
	keyFg, _, _ := keyCell.Style.Decompose()

	assert.Equal(t, "", keyCell.Text)
	assert.Equal(t, tcell.ColorWhite, keyFg)
	assert.Equal(t, "", valueCell.Text)
}

func TestNewHelpPage(t *testing.T) {
	p := NewHelpPage()
	assert.NotNil(t, p.GetLayout())
}
