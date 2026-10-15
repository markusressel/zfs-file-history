package dialog

import (
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"
	"zfs-file-history/internal/ui/theme"
	"zfs-file-history/internal/ui/txwidgets"
	"zfs-file-history/internal/ui/util"
	"zfs-file-history/internal/zfs"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

const (
	PoolHistoryDialogPage util.Page = "PoolHistoryDialog"
)

// poolHistoryTimeLayout is the time of a record in the list.
const poolHistoryTimeLayout = "2006-01-02 15:04:05"

// poolHistoryDetailsHeight is the number of lines of the record shown in full below the list.
const poolHistoryDetailsHeight = 3

// poolHistoryColumns are the titles of the columns of the list.
var poolHistoryColumns = []string{"Time", "Event", "Dataset", "Details", "User"}

// PoolHistoryDialog lists the history of a pool, or of a dataset (with its children and snapshots), newest first
// (see zfs.PoolHistory): the commands that changed it, and with i the events ZFS logged itself (e.g. each snapshot of
// a recursive command, the end of a scrub). / filters the records, the selected one is shown in full below the list.
// A pool may have hundreds of thousands of records, so the list is a virtual table (see poolHistoryContent).
type PoolHistoryDialog struct {
	application   *tview.Application
	layout        *tview.Flex
	actionChannel chan DialogActionId
	filterInput   *tview.InputField
	table         *tview.Table
	detailsView   *tview.TextView
	// hintView shows the keys and the number of records, see updateHint
	hintView *tview.TextView

	// All fields below are only accessed on the UI thread.

	// pool and dataset are what the history is shown of, dataset "": the whole pool
	pool, dataset string
	// content are the records shown
	content *poolHistoryContent
	// all are the records of the pool or dataset, newest first; nil while loading
	all []zfs.PoolHistoryEntry
	// err is why the history could not be read, nil if it was
	err error
	// internal is whether the events ZFS logged itself are shown
	internal bool
}

// NewPoolHistoryDialog shows the history of the pool, or of the dataset (with its children and snapshots) if dataset
// is not "", read by load in the background (see zfs.PoolHistory, which reads all pools). The events ZFS logged itself
// are shown for a dataset from the start: a recursive command names only the dataset it was given, the events name
// each dataset it changed. Must be called on the UI thread.
func NewPoolHistoryDialog(application *tview.Application, pool string, dataset string, load func() ([]zfs.PoolHistoryEntry, error)) *PoolHistoryDialog {
	d := &PoolHistoryDialog{
		application:   application,
		actionChannel: make(chan DialogActionId),
		pool:          pool,
		dataset:       dataset,
		internal:      dataset != "",
	}
	d.content = &poolHistoryContent{}
	d.createLayout()
	go func() {
		entries, err := load()
		var own []zfs.PoolHistoryEntry
		for i := len(entries) - 1; i >= 0; i-- {
			if entry := &entries[i]; entry.Pool == pool && (dataset == "" || entry.IsAbout(dataset)) {
				own = append(own, *entry)
			}
		}
		application.QueueUpdateDraw(func() { d.setEntries(own, err) })
	}()
	return d
}

func (d *PoolHistoryDialog) createLayout() {
	name := d.pool
	if d.dataset != "" {
		name = d.dataset
	}
	title := fmt.Sprintf(" History of %s ", name)

	d.filterInput = tview.NewInputField().
		SetLabel("Filter: ").
		SetLabelColor(theme.Colors.Info.Label).
		SetFieldBackgroundColor(tview.Styles.PrimitiveBackgroundColor).
		SetChangedFunc(func(string) { d.applyFilter() })
	d.filterInput.SetDoneFunc(func(key tcell.Key) {
		if key == tcell.KeyEscape {
			d.filterInput.SetText("")
		}
		d.application.SetFocus(d.table)
	})

	d.table = tview.NewTable().SetSelectable(true, false).SetFixed(1, 0).SetContent(d.content)
	d.table.SetSelectedStyle(tcell.StyleDefault.Foreground(theme.Colors.Layout.Table.SelectedForeground).Background(theme.Colors.Layout.Table.SelectedBackground))
	d.table.SetSelectionChangedFunc(func(row int, column int) { d.updateDetails() })
	util.IgnoreClicksBelowRows(d.table)

	d.detailsView = tview.NewTextView().SetDynamicColors(true).SetWrap(true).SetWordWrap(true)
	d.hintView = util.CreateAttentionTextView("")

	content := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(d.filterInput, 1, 0, false).
		AddItem(d.table, 0, 1, true).
		AddItem(d.detailsView, poolHistoryDetailsHeight, 0, false).
		AddItem(d.hintView, 1, 0, false)
	content.SetDrawFunc(func(screen tcell.Screen, x int, y int, width int, height int) (int, int, int, int) {
		return x + 1, y, width - 2, height
	})

	layout := createOverlayFrame(title, content)
	layout.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if d.application.GetFocus() == d.filterInput {
			return event
		}
		switch {
		case event.Key() == tcell.KeyEscape || (event.Key() == tcell.KeyRune && event.Rune() == 'q'):
			d.Close()
			return nil
		case event.Key() == tcell.KeyRune && event.Rune() == '/', event.Key() == tcell.KeyCtrlF:
			d.application.SetFocus(d.filterInput)
			return nil
		case event.Key() == tcell.KeyRune && event.Rune() == 'i':
			d.internal = !d.internal
			d.applyFilter()
			return nil
		}
		return event
	})
	d.layout = layout
	d.updateHint()
}

