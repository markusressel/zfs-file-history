package dialog

import (
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"
	"zfs-file-history/internal/ui/scrollbar"
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

// the pages of the area of the table: the table, or the loading view while the history is read
const (
	poolHistoryTablePage   = "table"
	poolHistoryLoadingPage = "loading"
)

// poolHistoryTimeLayout is the time of a record in the list.
const poolHistoryTimeLayout = "2006-01-02 15:04:05"

// poolHistoryDetailsHeight is the number of lines of the record shown in full below the list.
const poolHistoryDetailsHeight = 3

// poolHistoryColumns are the titles of the columns of the list. The details are last: they can be long (e.g. the
// devices of zpool create), and are cut off where the list ends, not the columns after them; the selected row is
// shown in full below the list.
var poolHistoryColumns = []string{"Time", "Event", "Dataset", "User", "Details"}

// PoolHistoryDialog lists the history of a pool, or of a dataset (with its children and snapshots), newest first
// (see zfs.PoolHistory). The records are grouped into what was done (see zfs.GroupPoolHistory: a scrub from its start
// to its end, a command with the events ZFS logged for it, the commands of a run of a tool), each group a row that is
// expanded to its records like the tree of the datasets (Enter, +, -, *). b lists the records one by one instead, in
// the order they were logged: the commands, and with i the events ZFS logged itself. / filters, the selected row is
// shown in full below the list. A pool may have hundreds of thousands of records, so the list is a virtual table (see
// poolHistoryContent).
type PoolHistoryDialog struct {
	application   *tview.Application
	layout        *tview.Flex
	actionChannel chan DialogActionId
	filterInput   *tview.InputField
	table         *poolHistoryTable
	// tableArea shows the table, or loadingView while the history is read
	tableArea   *tview.Pages
	loadingView *util.LoadingView
	detailsView *tview.TextView
	// hintView shows the keys and the number of records, see updateHint
	hintView *tview.TextView

	// All fields below are only accessed on the UI thread.

	// pool and dataset are what the history is shown of, dataset "": the whole pool
	pool, dataset string
	// content are the rows shown
	content *poolHistoryContent
	// all are the records of the pool or dataset, oldest first; nil while loading
	all []zfs.PoolHistoryEntry
	// groups are the records grouped (see zfs.GroupPoolHistory), oldest first, groupOf the group of each record
	groups  []*zfs.PoolHistoryGroup
	groupOf map[*zfs.PoolHistoryEntry]*zfs.PoolHistoryGroup
	// err is why the history could not be read, nil if it was
	err error
	// chronological is whether the records are listed one by one instead of grouped
	chronological bool
	// expanded are the groups whose records are shown below them
	expanded map[*zfs.PoolHistoryGroup]bool
	// internal is whether the events ZFS logged itself are shown in the chronological list
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
		expanded:      map[*zfs.PoolHistoryGroup]bool{},
	}
	d.content = &poolHistoryContent{}
	d.createLayout()
	go func() {
		entries, err := load()
		own := []zfs.PoolHistoryEntry{}
		for i := range entries {
			if entry := &entries[i]; entry.Pool == pool && (dataset == "" || entry.IsAbout(dataset)) {
				own = append(own, *entry)
			}
		}
		groups := zfs.GroupPoolHistory(own)
		groupOf := make(map[*zfs.PoolHistoryEntry]*zfs.PoolHistoryGroup, len(own))
		for _, group := range groups {
			for _, entry := range group.Entries {
				groupOf[entry] = group
			}
		}
		application.QueueUpdateDraw(func() { d.setEntries(own, groups, groupOf, err) })
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

	table := tview.NewTable().SetSelectable(true, false).SetFixed(1, 0).SetContent(d.content)
	table.SetSelectedStyle(tcell.StyleDefault.Foreground(theme.Colors.Layout.Table.SelectedForeground).Background(theme.Colors.Layout.Table.SelectedBackground))
	table.SetSelectionChangedFunc(func(row int, column int) { d.updateDetails() })
	util.IgnoreClicksBelowRows(table)
	d.table = &poolHistoryTable{Table: table, scrollbar: scrollbar.NewScrollbarComponent(d.application, scrollbar.ScrollBarVertical, 0, 0, 0, 0)}

	d.loadingView = util.NewLoadingView(d.application, "", "Reading the history (zpool history)…")
	d.loadingView.SetBorder(false)
	d.tableArea = tview.NewPages().
		AddPage(poolHistoryTablePage, d.table, true, false).
		AddPage(poolHistoryLoadingPage, d.loadingView, true, true)
	d.loadingView.Start()

	d.detailsView = tview.NewTextView().SetDynamicColors(true).SetWrap(true).SetWordWrap(true)
	d.hintView = util.CreateAttentionTextView("")

	content := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(d.filterInput, 1, 0, false).
		AddItem(d.tableArea, 0, 1, true).
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
		case event.Key() == tcell.KeyRune && event.Rune() == 'b':
			d.toggleChronological()
			return nil
		case event.Key() == tcell.KeyRune && event.Rune() == 'i' && d.chronological:
			d.internal = !d.internal
			d.applyFilter()
			return nil
		case event.Key() == tcell.KeyEnter && !d.chronological:
			if row := d.selectedRow(); row.entry == nil && row.group != nil {
				d.setExpanded(row.group, !d.expanded[row.group])
			}
			return nil
		case event.Key() == tcell.KeyRune && strings.ContainsRune("+-*", event.Rune()) && !d.chronological:
			d.handleTreeKey(event.Rune())
			return nil
		}
		return event
	})
	d.layout = layout
	d.updateHint()
}

