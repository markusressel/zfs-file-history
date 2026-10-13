package dialog

import (
	"cmp"
	"fmt"
	"slices"
	"strings"
	"time"
	"zfs-file-history/internal/ui/theme"
	"zfs-file-history/internal/ui/util"
	"zfs-file-history/internal/zfs"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

const (
	DataErrorsDialogPage util.Page = "DataErrorsDialog"
)

// dataErrorsHelp explains where the damaged files come from and how to get rid of them.
const dataErrorsHelp = "ZFS could not read these files from any copy. Restore them from a backup or an older " +
	"snapshot, or delete them. A snapshot keeps the damaged blocks of its files until it is destroyed. " +
	"The list is updated by the next scrubs (zpool clear, then scrub twice)."

// dataErrorsWidth is the width of the content of the dialog, see DialogSizeConstraints.ExtraContentWidth.
const dataErrorsWidth = 100

// DataErrorsDialog lists the damaged files of all pools (see zfs.ListDataErrors): the ones in the file system first,
// then the ones in snapshots, then the objects whose path ZFS does not know. Enter shows a file in the file browser,
// f looks for the objects without a path in the snapshots.
type DataErrorsDialog struct {
	application   *tview.Application
	layout        *tview.Flex
	actionChannel chan DialogActionId
	content       *tview.Flex
	helpView      *tview.TextView
	table         *tview.Table
	// searchView shows the state of the search for the objects, see SetObjectSearch
	searchView *tview.TextView
	// onShow is called with the file to show, see NewDataErrorsDialog
	onShow func(dataError zfs.DataError)
	// onFind starts the search for the objects without a path, nil: not offered
	onFind func()
	// onClose is called when the dialog is closed, e.g. to stop the search, nil: nothing
	onClose func()

	// All fields below are only accessed on the UI thread.

	// index are the damaged files shown, see SetIndex
	index *zfs.DataErrorIndex
}

// NewDataErrorsDialog shows the damaged files of the index. onShow is called on the UI thread with the file to show
// in the file browser, after the dialog was closed. onFind is called on the UI thread to look for the objects
// without a path in the snapshots (f), nil if it is not offered. onClose is called on the UI thread when the dialog
// is closed (nil: nothing), e.g. to stop the search, which reads whole snapshots. Must be called on the UI thread.
func NewDataErrorsDialog(application *tview.Application, index *zfs.DataErrorIndex, onShow func(dataError zfs.DataError), onFind func(), onClose func()) *DataErrorsDialog {
	dialog := &DataErrorsDialog{
		application:   application,
		actionChannel: make(chan DialogActionId),
		onShow:        onShow,
		onFind:        onFind,
		onClose:       onClose,
	}
	dialog.createLayout(index)
	return dialog
}

func (d *DataErrorsDialog) createLayout(index *zfs.DataErrorIndex) {
	dialogTitle := " ⚠ Damaged files "

	d.helpView = tview.NewTextView().SetWrap(true).SetWordWrap(true).SetDynamicColors(true)
	d.helpView.SetTextColor(theme.Colors.Alert.Hint)

	d.table = tview.NewTable().SetSelectable(true, false).SetFixed(1, 0)
	d.table.SetSelectedStyle(tcell.StyleDefault.Foreground(theme.Colors.Layout.Table.SelectedForeground).Background(theme.Colors.Layout.Table.SelectedBackground))
	d.table.SetSelectedFunc(func(row int, column int) {
		d.showRow(row)
	})
	util.IgnoreClicksBelowRows(d.table)

	d.searchView = tview.NewTextView().SetDynamicColors(true).SetWrap(true).SetWordWrap(true)
	d.searchView.SetTextColor(theme.Colors.Alert.Hint)

	hint := "Enter: show the file, esc: close"
	if d.onFind != nil {
		hint = "Enter: show the file, f: find the objects in the snapshots, esc: close"
	}
	closeTextView := util.CreateAttentionTextView(hint)

	d.content = tview.NewFlex().SetDirection(tview.FlexRow)
	d.content.AddItem(d.helpView, 1, 0, false)
	d.content.AddItem(d.table, 0, 1, true)
	d.content.AddItem(d.searchView, 0, 0, false)
	d.content.AddItem(closeTextView, 1, 0, false)
	// the explanation gets as many lines as it needs at the width of the dialog
	d.content.SetDrawFunc(func(screen tcell.Screen, x int, y int, width int, height int) (int, int, int, int) {
		d.content.ResizeItem(d.helpView, wrappedHeight(d.helpView.GetText(false), width-2)+1, 0)
		searchHeight := 0
		if text := d.searchView.GetText(false); text != "" {
			searchHeight = wrappedHeight(text, width-2)
		}
		d.content.ResizeItem(d.searchView, searchHeight, 0)
		return x + 1, y, width - 2, height
	})

	d.SetIndex(index)
	rows := min(d.table.GetRowCount(), 21)
	dialog := createModal(dialogTitle, d.content, DialogSizeConstraints{
		Title:             dialogTitle,
		ExtraContentWidth: dataErrorsWidth,
		StaticHeight:      rows + wrappedHeight(formatDataErrorsHelp(index), dataErrorsWidth-2) + 6,
	})
	dialog.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		switch {
		case event.Key() == tcell.KeyEscape || (event.Key() == tcell.KeyRune && event.Rune() == 'q'):
			d.Close()
			return nil
		case event.Key() == tcell.KeyRune && event.Rune() == 'f' && d.onFind != nil:
			d.onFind()
			return nil
		}
		return event
	})
	d.layout = dialog
}