// setEntries shows the records, newest first, or why they could not be read. Must be called on the UI thread.
func (d *PoolHistoryDialog) setEntries(entries []zfs.PoolHistoryEntry, err error) {
	d.all, d.err = entries, err
	if d.all == nil {
		d.all = []zfs.PoolHistoryEntry{}
	}
	d.applyFilter()
}

// applyFilter shows the records that match the filter (in the event, datasets, details or user, ignoring case), with
// or without the internal events. The selected record stays selected if it is still shown. Must be called on the UI
// thread.
func (d *PoolHistoryDialog) applyFilter() {
	if d.all == nil {
		return
	}
	selected := d.selectedEntry()
	filter := strings.TrimSpace(d.filterInput.GetText())
	d.content.entries = d.content.entries[:0]
	for i := range d.all {
		entry := &d.all[i]
		if (d.internal || !entry.Internal) && (filter == "" || poolHistoryEntryMatches(entry, filter)) {
			d.content.entries = append(d.content.entries, entry)
		}
	}
	row := 1
	if selected != nil {
		if index := slices.Index(d.content.entries, selected); index >= 0 {
			row = index + 1
		}
	}
	// drawn empty (while loading, or with a filter nothing matches), the table follows its end, but the newest
	// records are at the top; the selection is scrolled to again
	d.table.ScrollToBeginning()
	if len(d.content.entries) > 0 {
		d.table.Select(row, 0)
	}
	d.updateDetails()
	d.updateHint()
}

// poolHistoryEntryMatches returns whether the record contains the filter, ignoring case.
func poolHistoryEntryMatches(entry *zfs.PoolHistoryEntry, filter string) bool {
	if containsFold(entry.Event, filter) || containsFold(entry.Text, filter) || containsFold(entry.User, filter) {
		return true
	}
	return slices.ContainsFunc(entry.Datasets, func(dataset string) bool { return containsFold(dataset, filter) })
}

// containsFold returns whether text contains sub, ignoring case, without allocating: the filter runs over hundreds of
// thousands of records on each key. An ASCII sub (the usual case) is compared byte by byte, which is several times
// faster than strings.EqualFold at each position.
func containsFold(text string, sub string) bool {
	if sub == "" {
		return true
	}
	if !isASCII(sub) {
		for i := 0; i+len(sub) <= len(text); {
			if strings.EqualFold(text[i:i+len(sub)], sub) {
				return true
			}
			_, size := utf8.DecodeRuneInString(text[i:])
			i += size
		}
		return false
	}
	first := lowerASCII(sub[0])
	for i := 0; i+len(sub) <= len(text); i++ {
		if lowerASCII(text[i]) != first {
			continue
		}
		j := 1
		for j < len(sub) && lowerASCII(text[i+j]) == lowerASCII(sub[j]) {
			j++
		}
		if j == len(sub) {
			return true
		}
	}
	return false
}

// isASCII returns whether the text has only ASCII characters.
func isASCII(text string) bool {
	for i := 0; i < len(text); i++ {
		if text[i] >= utf8.RuneSelf {
			return false
		}
	}
	return true
}

// lowerASCII returns the lower case of an ASCII letter, other bytes as they are.
func lowerASCII(b byte) byte {
	if 'A' <= b && b <= 'Z' {
		return b + 'a' - 'A'
	}
	return b
}

// selectedEntry returns the selected record, nil if none.
func (d *PoolHistoryDialog) selectedEntry() *zfs.PoolHistoryEntry {
	row, _ := d.table.GetSelection()
	if row < 1 || row > len(d.content.entries) {
		return nil
	}
	return d.content.entries[row-1]
}