// setEntries shows the records (oldest first) and their groups, or why they could not be read. Must be called on the
// UI thread.
func (d *PoolHistoryDialog) setEntries(entries []zfs.PoolHistoryEntry, groups []*zfs.PoolHistoryGroup, groupOf map[*zfs.PoolHistoryEntry]*zfs.PoolHistoryGroup, err error) {
	d.loadingView.Stop()
	// the table is shown before the loading view is hidden, so a page is always shown (see uiutil.LoadingContainer)
	hadFocus := d.tableArea.HasFocus()
	d.tableArea.ShowPage(poolHistoryTablePage)
	d.tableArea.HidePage(poolHistoryLoadingPage)
	if hadFocus {
		d.application.SetFocus(d.table)
	}
	d.all, d.groups, d.groupOf, d.err = entries, groups, groupOf, err
	if d.all == nil {
		d.all = []zfs.PoolHistoryEntry{}
	}
	d.applyFilter()
}

// applyFilter shows the groups or records that match the filter (in the event, datasets, details or user, ignoring
// case; a group if its summary or one of its records does), the chronological list with or without the internal
// events. The selected row stays selected if it is still shown. Must be called on the UI thread.
func (d *PoolHistoryDialog) applyFilter() {
	d.showRows(d.selectedRow())
}

// showRows fills the list, and selects the row of the group and record of selected if it is shown, else the first.
// Must be called on the UI thread.
func (d *PoolHistoryDialog) showRows(selected poolHistoryRow) {
	if d.all == nil {
		return
	}
	// the line the selection is drawn on, kept if it is still shown (e.g. a group that is expanded or collapsed)
	shown := len(d.content.rows) > 0
	selectedRow, _ := d.table.GetSelection()
	offset, _ := d.table.GetOffset()
	line := max(selectedRow-offset, 1)
	filter := strings.TrimSpace(d.filterInput.GetText())
	rows := d.content.rows[:0]
	if d.chronological {
		for i := len(d.all) - 1; i >= 0; i-- {
			entry := &d.all[i]
			if (d.internal || !entry.Internal) && (filter == "" || poolHistoryEntryMatches(entry, filter)) {
				rows = append(rows, poolHistoryRow{entry: entry})
			}
		}
	} else {
		for i := len(d.groups) - 1; i >= 0; i-- {
			group := d.groups[i]
			if filter != "" && !poolHistoryGroupMatches(group, filter) {
				continue
			}
			if group.Kind == zfs.PoolHistorySingle {
				rows = append(rows, poolHistoryRow{group: group, entry: group.Entries[0]})
				continue
			}
			rows = append(rows, poolHistoryRow{group: group, expanded: d.expanded[group]})
			if d.expanded[group] {
				for j := len(group.Entries) - 1; j >= 0; j-- {
					rows = append(rows, poolHistoryRow{group: group, entry: group.Entries[j], child: true, last: j == 0})
				}
			}
		}
	}
	d.content.rows = rows
	row := 1
	index := slices.IndexFunc(rows, func(row poolHistoryRow) bool {
		return row.group == selected.group && row.entry == selected.entry
	})
	if index >= 0 {
		row = index + 1
	}
	if shown && index >= 0 {
		// without, tview scrolls only as far as needed to show the selection: a group expanded further down the list
		// would end up on its last line, its records out of sight
		d.table.SetOffset(max(row-line, 0), 0)
	} else {
		// drawn empty (while loading, or with a filter nothing matches), the table follows its end, but the newest
		// records are at the top; the selection is scrolled to again
		d.table.ScrollToBeginning()
	}
	if len(rows) > 0 {
		d.table.Select(row, 0)
	}
	d.updateDetails()
	d.updateHint()
}