// SetIndex shows the damaged files of the index, e.g. after objects were found in snapshots. The selected row
// stays. Must be called on the UI thread.
func (d *DataErrorsDialog) SetIndex(index *zfs.DataErrorIndex) {
	d.index = index
	d.helpView.SetText(tview.Escape(formatDataErrorsHelp(index)))

	selected, _ := d.table.GetSelection()
	d.table.Clear()
	for column, title := range []string{"Pool", "In", "File"} {
		d.table.SetCell(0, column, tview.NewTableCell(title).SetTextColor(theme.Colors.Layout.Table.HeaderForeground).
			SetBackgroundColor(theme.Colors.Layout.Table.HeaderBackground).SetSelectable(false))
	}
	dataErrors := sortedDataErrors(index)
	for i, dataError := range dataErrors {
		where, whereColor := formatDataErrorLocation(dataError)
		d.table.SetCell(i+1, 0, tview.NewTableCell(tview.Escape(dataError.Pool)).SetTextColor(theme.Colors.Alert.Pool))
		d.table.SetCell(i+1, 1, tview.NewTableCell(tview.Escape(where)).SetTextColor(whereColor))
		d.table.SetCell(i+1, 2, tview.NewTableCell(tview.Escape(formatDataErrorFile(dataError))).SetTextColor(theme.Colors.Alert.Message).
			SetExpansion(1).SetReference(dataError))
	}
	if len(dataErrors) > 0 {
		d.table.Select(min(max(selected, 1), len(dataErrors)), 0)
	}
}

// SetObjectSearch shows the state of the search for the objects without a path in the snapshots. Must be called on
// the UI thread.
func (d *DataErrorsDialog) SetObjectSearch(progress zfs.ObjectSearchProgress, running bool, err error) {
	d.searchView.SetText(tview.Escape(formatObjectSearch(progress, running, err)))
}

// formatObjectSearch returns the state of the search for the objects without a path, e.g. "Searching @daily (3 of
// 31 snapshots, 120000 entries): found 2 of 13 objects", and what it means for the ones that were not found.
func formatObjectSearch(progress zfs.ObjectSearchProgress, running bool, err error) string {
	found := fmt.Sprintf("found %d of %d %s", progress.Found, progress.Wanted, util.Plural(progress.Wanted, "object", "objects"))
	switch {
	case err != nil:
		return "The search failed: " + err.Error()
	case running && progress.Snapshot != "":
		return fmt.Sprintf("Searching @%s (%d of %d snapshots, %d entries): %s", progress.Snapshot, progress.Done+1,
			progress.Total, progress.Entries, found)
	case running:
		return "Searching the snapshots…"
	}
	text := fmt.Sprintf("Searched %d %s: %s.", progress.Total, util.Plural(progress.Total, "snapshot", "snapshots"), found)
	if progress.Found < progress.Wanted {
		// in no snapshot, and not in the file system (else ZFS would know the path); folders that cannot be read are
		// skipped though
		text += " The others are probably deleted everywhere (or in folders you cannot read): then their blocks are " +
			"freed, and the errors go away with the next scrubs."
	}
	return text
}

// wrappedHeight returns the number of lines of the text wrapped at the given width.
func wrappedHeight(text string, width int) int {
	lines := 0
	for _, line := range strings.Split(text, "\n") {
		lines += max(len(tview.WordWrap(line, width)), 1)
	}
	return lines
}

