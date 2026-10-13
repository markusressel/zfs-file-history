package dialog

import (
	"unicode/utf8"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// ChecklistItem is an entry of the checklist of a SelectionDialog, see NewSelectionDialogWithChecklist.
type ChecklistItem struct {
	Name string
	// Checked is changed by the user. Only accessed on the UI thread, see SelectionDialog.Checked.
	Checked bool
}

// formatChecklistItem returns the text of the row of the item.
func formatChecklistItem(item *ChecklistItem) string {
	if item.Checked {
		return "  ☑ " + item.Name
	}
	return "  ☐ " + item.Name
}

// checklistWidth returns the width of the widest row of the checklist.
func checklistWidth(items []*ChecklistItem) int {
	width := 0
	for _, item := range items {
		width = max(width, utf8.RuneCountInString(formatChecklistItem(item)))
	}
	return width
}

// createChecklistTable creates the table of the checklist: a click, Space or Enter checks or unchecks an item (see
// checklistInputCapture). Its selection is only shown while it has the focus, so only one row of the dialog is
// highlighted.
func (d *SelectionDialog) createChecklistTable() *tview.Table {
	checklistTable := tview.NewTable()
	checklistTable.SetSelectable(false, false)
	for row, item := range d.checklist {
		cell := tview.NewTableCell(formatChecklistItem(item)).SetExpansion(1)
		cell.SetClickedFunc(func() bool {
			d.toggleChecklistItem(row)
			return false
		})
		checklistTable.SetCell(row, 0, cell)
	}
	checklistTable.Select(0, 0)
	checklistTable.SetFocusFunc(func() { checklistTable.SetSelectable(true, false) })
	checklistTable.SetBlurFunc(func() { checklistTable.SetSelectable(false, false) })
	return checklistTable
}

// toggleChecklistItem checks or unchecks the item of the row, unless an action runs. Runs on the UI thread.
func (d *SelectionDialog) toggleChecklistItem(row int) {
	if d.stopLoading != nil || row < 0 || row >= len(d.checklist) {
		return
	}
	item := d.checklist[row]
	item.Checked = !item.Checked
	d.checklistTable.GetCell(row, 0).SetText(formatChecklistItem(item))
}

// checklistInputCapture moves between the checklist and the options (↑ from the first option, ↓ from the last item,
// Tab), and checks or unchecks the selected item (Space, Enter). A number selects an option, as without a
// checklist. The other keys are handled by optionCapture. Runs on the UI thread.
func (d *SelectionDialog) checklistInputCapture(optionCapture func(event *tcell.EventKey) *tcell.EventKey) func(event *tcell.EventKey) *tcell.EventKey {
	return func(event *tcell.EventKey) *tcell.EventKey {
		itemRow, _ := d.checklistTable.GetSelection()
		optionRow, _ := d.optionTable.GetSelection()
		switch {
		case event.Key() == tcell.KeyTab || event.Key() == tcell.KeyBacktab:
			if d.checklistTable.HasFocus() {
				d.application.SetFocus(d.optionTable)
			} else {
				d.application.SetFocus(d.checklistTable)
			}
			return nil
		case d.checklistTable.HasFocus() && (event.Key() == tcell.KeyEnter || (event.Key() == tcell.KeyRune && event.Rune() == ' ')):
			d.toggleChecklistItem(itemRow)
			return nil
		case d.checklistTable.HasFocus() && event.Key() == tcell.KeyDown && itemRow == len(d.checklist)-1:
			d.application.SetFocus(d.optionTable)
			d.optionTable.Select(0, 0)
			return nil
		case d.optionTable.HasFocus() && event.Key() == tcell.KeyUp && optionRow == 0:
			d.application.SetFocus(d.checklistTable)
			d.checklistTable.Select(len(d.checklist)-1, 0)
			return nil
		case event.Key() == tcell.KeyRune && event.Rune() >= '1' && event.Rune() <= '9':
			d.application.SetFocus(d.optionTable)
		}
		return optionCapture(event)
	}
}

// Checked returns whether each item of the checklist was checked when the running action was selected: unlike the
// items, it may be called by the handler in the background.
func (d *SelectionDialog) Checked() []bool {
	return d.checked
}