// toggleChronological switches between the groups and the records one by one. The selected record stays selected: a
// group selects its newest command (or event, if it has no commands), a record selects its group, expanded. The
// events ZFS logged are shown if an event is to be selected. Must be called on the UI thread.
func (d *PoolHistoryDialog) toggleChronological() {
	selected := d.selectedRow()
	d.chronological = !d.chronological
	if d.chronological {
		if selected.entry == nil && selected.group != nil {
			selected.entry = selected.group.Entries[len(selected.group.Entries)-1]
			if commands := selected.group.Commands(); len(commands) > 0 {
				selected.entry = commands[len(commands)-1]
			}
		}
		if selected.entry != nil && selected.entry.Internal {
			d.internal = true
		}
		d.showRows(poolHistoryRow{entry: selected.entry})
		return
	}
	if selected.entry != nil {
		selected.group = d.groupOf[selected.entry]
		if selected.group != nil && selected.group.Kind != zfs.PoolHistorySingle {
			d.expanded[selected.group] = true
		}
	}
	d.showRows(selected)
}

// setExpanded shows or hides the records of the group, and selects it. Must be called on the UI thread.
func (d *PoolHistoryDialog) setExpanded(group *zfs.PoolHistoryGroup, expanded bool) {
	if expanded {
		d.expanded[group] = true
	} else {
		delete(d.expanded, group)
	}
	d.showRows(poolHistoryRow{group: group})
}

// handleTreeKey expands and collapses the groups like the tree of the datasets (see
// dataset_browser.handleTreeKey): - collapses the selected group, or selects the group of a record; + expands it, or
// selects its newest record if it is expanded; * collapses all groups if any is expanded, else expands all. Must be
// called on the UI thread.
func (d *PoolHistoryDialog) handleTreeKey(key rune) {
	row := d.selectedRow()
	switch {
	case key == '*' && len(d.expanded) > 0:
		clear(d.expanded)
		if row.child {
			row.entry = nil
		}
		d.showRows(row)
	case key == '*':
		for _, group := range d.groups {
			if group.Kind != zfs.PoolHistorySingle {
				d.expanded[group] = true
			}
		}
		d.showRows(row)
	case row.group == nil || row.group.Kind == zfs.PoolHistorySingle:
	case key == '-' && row.child:
		d.showRows(poolHistoryRow{group: row.group})
	case key == '-':
		d.setExpanded(row.group, false)
	case key == '+' && row.entry == nil && !d.expanded[row.group]:
		d.setExpanded(row.group, true)
	case key == '+' && row.entry == nil:
		d.showRows(poolHistoryRow{group: row.group, entry: row.group.Entries[len(row.group.Entries)-1]})
	}
}

