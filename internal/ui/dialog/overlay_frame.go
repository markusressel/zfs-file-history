package dialog

import (
	"os"
	"zfs-file-history/internal/ui/table"
	uiutil "zfs-file-history/internal/ui/util"

	"github.com/rivo/tview"
	"golang.org/x/term"
)

// NewOverlayFrame puts content into a frame that fills nearly the whole screen, for the history overlays.
// The frame clears its inside (see clearInside) and follows terminal resizes.
func NewOverlayFrame(title string, content tview.Primitive) *tview.Flex {
	termWidth, termHeight, err := term.GetSize(int(os.Stdout.Fd()))
	if err != nil || termWidth <= 0 || termHeight <= 0 {
		termWidth = 100
		termHeight = 30
	}
	width := max(termWidth-4, 80)
	height := max(termHeight-2, 15)

	dialogFrame := tview.NewFlex()
	dialogFrame.SetBorder(true)
	uiutil.SetupDialogWindow(dialogFrame, title)
	clearInside(dialogFrame.Box)
	dialogFrame.AddItem(content, 0, 1, true)

	dialogContentColumnWrapper := tview.NewFlex()
	dialogContentColumnWrapper.AddItem(nil, 0, 1, false)

	dialogContentRowWrapper := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(nil, 0, 1, false).
		AddItem(dialogFrame, height, 1, true).
		AddItem(nil, 0, 1, false)

	dialogContentColumnWrapper.
		AddItem(dialogContentRowWrapper, width, 1, true).
		AddItem(nil, 0, 1, false)

	MakeFlexResizing(dialogContentColumnWrapper, dialogContentRowWrapper, dialogFrame, 99999, 80, 99999, 15)
	return dialogContentColumnWrapper
}

// ShowColumnDialogOnPages lets the user select and order the columns of a table (F2), shown on the given pages.
// Must be called on the UI thread.
func ShowColumnDialogOnPages[T table.RowSelectionTableEntry](application *tview.Application, pages *tview.Pages, title string,
	allColumns []*table.Column, tableContainer *table.RowSelectionTable[T]) {
	if pages == nil {
		return
	}
	ShowDialogOnPages(application, pages, NewTableColumnSelectionDialog(application, title, allColumns, tableContainer), nil)
}