// showRow closes the dialog and shows the file of the row, if it has one in the file system (or was found in a
// snapshot). Runs on the UI thread.
func (d *DataErrorsDialog) showRow(row int) {
	dataError, ok := d.table.GetCell(row, 2).GetReference().(zfs.DataError)
	if !ok || d.onShow == nil {
		return
	}
	if dataError.LivePath == "" && len(dataError.Locations) > 0 {
		dataError.LivePath = dataError.Locations[0].LivePath
	}
	if dataError.LivePath == "" {
		return
	}
	d.Close()
	go func() {
		// the dialog is closed first, so the file browser gets the focus back before it shows the file
		time.Sleep(10 * time.Millisecond)
		d.application.QueueUpdateDraw(func() { d.onShow(dataError) })
	}()
}

// formatDataErrorsHelp returns the explanation above the list, with the pools whose list needs root, and the ones
// that do not report the errors for every snapshot.
func formatDataErrorsHelp(index *zfs.DataErrorIndex) string {
	text := dataErrorsHelp
	if index == nil {
		return text
	}
	if len(index.Unavailable) > 0 {
		text = fmt.Sprintf("The damaged files of %s can only be listed by root. %s\n%s",
			strings.Join(index.Unavailable, ", "), zfs.SetupDataErrorsHint, text)
	}
	for _, pool := range index.WithoutHeadErrlog {
		text += fmt.Sprintf("\n%s reports an error only for the dataset or snapshot whose read failed, not for every "+
			"snapshot that shares the damaged block: its feature head_errlog is disabled. To enable it (cannot be undone, "+
			"the pool then needs OpenZFS 2.2 or newer, also in the initramfs and boot loader): %s",
			pool, zfs.EnableHeadErrlogCommand(pool))
	}
	return text
}

// formatDataErrorFile returns the file of a damaged object: its path in the file system (or in the dataset, if not
// mounted), the path it was found at in snapshots, or the entry as printed by zpool if the path is not known.
func formatDataErrorFile(dataError zfs.DataError) string {
	switch {
	case dataError.LivePath != "":
		return dataError.LivePath
	case dataError.Path != "":
		return dataError.Path
	case len(dataError.Locations) > 0:
		return dataError.Locations[0].LivePath + "  (" + dataError.Entry + ", by inode)"
	}
	return dataError.Entry
}

// formatDataErrorLocation returns where the damaged object is, and its color: "file system" (the error color),
// the name of the snapshot (the warning color; the newest one it was found in by its inode, with the number of
// others), "not mounted" if its dataset is not mounted, "no snapshot" if it was looked for in vain, or "unknown" if
// ZFS does not know its path (e.g. metadata, or a deleted file).
func formatDataErrorLocation(dataError zfs.DataError) (string, tcell.Color) {
	colors := theme.Colors.Alert
	switch {
	case len(dataError.Locations) > 1:
		return fmt.Sprintf("@%s +%d", dataError.Locations[0].Snapshot, len(dataError.Locations)-1), colors.Warning
	case len(dataError.Locations) == 1:
		return "@" + dataError.Locations[0].Snapshot, colors.Warning
	case dataError.Searched:
		return "no snapshot", colors.Unknown
	case !dataError.IsResolved() && dataError.LivePath == "":
		return "unknown", colors.Unknown
	case dataError.Snapshot != "":
		return "@" + dataError.Snapshot, colors.Warning
	case dataError.LivePath == "":
		return "not mounted", colors.Unknown
	}
	return "file system", colors.Error
}

// sortedDataErrors returns the errors of the index in the order they are listed: the files in the file system first,
// then the ones in snapshots, then the ones whose path is not known, each by pool and path.
func sortedDataErrors(index *zfs.DataErrorIndex) []zfs.DataError {
	if index == nil {
		return nil
	}
	dataErrors := slices.Clone(index.Errors)
	rank := func(dataError zfs.DataError) int {
		switch {
		case dataError.Snapshot == "" && dataError.LivePath != "":
			return 0
		case dataError.IsResolved() || len(dataError.Locations) > 0:
			return 1
		}
		return 2
	}
	slices.SortStableFunc(dataErrors, func(a, b zfs.DataError) int {
		return cmp.Or(
			cmp.Compare(rank(a), rank(b)),
			cmp.Compare(a.Pool, b.Pool),
			cmp.Compare(a.LivePath, b.LivePath),
			cmp.Compare(a.Entry, b.Entry),
		)
	})
	return dataErrors
}

func (d *DataErrorsDialog) GetName() string {
	return string(DataErrorsDialogPage)
}

func (d *DataErrorsDialog) GetLayout() *tview.Flex {
	return d.layout
}

func (d *DataErrorsDialog) GetActionChannel() <-chan DialogActionId {
	return d.actionChannel
}

// Close closes the dialog. Must be called on the UI thread.
func (d *DataErrorsDialog) Close() {
	if d.onClose != nil {
		d.onClose()
	}
	go func() {
		d.actionChannel <- DialogCloseActionId
	}()
}