// poolHistoryGroupMatches returns whether the summary of the group, or one of its records, contains the filter,
// ignoring case.
func poolHistoryGroupMatches(group *zfs.PoolHistoryGroup, filter string) bool {
	if group.Kind != zfs.PoolHistorySingle {
		if summary, _ := poolHistoryGroupSummary(group); containsFold(poolHistoryGroupTitle(group), filter) || containsFold(summary, filter) {
			return true
		}
	}
	return slices.ContainsFunc(group.Entries, func(entry *zfs.PoolHistoryEntry) bool { return poolHistoryEntryMatches(entry, filter) })
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

// selectedRow returns the selected row, the zero row if none.
func (d *PoolHistoryDialog) selectedRow() poolHistoryRow {
	row, _ := d.table.GetSelection()
	if row < 1 || row > len(d.content.rows) {
		return poolHistoryRow{}
	}
	return d.content.rows[row-1]
}

// updateDetails shows the selected record, or the selected group, in full below the list. Must be called on the UI
// thread.
func (d *PoolHistoryDialog) updateDetails() {
	row := d.selectedRow()
	entry := row.entry
	if entry == nil && row.group != nil {
		d.detailsView.SetText(poolHistoryGroupDetails(row.group))
		return
	}
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
	switch {
	case d.err != nil:
		d.hintView.SetText(tview.Escape(fmt.Sprintf("Could not read the history: %s", d.err)))
	case d.all == nil:
		d.hintView.SetText("")
	case d.chronological:
		internalAction := "show"
		if d.internal {
			internalAction = "hide"
		}
		d.hintView.SetText(tview.Escape(fmt.Sprintf("%d of %d records · b: grouped · i: %s events logged by ZFS · /: filter · Esc: close",
			len(d.content.rows), len(d.all), internalAction)))
	default:
		shown := 0
		for _, row := range d.content.rows {
			if !row.child {
				shown++
			}
		}
		d.hintView.SetText(tview.Escape(fmt.Sprintf("%d of %d groups (%d records) · Enter/+/-/*: expand, collapse · b: each record · /: filter · Esc: close",
			shown, len(d.groups), len(d.all))))
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
	d.loadingView.Stop()
	go func() {
		d.actionChannel <- DialogCloseActionId
	}()
}

// poolHistoryTable is the list, with a scrollbar on its right while not all rows fit (like table.RowSelectionTable).
// The bar is drawn after the table: tview sets the row offset while drawing it (e.g. to keep the selection visible).
type poolHistoryTable struct {
	*tview.Table
	scrollbar *scrollbar.ScrollbarComponent
}

func (t *poolHistoryTable) Draw(screen tcell.Screen) {
	x, y, width, height := t.GetRect()
	// without the (fixed) header row
	rows := t.GetRowCount() - 1
	if rows <= height-1 || width < 2 {
		t.Table.Draw(screen)
		return
	}
	t.SetRect(x, y, width-1, height)
	t.Table.Draw(screen)
	t.SetRect(x, y, width, height)
	offset, _ := t.GetOffset()
	t.scrollbar.SetMax(rows)
	t.scrollbar.SetPosition(offset)
	t.scrollbar.SetWidth(max(height-1, 1))
	layout := t.scrollbar.GetLayout()
	layout.SetRect(x+width-1, y, 1, height)
	layout.Draw(screen)
}

// poolHistoryContent are the rows of the list, created when drawn (see tview.TableContent): a table with cells for
// hundreds of thousands of records would take long to fill and much memory.
type poolHistoryContent struct {
	tview.TableContentReadOnly
	// rows are the rows shown, newest first. Only accessed on the UI thread.
	rows []poolHistoryRow
}

// poolHistoryRow is a row of the list: a group (entry nil), a record of an expanded group (child), a group of one
// record, or a record of the chronological list (group nil).
type poolHistoryRow struct {
	group *zfs.PoolHistoryGroup
	entry *zfs.PoolHistoryEntry
	// child is set for the records below an expanded group, last for its oldest one (drawn as the end of the tree)
	child, last bool
	// expanded is set for a group whose records are shown below it
	expanded bool
}

func (c *poolHistoryContent) GetRowCount() int {
	return len(c.rows) + 1
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
	if row > len(c.rows) {
		return nil
	}
	shown := c.rows[row-1]
	if shown.entry == nil {
		return poolHistoryGroupCell(shown.group, column, shown.expanded)
	}
	entry := shown.entry
	color := theme.Colors.Info.Value
	if entry.Internal {
		color = theme.Colors.Info.Muted
	}
	switch column {
	case 0:
		return tview.NewTableCell(entry.Time.Format(poolHistoryTimeLayout)).SetTextColor(theme.Colors.Info.Label)
	case 1:
		prefix := ""
		if shown.child && shown.last {
			prefix = txwidgets.Span(theme.Colors.Layout.Table.TreeLines, "└─ ")
		} else if shown.child {
			prefix = txwidgets.Span(theme.Colors.Layout.Table.TreeLines, "├─ ")
		}
		return tview.NewTableCell(prefix + tview.Escape(entry.Event)).SetTextColor(color)
	case 2:
		return tview.NewTableCell(tview.Escape(entry.Dataset())).SetTextColor(theme.Colors.Layout.Table.Accent).SetMaxWidth(50)
	case 3:
		return tview.NewTableCell(tview.Escape(entry.User)).SetTextColor(theme.Colors.Info.Label)
	default:
		return tview.NewTableCell(tview.Escape(poolHistoryDetails(entry))).SetTextColor(color).SetExpansion(1)
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
