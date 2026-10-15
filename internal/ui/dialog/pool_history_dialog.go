package dialog

import (
	"fmt"
	"slices"
	"strings"
	"sync"
	"unicode/utf8"
	"zfs-file-history/internal/state"
	"zfs-file-history/internal/ui/scrollbar"
	"zfs-file-history/internal/ui/table"
	"zfs-file-history/internal/ui/theme"
	"zfs-file-history/internal/ui/txwidgets"
	"zfs-file-history/internal/ui/util"
	"zfs-file-history/internal/zfs"

	"github.com/dustin/go-humanize"
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

// The columns of the list. The details are last by default: they can be long (e.g. the devices of zpool create), and
// are cut off where the list ends, not the columns after them; the selected row is shown in full below the list.
var (
	poolHistoryColumnTime    = &table.Column{Id: 0, Key: "time", Title: "Time", Alignment: tview.AlignLeft}
	poolHistoryColumnEvent   = &table.Column{Id: 1, Key: "event", Title: "Event", Alignment: tview.AlignLeft}
	poolHistoryColumnDataset = &table.Column{Id: 2, Key: "dataset", Title: "Dataset", Alignment: tview.AlignLeft}
	poolHistoryColumnUser    = &table.Column{Id: 3, Key: "user", Title: "User", Alignment: tview.AlignLeft}
	poolHistoryColumnDetails = &table.Column{Id: 4, Key: "details", Title: "Details", Alignment: tview.AlignLeft}
	poolHistoryColumns       = []*table.Column{poolHistoryColumnTime, poolHistoryColumnEvent, poolHistoryColumnDataset, poolHistoryColumnUser, poolHistoryColumnDetails}
)

// poolHistoryDefaultLayout shows all columns, the newest first.
var poolHistoryDefaultLayout = table.ColumnLayout{Columns: poolHistoryColumns, SortColumn: poolHistoryColumnTime, SortInverted: true}

// PoolHistoryDialog lists the history of a pool, or of a dataset (with its children and snapshots), newest first
// (see zfs.PoolHistory). The records are grouped into what was done (see zfs.GroupPoolHistory: a scrub from its start
// to its end, a command with the events ZFS logged for it, the commands of a run of a tool), each group a row that is
// expanded to its records like the tree of the datasets (Enter, +, -, *). b lists the records one by one instead, in
// the order they were logged: the commands, and with i the events ZFS logged itself. / filters, the selected row is
// shown in full below the list. The rows are sorted by a column (the groups, their records stay below them), chosen on
// the header row like in the other tables; F2 chooses the columns. A pool may have hundreds of thousands of records,
// so the list is a virtual table (see poolHistoryContent), not a table.RowSelectionTable.
type PoolHistoryDialog struct {
	application *tview.Application
	// pages are the pages the dialog is shown on, for the column dialog (see pagesAware)
	pages         *tview.Pages
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
	// err is why the history could not be read (the last time), nil if it was
	err error
	// state is the state of the history of the pool shown, see PoolHistoryCache.Watch
	state PoolHistoryState
	// shown is the history the records are of (see viewOf), nil while none was read
	shown *PoolHistory
	// spinner is the frame of the spinner of the hint while the history is read again, see readingHint
	spinner int
	// stopWatching stops the updates of the history of the pool, see PoolHistoryCache.Watch
	stopWatching func()

	// viewMutex guards viewSource and view, the history of the dataset taken from the one of the pool in the
	// background (see viewOf).
	viewMutex  sync.Mutex
	viewSource *PoolHistory
	view       *PoolHistory
	// chronological is whether the records are listed one by one instead of grouped
	chronological bool
	// expanded are the groups whose records are shown below them
	expanded map[*zfs.PoolHistoryGroup]bool
	// internal is whether the events ZFS logged itself are shown in the chronological list
	internal bool
	// groupOrder and entryOrder are the indices of groups and all in the order they are shown, see sortRows
	groupOrder, entryOrder []int
	// headerWasSelected is whether the header row was selected when the mouse button went down, see handleHeaderClick
	headerWasSelected bool
}

// NewPoolHistoryDialog shows the history of the pool, or of the dataset (with its children and snapshots) if dataset
// is not "", from the cache: the one read before right away, while it is read again (see PoolHistoryCache.Watch). The
// events ZFS logged itself are shown for a dataset from the start: a recursive command names only the dataset it was
// given, the events name each dataset it changed. Must be called on the UI thread.
func NewPoolHistoryDialog(application *tview.Application, pool string, dataset string, cache *PoolHistoryCache) *PoolHistoryDialog {
	d := &PoolHistoryDialog{
		application:   application,
		actionChannel: make(chan DialogActionId),
		pool:          pool,
		dataset:       dataset,
		internal:      dataset != "",
		expanded:      map[*zfs.PoolHistoryGroup]bool{},
	}
	d.content = &poolHistoryContent{}
	d.applyColumnLayout(poolHistoryDefaultLayout)
	if layout, ok := table.LoadColumnLayout(state.Current, stateKeyPoolHistoryTable, poolHistoryColumns); ok {
		d.applyColumnLayout(layout)
	}
	d.createLayout()
	current, stop := cache.Watch(pool, d.onState)
	d.stopWatching = stop
	go d.onState(current)
	return d
}

// onState shows the state of the history of the pool: the records of the dataset are taken from it here, in the
// background (see viewOf). Called from the goroutine of the read, or of the dialog.
func (d *PoolHistoryDialog) onState(historyState PoolHistoryState) {
	view := d.viewOf(historyState.History)
	d.application.QueueUpdateDraw(func() { d.setState(historyState, view) })
}

// viewOf returns the records of the history that are shown: of the pool all, of a dataset the ones about it,
// grouped again. Taken once for each history read. May take long for a large pool, so it must not be called on the UI
// thread.
func (d *PoolHistoryDialog) viewOf(history *PoolHistory) *PoolHistory {
	if history == nil || d.dataset == "" {
		return history
	}
	d.viewMutex.Lock()
	defer d.viewMutex.Unlock()
	if d.viewSource != history {
		var own []zfs.PoolHistoryEntry
		for i := range history.Entries {
			if entry := &history.Entries[i]; entry.IsAbout(d.dataset) {
				own = append(own, *entry)
			}
		}
		d.viewSource, d.view = history, groupPoolHistory(own)
	}
	return d.view
}

// setState shows the state of the history, unless a newer one is shown already: the records of a new history (see
// setHistory), the progress of a read. Must be called on the UI thread.
func (d *PoolHistoryDialog) setState(historyState PoolHistoryState, view *PoolHistory) {
	if historyState.Version <= d.state.Version && d.state.Version != 0 {
		return
	}
	if historyState.Reading {
		d.spinner++
	}
	d.state, d.err = historyState, historyState.Err
	d.loadingView.SetMessage("Reading the history (zpool history)… " + poolHistoryProgressText(historyState))
	if view != nil && view != d.shown {
		d.setHistory(view)
	} else if view == nil && !historyState.Reading {
		// could not be read, and none was before
		d.showTable()
		d.all, d.groups, d.groupOf = []zfs.PoolHistoryEntry{}, nil, nil
		d.sortRows()
		d.applyFilter()
	}
	d.updateHint()
}

// poolHistoryProgressText tells how far the history was read, e.g. "12% · 40,123 records · 23 MB": the share of the
// size of the history read before, if known.
func poolHistoryProgressText(historyState PoolHistoryState) string {
	text := fmt.Sprintf("%s records · %s", humanize.Comma(int64(historyState.Progress.Records)), humanize.IBytes(uint64(historyState.Progress.Bytes)))
	if historyState.Expected > 0 {
		percent := min(100*historyState.Progress.Bytes/historyState.Expected, 99)
		text = fmt.Sprintf("%d%% · %s", percent, text)
	}
	return text
}

// showTable shows the table instead of the loading view. Must be called on the UI thread.
func (d *PoolHistoryDialog) showTable() {
	d.loadingView.Stop()
	// the table is shown before the loading view is hidden, so a page is always shown (see uiutil.LoadingContainer)
	hadFocus := d.tableArea.HasFocus()
	d.tableArea.ShowPage(poolHistoryTablePage)
	d.tableArea.HidePage(poolHistoryLoadingPage)
	if hadFocus {
		d.application.SetFocus(d.table)
	}
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

	list := tview.NewTable().SetSelectable(true, false).SetFixed(1, 0).SetContent(d.content)
	list.SetSelectedStyle(tcell.StyleDefault.Foreground(theme.Colors.Layout.Table.SelectedForeground).Background(theme.Colors.Layout.Table.SelectedBackground))
	list.SetSelectionChangedFunc(func(row int, column int) {
		if row == 0 {
			// tview keeps the offset when the (fixed) header row is selected, e.g. with PgUp: the first rows would
			// stay out of sight
			d.table.ScrollToBeginning()
		}
		d.updateDetails()
		d.updateHint()
	})
	util.IgnoreClicksBelowRows(list)
	ignoreClicksBelowRows := list.GetMouseCapture()
	list.SetMouseCapture(func(action tview.MouseAction, event *tcell.EventMouse) (tview.MouseAction, *tcell.EventMouse) {
		action, event = ignoreClicksBelowRows(action, event)
		if event == nil {
			return action, event
		}
		return d.handleHeaderClick(action, event)
	})
	d.table = &poolHistoryTable{Table: list, scrollbar: scrollbar.NewScrollbarComponent(d.application, scrollbar.ScrollBarVertical, 0, 0, 0, 0)}

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
		case event.Key() == tcell.KeyF2:
			d.openColumnDialog()
			return nil
		case d.isHeaderSelected() && (event.Key() == tcell.KeyLeft || event.Key() == tcell.KeyRight):
			step := 1
			if event.Key() == tcell.KeyLeft {
				step = -1
			}
			columns := d.content.columns
			index := slices.Index(columns, d.content.sortColumn)
			d.setSort(columns[(index+step+len(columns))%len(columns)], d.content.sortInverted)
			return nil
		case d.isHeaderSelected() && event.Key() == tcell.KeyEnter:
			d.setSort(d.content.sortColumn, !d.content.sortInverted)
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

// setHistory shows the records of the history. A history read again (or with the records ZFS reported, see
// PoolHistoryCache.Record) replaces the one shown only if it differs, keeping the selected row, the expanded groups,
// the filter and the view: the selected row stays on its line while new records come in above it, unless the list
// was scrolled to its top, where the new ones are shown. Must be called on the UI thread.
func (d *PoolHistoryDialog) setHistory(history *PoolHistory) {
	previous := d.shown
	d.shown = history
	if previous != nil && samePoolHistory(previous, history) {
		return
	}
	d.showTable()
	offset, _ := d.table.GetOffset()
	atTop := previous != nil && offset == 0
	selected := d.selectedRow()
	expanded := map[poolHistoryEntryKey]bool{}
	for group := range d.expanded {
		expanded[poolHistoryKeyOf(group.Entries[0])] = true
	}
	d.all, d.groups, d.groupOf = history.Entries, history.Groups, history.GroupOf
	clear(d.expanded)
	for _, group := range d.groups {
		if expanded[poolHistoryKeyOf(group.Entries[0])] {
			d.expanded[group] = true
		}
	}
	d.sortRows()
	d.showRows(d.findRow(selected))
	if atTop {
		d.table.SetOffset(0, 0)
	}
}

// poolHistoryEntryKey identifies a record across reads of the history: ZFS drops the oldest records once its ring
// buffer is full, so their index changes.
type poolHistoryEntryKey struct {
	// time is the second of the record (as listed, see zfs.PoolHistory)
	time     int64
	internal bool
	txg      uint64
	event    string
	text     string
}

// poolHistoryKeyOf returns the key of the record.
func poolHistoryKeyOf(entry *zfs.PoolHistoryEntry) poolHistoryEntryKey {
	return poolHistoryEntryKey{time: entry.Time.Unix(), internal: entry.Internal, txg: entry.Txg, event: entry.Event, text: entry.Text}
}

// samePoolHistory returns whether the histories have the same records: nothing was added or dropped (the last and
// first record are the same, and their number).
func samePoolHistory(a *PoolHistory, b *PoolHistory) bool {
	if len(a.Entries) != len(b.Entries) {
		return false
	}
	if len(a.Entries) == 0 {
		return true
	}
	last := len(a.Entries) - 1
	return poolHistoryKeyOf(&a.Entries[0]) == poolHistoryKeyOf(&b.Entries[0]) &&
		poolHistoryKeyOf(&a.Entries[last]) == poolHistoryKeyOf(&b.Entries[last])
}

// findRow returns the row of the records shown that is the row of another history (the same group and record, see
// poolHistoryEntryKey), the zero row if there is none.
func (d *PoolHistoryDialog) findRow(row poolHistoryRow) poolHistoryRow {
	var found poolHistoryRow
	if row.entry != nil {
		key := poolHistoryKeyOf(row.entry)
		// from the newest, where the selection usually is
		for i := len(d.all) - 1; i >= 0; i-- {
			if poolHistoryKeyOf(&d.all[i]) == key {
				found.entry = &d.all[i]
				break
			}
		}
		if found.entry == nil {
			return poolHistoryRow{}
		}
	}
	if row.group != nil {
		found.group = d.groupOf[found.entry]
		if found.entry == nil {
			key := poolHistoryKeyOf(row.group.Entries[0])
			for i := len(d.groups) - 1; i >= 0; i-- {
				if poolHistoryKeyOf(d.groups[i].Entries[0]) == key {
					found.group = d.groups[i]
					break
				}
			}
		}
	}
	return found
}

// applyColumnLayout shows the columns of the layout and sorts by its column; by the time, the newest first, if its
// column is not shown (or the first column if the time is not shown either). Does not sort the rows again (see
// sortRows). Must be called on the UI thread.
func (d *PoolHistoryDialog) applyColumnLayout(layout table.ColumnLayout) {
	if len(layout.Columns) == 0 {
		return
	}
	d.content.columns = slices.Clone(layout.Columns)
	d.content.sortColumn, d.content.sortInverted = layout.SortColumn, layout.SortInverted
	if !slices.Contains(d.content.columns, d.content.sortColumn) {
		d.content.sortColumn, d.content.sortInverted = poolHistoryDefaultLayout.SortColumn, poolHistoryDefaultLayout.SortInverted
		if !slices.Contains(d.content.columns, d.content.sortColumn) {
			d.content.sortColumn = d.content.columns[0]
		}
	}
}

// columnLayout returns the columns shown and the sort order.
func (d *PoolHistoryDialog) columnLayout() table.ColumnLayout {
	return table.ColumnLayout{Columns: slices.Clone(d.content.columns), SortColumn: d.content.sortColumn, SortInverted: d.content.sortInverted}
}

// changeColumnLayout shows the columns of the layout, sorted by its column, and saves it. Must be called on the UI
// thread.
func (d *PoolHistoryDialog) changeColumnLayout(layout table.ColumnLayout) {
	d.applyColumnLayout(layout)
	table.SaveColumnLayout(state.Current, stateKeyPoolHistoryTable, d.columnLayout())
	d.sortRows()
	d.showRows(d.selectedRow())
}

// setSort sorts the rows by the column, descending if inverted, and saves it. Must be called on the UI thread.
func (d *PoolHistoryDialog) setSort(column *table.Column, inverted bool) {
	layout := d.columnLayout()
	layout.SortColumn, layout.SortInverted = column, inverted
	d.changeColumnLayout(layout)
}

// openColumnDialog lets the user choose and order the columns (F2), like in the other tables. Must be called on the
// UI thread.
func (d *PoolHistoryDialog) openColumnDialog() {
	if d.pages == nil {
		return
	}
	columns := NewColumnSelectionDialog(d.application, "Configure History Columns", poolHistoryColumns, d.content.columns,
		func(columns []*table.Column) {
			layout := d.columnLayout()
			layout.Columns = columns
			d.changeColumnLayout(layout)
		})
	columns.SetResetFunc(func() []*table.Column {
		d.applyColumnLayout(poolHistoryDefaultLayout)
		state.Current.DeleteTableLayout(stateKeyPoolHistoryTable)
		d.sortRows()
		d.showRows(d.selectedRow())
		return slices.Clone(d.content.columns)
	})
	ShowDialogOnPages(d.application, d.pages, columns, nil)
}

func (d *PoolHistoryDialog) setPages(pages *tview.Pages) {
	d.pages = pages
}

// isHeaderSelected returns whether the header row is selected, where ← and → choose the sort column and Enter the
// direction.
func (d *PoolHistoryDialog) isHeaderSelected() bool {
	row, _ := d.table.GetSelection()
	return row == 0 && len(d.content.rows) > 0
}

// handleHeaderClick handles clicks on the header row like table.RowSelectionTable: the first click selects it, the
// next ones sort by the clicked column, or toggle the direction if it is the sort column already. Clicks elsewhere
// are passed on. Runs as the mouse capture of the table.
func (d *PoolHistoryDialog) handleHeaderClick(action tview.MouseAction, event *tcell.EventMouse) (tview.MouseAction, *tcell.EventMouse) {
	x, y := event.Position()
	switch action {
	case tview.MouseLeftDown:
		// before tview focuses the table: the click that selects the header must not sort as well
		d.headerWasSelected = d.isHeaderSelected() && d.table.HasFocus()
	case tview.MouseLeftClick:
		if !d.table.InRect(x, y) || !d.headerWasSelected {
			return action, event
		}
		row, column := d.table.CellAt(x, y)
		if row != 0 || column < 0 || column >= len(d.content.columns) {
			return action, event
		}
		if clicked := d.content.columns[column]; clicked == d.content.sortColumn {
			d.setSort(clicked, !d.content.sortInverted)
		} else {
			d.setSort(clicked, d.content.sortInverted)
		}
		return tview.MouseConsumed, nil
	default:
	}
	return action, event
}

// sortRows puts the groups and the records in the order of the sort column (see poolHistoryOrder). Sorted once, not
// on each change of the filter: a pool may have hundreds of thousands of records. Must be called on the UI thread.
func (d *PoolHistoryDialog) sortRows() {
	if d.all == nil {
		return
	}
	column := d.content.sortColumn
	d.groupOrder = poolHistoryOrder(len(d.groups), column, d.content.sortInverted, func(i int) string {
		return poolHistoryGroupSortKey(d.groups[i], column)
	})
	d.entryOrder = poolHistoryOrder(len(d.all), column, d.content.sortInverted, func(i int) string {
		return poolHistoryEntrySortKey(&d.all[i], column)
	})
}

// poolHistoryOrder returns the indices of n records or groups (oldest first, in the order they were logged) in the
// order they are shown: by the time in the order they were logged, else by the key of the column, the newest first
// where the keys are the same.
func poolHistoryOrder(n int, column *table.Column, inverted bool, key func(i int) string) []int {
	order := make([]int, n)
	for i := range order {
		order[i] = n - 1 - i
	}
	if column == poolHistoryColumnTime {
		if !inverted {
			slices.Reverse(order)
		}
		return order
	}
	keys := make([]string, n)
	for i := range keys {
		keys[i] = key(i)
	}
	slices.SortStableFunc(order, func(a int, b int) int {
		result := strings.Compare(keys[a], keys[b])
		if inverted {
			return -result
		}
		return result
	})
	return order
}

// poolHistoryGroupSortKey returns the text of the group in the column, without colors, to sort by.
func poolHistoryGroupSortKey(group *zfs.PoolHistoryGroup, column *table.Column) string {
	if group.Kind == zfs.PoolHistorySingle {
		return poolHistoryEntrySortKey(group.Entries[0], column)
	}
	switch column {
	case poolHistoryColumnEvent:
		return poolHistoryGroupTitle(group)
	case poolHistoryColumnDataset:
		return poolHistoryCommonDataset(group)
	case poolHistoryColumnUser:
		return strings.Join(poolHistoryGroupUsers(group), ", ")
	case poolHistoryColumnDetails:
		summary, _ := poolHistoryGroupSummary(group)
		return summary
	}
	return ""
}

// poolHistoryEntrySortKey returns the text of the record in the column, to sort by.
func poolHistoryEntrySortKey(entry *zfs.PoolHistoryEntry, column *table.Column) string {
	switch column {
	case poolHistoryColumnEvent:
		return entry.Event
	case poolHistoryColumnDataset:
		return entry.Dataset()
	case poolHistoryColumnUser:
		return entry.User
	case poolHistoryColumnDetails:
		return poolHistoryDetails(entry)
	}
	return ""
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
	// e.g. while the sort column is chosen
	headerSelected := shown && selectedRow == 0
	filter := strings.TrimSpace(d.filterInput.GetText())
	rows := d.content.rows[:0]
	// the records of a group are listed newest first, unless the rows are sorted by the time, oldest first
	oldestFirst := d.content.sortColumn == poolHistoryColumnTime && !d.content.sortInverted
	if d.chronological {
		for _, i := range d.entryOrder {
			entry := &d.all[i]
			if (d.internal || !entry.Internal) && (filter == "" || poolHistoryEntryMatches(entry, filter)) {
				rows = append(rows, poolHistoryRow{entry: entry})
			}
		}
	} else {
		for _, i := range d.groupOrder {
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
				for k := range group.Entries {
					j := len(group.Entries) - 1 - k
					if oldestFirst {
						j = k
					}
					rows = append(rows, poolHistoryRow{group: group, entry: group.Entries[j], child: true, last: k == len(group.Entries)-1})
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
	if headerSelected && len(rows) > 0 {
		d.table.Select(0, 0)
	} else if len(rows) > 0 {
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
		// its first record, below it
		first := row.group.Entries[len(row.group.Entries)-1]
		if d.content.sortColumn == poolHistoryColumnTime && !d.content.sortInverted {
			first = row.group.Entries[0]
		}
		d.showRows(poolHistoryRow{group: row.group, entry: first})
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
	case d.err != nil && d.shown != nil:
		d.hintView.SetText(tview.Escape(fmt.Sprintf("Could not read the history again, it is from before: %s", d.err)))
	case d.err != nil:
		d.hintView.SetText(tview.Escape(fmt.Sprintf("Could not read the history: %s", d.err)))
	case d.all == nil:
		d.hintView.SetText("")
	case d.state.Reading && !d.isHeaderSelected():
		d.hintView.SetText(d.readingHint())
	case d.isHeaderSelected():
		d.hintView.SetText("←/→: sort column · Enter: sort direction · F2: columns · Esc: close")
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

// readingHint tells that the history shown is read again, and how far: with a spinner, which moves with each report
// of the progress (see zfs.PoolHistory).
func (d *PoolHistoryDialog) readingHint() string {
	frame := util.SpinnerFrames[d.spinner%len(util.SpinnerFrames)]
	return tview.Escape(fmt.Sprintf("%s Reading the history again, this is the one from before… %s", frame, poolHistoryProgressText(d.state)))
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
	d.stopWatching()
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
	// rows are the rows shown, in the order of the sort column. Only accessed on the UI thread.
	rows []poolHistoryRow
	// columns are the columns shown, in their order. Only accessed on the UI thread.
	columns []*table.Column
	// sortColumn is the column the rows are sorted by, sortInverted whether descending (e.g. the newest first). Only
	// accessed on the UI thread.
	sortColumn   *table.Column
	sortInverted bool
}

// poolHistoryRow is a row of the list: a group (entry nil), a record of an expanded group (child), a group of one
// record, or a record of the chronological list (group nil).
type poolHistoryRow struct {
	group *zfs.PoolHistoryGroup
	entry *zfs.PoolHistoryEntry
	// child is set for the records below an expanded group, last for the one drawn last (the end of the tree)
	child, last bool
	// expanded is set for a group whose records are shown below it
	expanded bool
}

func (c *poolHistoryContent) GetRowCount() int {
	return len(c.rows) + 1
}

func (c *poolHistoryContent) GetColumnCount() int {
	return len(c.columns)
}

func (c *poolHistoryContent) GetCell(row int, index int) *tview.TableCell {
	if index < 0 || index >= len(c.columns) {
		return nil
	}
	column := c.columns[index]
	if row == 0 {
		return c.headerCell(column)
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
	case poolHistoryColumnTime:
		return tview.NewTableCell(entry.Time.Format(poolHistoryTimeLayout)).SetTextColor(theme.Colors.Info.Label)
	case poolHistoryColumnEvent:
		prefix := ""
		if shown.child && shown.last {
			prefix = txwidgets.Span(theme.Colors.Layout.Table.TreeLines, "└─ ")
		} else if shown.child {
			prefix = txwidgets.Span(theme.Colors.Layout.Table.TreeLines, "├─ ")
		}
		return tview.NewTableCell(prefix + tview.Escape(entry.Event)).SetTextColor(color)
	case poolHistoryColumnDataset:
		return tview.NewTableCell(tview.Escape(entry.Dataset())).SetTextColor(theme.Colors.Layout.Table.Accent).SetMaxWidth(50)
	case poolHistoryColumnUser:
		return tview.NewTableCell(tview.Escape(entry.User)).SetTextColor(theme.Colors.Info.Label)
	default:
		return tview.NewTableCell(tview.Escape(poolHistoryDetails(entry))).SetTextColor(color).SetExpansion(1)
	}
}

// headerCell returns the cell of the column in the header row, with the direction of the sort column (like
// table.RowSelectionTable), highlighted while the header row is selected.
func (c *poolHistoryContent) headerCell(column *table.Column) *tview.TableCell {
	text := column.Title
	if column == c.sortColumn && c.sortInverted {
		text += " ↓"
	} else if column == c.sortColumn {
		text += " ↑"
	}
	cell := tview.NewTableCell(text).SetTextColor(theme.Colors.Layout.Table.HeaderForeground).
		SetBackgroundColor(theme.Colors.Layout.Table.HeaderBackground).SetAttributes(tcell.AttrBold)
	if column == c.sortColumn {
		// a cell's selected style is only applied while its row is selected
		cell.SetSelectedStyle(tcell.StyleDefault.Foreground(theme.Colors.Layout.Table.SortColumnSelectedForeground).
			Background(theme.Colors.Layout.Table.SortColumnSelectedBackground))
	}
	return cell
}

// poolHistoryDetails returns what the record says besides its event and dataset: the arguments of a command (e.g.
// "-r tank/data@daily"), the text of an internal event (e.g. "errors=0").
func poolHistoryDetails(entry *zfs.PoolHistoryEntry) string {
	if entry.Internal {
		return entry.Text
	}
	return strings.TrimSpace(strings.TrimPrefix(entry.Text, entry.Event))
}