// updateDetails shows the selected record in full below the list. Must be called on the UI thread.
func (d *PoolHistoryDialog) updateDetails() {
	entry := d.selectedEntry()
	if entry == nil {
		d.detailsView.SetText("")
		return
	}
	who := entry.Host
	if entry.User != "" {
		who = entry.User + " on " + entry.Host
	}
	kind := "Command"
	text := entry.Text
	if entry.Internal {
		kind = fmt.Sprintf("Logged by ZFS (txg %d)", entry.Txg)
		text = strings.TrimSpace(strings.Join(append([]string{entry.Event}, append(slices.Clone(entry.Datasets), entry.Text)...), " "))
	}
	d.detailsView.SetText(
		txwidgets.Span(theme.Colors.Info.Label, "%s, %s, %s: ", entry.Time.Format(poolHistoryTimeLayout), kind, who) +
			txwidgets.Span(theme.Colors.Info.Value, "%s", text))
}

// updateHint shows the keys, and how many records are shown, or that they are loading or could not be read. Must
// be called on the UI thread.
func (d *PoolHistoryDialog) updateHint() {
	internalAction := "show"
	if d.internal {
		internalAction = "hide"
	}
	keys := fmt.Sprintf("/: filter · i: %s events logged by ZFS · Esc: close", internalAction)
	switch {
	case d.err != nil:
		d.hintView.SetText(tview.Escape(fmt.Sprintf("Could not read the history: %s", d.err)))
	case d.all == nil:
		d.hintView.SetText("Reading the history…")
	default:
		d.hintView.SetText(tview.Escape(fmt.Sprintf("%d of %d records · %s", len(d.content.entries), len(d.all), keys)))
	}
}

func (d *PoolHistoryDialog) GetName() string {
	return string(PoolHistoryDialogPage)
}

func (d *PoolHistoryDialog) GetLayout() *tview.Flex {
	return d.layout
}

func (d *PoolHistoryDialog) GetActionChannel() <-chan DialogActionId {
	return d.actionChannel
}

func (d *PoolHistoryDialog) Close() {
	go func() {
		d.actionChannel <- DialogCloseActionId
	}()
}

// poolHistoryContent are the rows of the list of records, created when drawn (see tview.TableContent): a table with
// cells for hundreds of thousands of records would take long to fill and much memory.
type poolHistoryContent struct {
	tview.TableContentReadOnly
	// entries are the records shown, newest first. Only accessed on the UI thread.
	entries []*zfs.PoolHistoryEntry
}

func (c *poolHistoryContent) GetRowCount() int {
	return len(c.entries) + 1
}

func (c *poolHistoryContent) GetColumnCount() int {
	return len(poolHistoryColumns)
}

func (c *poolHistoryContent) GetCell(row int, column int) *tview.TableCell {
	if column < 0 || column >= len(poolHistoryColumns) {
		return nil
	}
	if row == 0 {
		return tview.NewTableCell(poolHistoryColumns[column]).SetTextColor(theme.Colors.Layout.Table.HeaderForeground).
			SetBackgroundColor(theme.Colors.Layout.Table.HeaderBackground).SetSelectable(false)
	}
	if row > len(c.entries) {
		return nil
	}
	entry := c.entries[row-1]
	color := theme.Colors.Info.Value
	if entry.Internal {
		color = theme.Colors.Info.Muted
	}
	switch column {
	case 0:
		return tview.NewTableCell(entry.Time.Format(poolHistoryTimeLayout)).SetTextColor(theme.Colors.Info.Label)
	case 1:
		return tview.NewTableCell(tview.Escape(entry.Event)).SetTextColor(color)
	case 2:
		return tview.NewTableCell(tview.Escape(entry.Dataset())).SetTextColor(theme.Colors.Layout.Table.Accent).SetMaxWidth(50)
	case 3:
		return tview.NewTableCell(tview.Escape(poolHistoryDetails(entry))).SetTextColor(color).SetExpansion(1)
	default:
		return tview.NewTableCell(tview.Escape(entry.User)).SetTextColor(theme.Colors.Info.Label)
	}
}

// poolHistoryDetails returns what the record says besides its event and dataset: the arguments of a command (e.g.
// "-r tank/data@daily"), the text of an internal event (e.g. "errors=0").
func poolHistoryDetails(entry *zfs.PoolHistoryEntry) string {
	if entry.Internal {
		return entry.Text
	}
	return strings.TrimSpace(strings.TrimPrefix(entry.Text, entry.Event))
}
