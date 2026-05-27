package dialog

import (
	"slices"
	"zfs-file-history/internal/ui/table"
	"zfs-file-history/internal/ui/util"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

const (
	ColumnSelectionDialogPage util.Page = "ColumnSelectionDialog"
)

type ColumnSelectionDialog struct {
	application *tview.Application

	title string

	allColumns       []*table.Column
	activeColumns    []*table.Column
	availableColumns []*table.Column

	onChange func(activeColumns []*table.Column)

	layout         *tview.Flex
	actionChannel  chan DialogActionId
	activeTable    *tview.Table
	availableTable *tview.Table
	focusActive    bool
}

func NewColumnSelectionDialog(
	application *tview.Application,
	title string,
	allColumns []*table.Column,
	activeColumns []*table.Column,
	onChange func(activeColumns []*table.Column),
) *ColumnSelectionDialog {
	d := &ColumnSelectionDialog{
		application:      application,
		title:            title,
		allColumns:       slices.Clone(allColumns),
		activeColumns:    slices.Clone(activeColumns),
		actionChannel:    make(chan DialogActionId),
		onChange:         onChange,
		availableColumns: computeAvailableColumns(allColumns, activeColumns),
		focusActive:      true,
	}
	d.createLayout()
	return d
}

func (d *ColumnSelectionDialog) createLayout() {
	d.activeTable = tview.NewTable().SetSelectable(true, false)
	d.activeTable.SetBorder(true)
	d.activeTable.SetTitle(" Active ")
	d.activeTable.SetTitleAlign(tview.AlignLeft)

	d.availableTable = tview.NewTable().SetSelectable(true, false)
	d.availableTable.SetBorder(true)
	d.availableTable.SetTitle(" Available ")
	d.availableTable.SetTitleAlign(tview.AlignLeft)

	d.refreshTables()

	help := tview.NewTextView().SetText("Left/Right: switch | Enter: add | Del: remove | Shift+Up/Down: reorder | Esc: close")
	columns := tview.NewFlex().SetDirection(tview.FlexColumn)
	columns.AddItem(d.activeTable, 0, 1, true)
	columns.AddItem(d.availableTable, 0, 1, false)

	content := tview.NewFlex().SetDirection(tview.FlexRow)
	content.AddItem(help, 1, 0, false)
	content.AddItem(columns, 0, 1, true)

	d.layout = createModal(d.title, content, 70, 20)
	d.layout.SetInputCapture(d.captureInput)
	d.application.SetFocus(d.activeTable)
}

func (d *ColumnSelectionDialog) GetName() string {
	return string(ColumnSelectionDialogPage)
}

func (d *ColumnSelectionDialog) GetLayout() *tview.Flex {
	return d.layout
}

func (d *ColumnSelectionDialog) GetActionChannel() <-chan DialogActionId {
	return d.actionChannel
}

func (d *ColumnSelectionDialog) Close() {
	emitDialogActions(d.actionChannel, DialogCloseActionId)
}

func (d *ColumnSelectionDialog) refreshTables() {
	renderColumnTable(d.activeTable, d.activeColumns)
	renderColumnTable(d.availableTable, d.availableColumns)
	if d.focusActive {
		d.application.SetFocus(d.activeTable)
	} else {
		d.application.SetFocus(d.availableTable)
	}
}

func renderColumnTable(tableView *tview.Table, columns []*table.Column) {
	tableView.Clear()
	for row, column := range columns {
		tableView.SetCell(row, 0, tview.NewTableCell(column.Title).SetAlign(tview.AlignLeft))
	}
	if len(columns) > 0 {
		tableView.Select(0, 0)
	}
}

func (d *ColumnSelectionDialog) captureInput(event *tcell.EventKey) *tcell.EventKey {
	if d.focusActive && event.Modifiers()&tcell.ModShift != 0 {
		switch event.Key() {
		case tcell.KeyUp:
			d.moveActiveColumnUp()
			return nil
		case tcell.KeyDown:
			d.moveActiveColumnDown()
			return nil
		default:
		}
	}

	switch event.Key() {
	case tcell.KeyEscape:
		d.Close()
		return nil
	case tcell.KeyLeft:
		d.focusActive = true
		d.application.SetFocus(d.activeTable)
		return nil
	case tcell.KeyRight:
		d.focusActive = false
		d.application.SetFocus(d.availableTable)
		return nil
	case tcell.KeyEnter:
		if !d.focusActive {
			d.addSelectedAvailableColumn()
			return nil
		}
	case tcell.KeyDelete, tcell.KeyBackspace, tcell.KeyBackspace2:
		if d.focusActive {
			d.removeSelectedActiveColumn()
			return nil
		}
	}
	return event
}

func (d *ColumnSelectionDialog) addSelectedAvailableColumn() {
	if len(d.availableColumns) <= 0 {
		return
	}
	row, _ := d.availableTable.GetSelection()
	if row < 0 || row >= len(d.availableColumns) {
		return
	}
	selected := d.availableColumns[row]
	d.activeColumns = append(d.activeColumns, selected)
	d.availableColumns = computeAvailableColumns(d.allColumns, d.activeColumns)
	d.refreshTables()
	if len(d.availableColumns) > 0 {
		d.availableTable.Select(min(row, len(d.availableColumns)-1), 0)
	}
	d.emitChange()
}

func (d *ColumnSelectionDialog) removeSelectedActiveColumn() {
	if len(d.activeColumns) <= 1 {
		return
	}
	row, _ := d.activeTable.GetSelection()
	if row < 0 || row >= len(d.activeColumns) {
		return
	}
	d.activeColumns = slices.Delete(d.activeColumns, row, row+1)
	d.availableColumns = computeAvailableColumns(d.allColumns, d.activeColumns)
	d.refreshTables()
	d.activeTable.Select(min(row, len(d.activeColumns)-1), 0)
	d.emitChange()
}

func (d *ColumnSelectionDialog) moveActiveColumnUp() {
	if len(d.activeColumns) <= 1 {
		return
	}
	row, _ := d.activeTable.GetSelection()
	if row <= 0 || row >= len(d.activeColumns) {
		return
	}

	d.activeColumns[row-1], d.activeColumns[row] = d.activeColumns[row], d.activeColumns[row-1]
	d.refreshTables()
	d.activeTable.Select(row-1, 0)
	d.emitChange()
}

func (d *ColumnSelectionDialog) moveActiveColumnDown() {
	if len(d.activeColumns) <= 1 {
		return
	}
	row, _ := d.activeTable.GetSelection()
	if row < 0 || row >= len(d.activeColumns)-1 {
		return
	}

	d.activeColumns[row+1], d.activeColumns[row] = d.activeColumns[row], d.activeColumns[row+1]
	d.refreshTables()
	d.activeTable.Select(row+1, 0)
	d.emitChange()
}

func (d *ColumnSelectionDialog) emitChange() {
	if d.onChange != nil {
		d.onChange(slices.Clone(d.activeColumns))
	}
}

func computeAvailableColumns(allColumns []*table.Column, activeColumns []*table.Column) []*table.Column {
	return slices.DeleteFunc(slices.Clone(allColumns), func(c *table.Column) bool {
		return slices.ContainsFunc(activeColumns, func(active *table.Column) bool {
			return active != nil && c != nil && active.Id == c.Id
		})
	})
}
