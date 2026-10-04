package dialog

import (
	"slices"
	"unicode/utf8"
	"zfs-file-history/internal/ui/shortcut_helper"
	"zfs-file-history/internal/ui/table"
	"zfs-file-history/internal/ui/util"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

const (
	ColumnSelectionDialogPage util.Page = "ColumnSelectionDialog"

	// columnSelectionShortcutLines is the height of the shortcut map
	columnSelectionShortcutLines = 2
	// columnSelectionMinContentWidth is the minimum content width, so all shortcuts fit into
	// columnSelectionShortcutLines (see TestColumnSelectionDialog_ShortcutsFit)
	columnSelectionMinContentWidth = 58
)

type ColumnSelectionDialog struct {
	application *tview.Application

	title string

	allColumns       []*table.Column
	activeColumns    []*table.Column
	availableColumns []*table.Column

	onChange func(activeColumns []*table.Column)
	// onReset restores the default columns and returns them, nil if resetting is not available
	onReset func() []*table.Column

	layout         *tview.Flex
	actionChannel  chan DialogActionId
	activeTable    *tview.Table
	availableTable *tview.Table
	shortcutMap    *shortcut_helper.ShortcutMapComponent
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

// NewTableColumnSelectionDialog creates a ColumnSelectionDialog that applies the changes to tableContainer directly.
// If the layout of tableContainer is saved (see table.RowSelectionTable.BindColumnLayout), it can be reset.
func NewTableColumnSelectionDialog[T table.RowSelectionTableEntry](
	application *tview.Application,
	title string,
	allColumns []*table.Column,
	tableContainer *table.RowSelectionTable[T],
) *ColumnSelectionDialog {
	d := NewColumnSelectionDialog(application, title, allColumns, tableContainer.GetColumnSpec(), tableContainer.SetActiveColumns)
	if tableContainer.CanResetColumnLayout() {
		d.SetResetFunc(func() []*table.Column {
			tableContainer.ResetColumnLayout()
			return tableContainer.GetColumnSpec()
		})
	}
	return d
}

// SetResetFunc enables resetting the columns (key "r"): f restores the default columns and returns them.
func (d *ColumnSelectionDialog) SetResetFunc(f func() []*table.Column) *ColumnSelectionDialog {
	d.onReset = f
	d.updateShortcutMap()
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

	d.shortcutMap = shortcut_helper.NewShortcutMap(d.application)
	d.updateShortcutMap()
	columns := tview.NewFlex().SetDirection(tview.FlexColumn)
	columns.AddItem(d.activeTable, 0, 1, true)
	columns.AddItem(d.availableTable, 0, 1, false)

	content := tview.NewFlex().SetDirection(tview.FlexRow)
	content.AddItem(d.shortcutMap.GetLayout(), columnSelectionShortcutLines, 0, false)
	content.AddItem(columns, 0, 1, true)
	content.SetMouseCapture(func(action tview.MouseAction, event *tcell.EventMouse) (tview.MouseAction, *tcell.EventMouse) {
		// mouse captures get all events, not only those on the primitive (https://github.com/rivo/tview/issues/926)
		if action == tview.MouseLeftDown {
			x, y := event.Position()
			if d.activeTable.InRect(x, y) {
				d.focusActive = true
				d.updateShortcutMap()
			} else if d.availableTable.InRect(x, y) {
				d.focusActive = false
				d.updateShortcutMap()
			}
		}
		return action, event
	})

	maxColWidth := 0
	for _, col := range d.allColumns {
		if col != nil {
			if l := utf8.RuneCountInString(col.Title); l > maxColWidth {
				maxColWidth = l
			}
		}
	}

	extraWidth := max(2*(maxColWidth+4), columnSelectionMinContentWidth)
	staticHeight := columnSelectionShortcutLines + len(d.allColumns) + 2

	d.layout = createModal(d.title, content, DialogSizeConstraints{
		Title:             d.title,
		ExtraContentWidth: extraWidth,
		StaticHeight:      staticHeight,
	})
	d.layout.SetInputCapture(d.captureInput)
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
	d.updateShortcutMap()
	if d.layout != nil {
		if d.focusActive {
			d.application.SetFocus(d.activeTable)
		} else {
			d.application.SetFocus(d.availableTable)
		}
	}
}

func (d *ColumnSelectionDialog) updateShortcutMap() {
	if d.shortcutMap == nil {
		return
	}

	entries := []shortcut_helper.ShortcutEntry{
		{KeyCombo: []string{"←", "→"}, Name: "Switch Side", Group: shortcut_helper.GroupNavigation},
		{KeyCombo: []string{shortcut_helper.KeyEsc}, Name: "Close", Group: shortcut_helper.GroupGlobal},
	}

	if d.focusActive {
		entries = append(entries,
			shortcut_helper.ShortcutEntry{KeyCombo: []string{shortcut_helper.KeyDelete}, Name: "Deactivate"},
			shortcut_helper.ShortcutEntry{KeyCombo: []string{shortcut_helper.Shift("↑"), shortcut_helper.Shift("↓")}, Name: "Reorder"},
		)
	} else {
		entries = append(entries,
			shortcut_helper.ShortcutEntry{KeyCombo: []string{shortcut_helper.KeyEnter}, Name: "Activate"},
		)
	}

	if d.onReset != nil {
		entries = append(entries, shortcut_helper.ShortcutEntry{KeyCombo: []string{"r"}, Name: "Reset"})
	}

	d.shortcutMap.SetEntries(entries)
}

// reset restores the default columns. Must only be called if onReset is set.
func (d *ColumnSelectionDialog) reset() {
	d.activeColumns = slices.Clone(d.onReset())
	d.availableColumns = computeAvailableColumns(d.allColumns, d.activeColumns)
	d.refreshTables()
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

	if d.onReset != nil && event.Key() == tcell.KeyRune && event.Rune() == 'r' && event.Modifiers() == tcell.ModNone {
		d.reset()
		return nil
	}

	switch event.Key() {
	case tcell.KeyEscape:
		d.Close()
		return nil
	case tcell.KeyLeft:
		d.focusActive = true
		d.updateShortcutMap()
		d.application.SetFocus(d.activeTable)
		return nil
	case tcell.KeyRight:
		d.focusActive = false
		d.updateShortcutMap()
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
