package util

import (
	"zfs-file-history/internal/ui/theme"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// SetSelectionStyle gives the selected row of the table the colors of the selection of all tables (see
// theme.SelectedStyle), for the cells without a selected style of their own (see KeepColorWhenSelected).
func SetSelectionStyle(table *tview.Table) *tview.Table {
	return table.SetSelectedStyle(tcell.StyleDefault.
		Foreground(theme.Colors.Layout.Table.SelectedForeground).
		Background(theme.Colors.Layout.Table.SelectedBackground))
}

// KeepColorWhenSelected keeps the text color of the cell while its row is selected, on the background of the
// selection, so colors that mean something (e.g. a destructive action, a state) stay visible. Without, tview draws
// all cells of the selected row in the selected style of the table.
func KeepColorWhenSelected(cell *tview.TableCell) *tview.TableCell {
	return cell.SetSelectedStyle(theme.SelectedStyle(TextColor(cell)))
}

// TextColor returns the color of the text of the cell: in its style if it has one (tview.NewTableCell gives it one,
// and SetTextColor then sets the color there), otherwise in Color.
func TextColor(cell *tview.TableCell) tcell.Color {
	if cell.Style != tcell.StyleDefault {
		color, _, _ := cell.Style.Decompose()
		return color
	}
	return cell.Color
}
