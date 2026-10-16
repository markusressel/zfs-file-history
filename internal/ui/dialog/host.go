package dialog

import (
	"zfs-file-history/internal/ui/table"

	"github.com/rivo/tview"
)

// Host shows the dialogs of a component. Components embed it, so they share how their dialogs are shown: on the
// pages of the application once the page sets them (SetDialogPages), so they are modal for the whole screen, and on
// the own pages of the component until then.
//
// Only accessed on the UI thread.
type Host struct {
	application *tview.Application
	// own are the pages of the component, which show its dialogs until SetDialogPages was called, nil: none
	own *tview.Pages
	// dialogPages are the pages dialogs are shown on, see SetDialogPages
	dialogPages *tview.Pages
}

// NewHost returns the host of the dialogs of a component, which shows them on its own pages (nil: not at all) until
// SetDialogPages is called.
func NewHost(application *tview.Application, own *tview.Pages) Host {
	return Host{application: application, own: own}
}

// SetDialogPages sets the pages its dialogs are shown on: the pages of the application, so they are modal for the
// whole screen, not only the area of this component. Without them, they are shown on its own layout.
func (host *Host) SetDialogPages(pages *tview.Pages) {
	host.dialogPages = pages
}

// ShowDialog shows the dialog on the dialog pages (see SetDialogPages), and calls onClosed (if not nil) when it is
// closed. Does nothing if there are no pages to show it on.
func (host *Host) ShowDialog(d Dialog, onClosed func()) {
	if pages := host.pages(); pages != nil {
		ShowDialogOnPages(host.application, pages, d, onClosed)
	}
}

// pages returns the pages dialogs are shown on now, nil: none.
func (host *Host) pages() *tview.Pages {
	if host.dialogPages != nil {
		return host.dialogPages
	}
	return host.own
}

// ShowColumnSelection lets the user select and order the displayed columns of a table of the component (F2).
func ShowColumnSelection[T table.RowSelectionTableEntry](host *Host, title string, allColumns []*table.Column, tableContainer *table.RowSelectionTable[T]) {
	ShowColumnDialogOnPages(host.application, host.pages(), title, allColumns, tableContainer)
}
