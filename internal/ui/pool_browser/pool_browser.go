package pool_browser

import (
	"cmp"
	"context"
	"fmt"
	"sort"
	"strings"
	"zfs-file-history/internal/logging"
	"zfs-file-history/internal/state"
	"zfs-file-history/internal/ui/dialog"
	"zfs-file-history/internal/ui/pool_status"
	"zfs-file-history/internal/ui/shortcut_helper"
	"zfs-file-history/internal/ui/status_message"
	"zfs-file-history/internal/ui/table"
	"zfs-file-history/internal/ui/theme"
	"zfs-file-history/internal/ui/txwidgets"
	uiutil "zfs-file-history/internal/ui/util"
	"zfs-file-history/internal/util"
	"zfs-file-history/internal/zfs"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

var (
	columnName = &table.Column{
		Id:        0,
		Key:       "name",
		Title:     "Name",
		Alignment: tview.AlignLeft,
	}
	columnHealth = &table.Column{
		Id:        1,
		Key:       "health",
		Title:     "Health",
		Alignment: tview.AlignLeft,
	}
	columnSize = &table.Column{
		Id:        2,
		Key:       "size",
		Title:     "Size",
		Alignment: tview.AlignRight,
	}
	columnAllocated = &table.Column{
		Id:        3,
		Key:       "allocated",
		Title:     "Alloc",
		Alignment: tview.AlignRight,
	}
	columnFree = &table.Column{
		Id:        4,
		Key:       "free",
		Title:     "Free",
		Alignment: tview.AlignRight,
	}
	columnCapacity = &table.Column{
		Id:        5,
		Key:       "capacity",
		Title:     "Cap",
		Alignment: tview.AlignRight,
	}
	columnFragmentation = &table.Column{
		Id:        6,
		Key:       "fragmentation",
		Title:     "Frag",
		Alignment: tview.AlignRight,
	}
	columnDedup = &table.Column{
		Id:        7,
		Key:       "dedup",
		Title:     "Dedup",
		Alignment: tview.AlignRight,
	}

	tableColumns = []*table.Column{
		columnName,
		columnHealth,
		columnSize,
		columnAllocated,
		columnFree,
		columnCapacity,
		columnFragmentation,
		columnDedup,
	}
)

// stateKeyTable is the key of the column layout remembered in state.Current
const stateKeyTable = "poolBrowser"

// listPools is replaceable in tests.
var listPools = zfs.ListPools

type Event interface{}

type SelectedPoolChangedEvent struct {
	Pool *zfs.PoolListEntry
}

type PoolBrowserStatusEvent struct {
	Message *status_message.StatusMessage
}

// PoolBrowserComponent lists the pools, like "zpool list".
type PoolBrowserComponent struct {
	Events *util.Emitter[Event]

	application    *tview.Application
	layout         *tview.Pages
	tableContainer *table.RowSelectionTable[zfs.PoolListEntry]
	loader         *uiutil.DataLoader[[]*zfs.PoolListEntry]

	// allEntries are all pools of the last load, nil until the first one. Only accessed on the UI thread.
	allEntries []*zfs.PoolListEntry
	// initialSelection is the name of the pool to select once the pools are loaded, see SelectOnLoad.
	// Only accessed on the UI thread.
	initialSelection string
	// dialogPages are the pages dialogs are shown on, see SetDialogPages
	dialogPages *tview.Pages
}

func NewPoolBrowser(application *tview.Application) *PoolBrowserComponent {
	poolBrowser := &PoolBrowserComponent{
		Events:      util.NewEmitter[Event](),
		application: application,
	}

	poolBrowser.tableContainer = table.NewTableContainer[zfs.PoolListEntry](
		application,
		toTableCells,
		sortPoolEntries,
	)
	poolBrowser.layout = tview.NewPages().
		AddPage("pool-browser", poolBrowser.tableContainer.GetLayout(), true, true)
	poolBrowser.setupTable()

	poolBrowser.loader = uiutil.NewDataLoader[[]*zfs.PoolListEntry](application).
		OnLoad(poolBrowser.onPoolsLoaded).
		OnError(func(err error) {
			logging.Error("Could not load pools: %s", err.Error())
			poolBrowser.Events.Emit(PoolBrowserStatusEvent{
				Message: status_message.NewErrorStatusMessage(fmt.Sprintf("Could not load pools: %s", err.Error())),
			})
		})

	return poolBrowser
}

func (poolBrowser *PoolBrowserComponent) setupTable() {
	poolBrowser.tableContainer.SetTitle("Pools")
	poolBrowser.tableContainer.SetFilterFunc(func(entry *zfs.PoolListEntry, filterText string) bool {
		return table.MatchesGlob(entry.Name, filterText)
	})
	poolBrowser.tableContainer.SetFilterChangedCallback(poolBrowser.updateStatus)
	poolBrowser.updateStatus()
	poolBrowser.tableContainer.SetColumnSpec(tableColumns, columnName, false)
	poolBrowser.tableContainer.SetActiveColumns(tableColumns)
	poolBrowser.tableContainer.BindColumnLayout(state.Current, stateKeyTable, tableColumns)

	poolBrowser.tableContainer.SetSelectionChangedCallback(func(selectedEntry *zfs.PoolListEntry) {
		poolBrowser.Events.Emit(SelectedPoolChangedEvent{selectedEntry})
	})

	poolBrowser.tableContainer.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyF2 {
			poolBrowser.openColumnSelectionDialog()
			return nil
		}
		if poolBrowser.tableContainer.GetSelectedEntry() != nil && event.Key() == tcell.KeyEnter {
			// no action on data rows (yet), but don't let tview handle it either
			return nil
		}
		return event
	})
}

// toTableCells runs on the UI thread and must only use the pre-fetched listing values.
func toTableCells(row int, columns []*table.Column, entry *zfs.PoolListEntry) (cells []*tview.TableCell) {
	for _, column := range columns {
		var text string
		var color tcell.Color = tcell.ColorDefault
		alignment := column.Alignment

		switch column {
		case columnName:
			text = tview.Escape(entry.Name)
		case columnHealth:
			text = entry.Health
			color = pool_status.StateColor(entry.Health, color)
		case columnSize:
			text = uiutil.StableLengthHumanizedBytes(entry.Size)
		case columnAllocated:
			text = uiutil.StableLengthHumanizedBytes(entry.Allocated)
		case columnFree:
			text = uiutil.StableLengthHumanizedBytes(entry.Free)
		case columnCapacity:
			text = fmt.Sprintf("%d%%", entry.Capacity)
			// warm when the pool fills up
			color = uiutil.GradientColor(theme.Colors.Magnitude, float64(entry.Capacity)/100, color)
		case columnFragmentation:
			text = formatPercent(entry.Fragmentation)
		case columnDedup:
			text = entry.DedupRatio + "x"
		}

		cells = append(cells, tview.NewTableCell(text).
			SetTextColor(color).
			SetAlign(alignment))
	}
	return cells
}

// formatPercent formats a percentage, or "-" if it is not known (-1).
func formatPercent(value int) string {
	if value < 0 {
		return "-"
	}
	return fmt.Sprintf("%d%%", value)
}

// sortPoolEntries sorts the displayed entries by the column, and by name if equal.
func sortPoolEntries(entries []*zfs.PoolListEntry, column *table.Column, inverted bool) []*zfs.PoolListEntry {
	sort.SliceStable(entries, func(i, j int) bool {
		a := entries[i]
		b := entries[j]

		result := 0
		switch column {
		case columnHealth:
			result = strings.Compare(a.Health, b.Health)
		case columnSize:
			result = cmp.Compare(a.Size, b.Size)
		case columnAllocated:
			result = cmp.Compare(a.Allocated, b.Allocated)
		case columnFree:
			result = cmp.Compare(a.Free, b.Free)
		case columnCapacity:
			result = cmp.Compare(a.Capacity, b.Capacity)
		case columnFragmentation:
			result = cmp.Compare(a.Fragmentation, b.Fragmentation)
		case columnDedup:
			result = strings.Compare(a.DedupRatio, b.DedupRatio)
		}
		if result == 0 {
			result = strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name))
		}
		if inverted {
			result *= -1
		}
		return result < 0
	})
	return entries
}

// openColumnSelectionDialog lets the user select and order the displayed columns. Runs on the UI thread.
func (poolBrowser *PoolBrowserComponent) openColumnSelectionDialog() {
	pages := poolBrowser.layout
	if poolBrowser.dialogPages != nil {
		pages = poolBrowser.dialogPages
	}
	d := dialog.NewTableColumnSelectionDialog(poolBrowser.application, "Configure Pool Columns", tableColumns, poolBrowser.tableContainer)
	dialog.ShowDialogOnPages(poolBrowser.application, pages, d, nil)
}

// SetDialogPages sets the pages its dialogs are shown on: the pages of the application, so they are modal for the
// whole screen, not only the area of this component. Without them, they are shown on its own layout.
func (poolBrowser *PoolBrowserComponent) SetDialogPages(pages *tview.Pages) {
	poolBrowser.dialogPages = pages
}

// SelectOnLoad selects the pool with the given name once the pools are loaded (if there is one), e.g. to restore
// the last session. Must be called on the UI thread.
func (poolBrowser *PoolBrowserComponent) SelectOnLoad(name string) {
	poolBrowser.initialSelection = name
}

// Refresh reloads the pools in the background.
// Must be called from the UI thread; the result is applied on the UI thread.
func (poolBrowser *PoolBrowserComponent) Refresh() {
	poolBrowser.loader.Load(func(ctx context.Context) ([]*zfs.PoolListEntry, error) {
		return listPools()
	})
}

// IsLoading returns whether the pools are being loaded (their result, or error, is not shown yet).
func (poolBrowser *PoolBrowserComponent) IsLoading() bool {
	return poolBrowser.loader.IsLoading()
}

// onPoolsLoaded displays the pools, keeping the selected one selected. Runs on the UI thread.
func (poolBrowser *PoolBrowserComponent) onPoolsLoaded(entries []*zfs.PoolListEntry) {
	if entries == nil {
		// loaded: no pools
		entries = []*zfs.PoolListEntry{}
	}
	previousName := ""
	if previous := poolBrowser.tableContainer.GetSelectedEntry(); previous != nil {
		previousName = previous.Name
	} else if poolBrowser.initialSelection != "" {
		previousName = poolBrowser.initialSelection
		poolBrowser.initialSelection = ""
	}

	poolBrowser.allEntries = entries
	poolBrowser.tableContainer.SetData(entries)
	poolBrowser.updateStatus()

	// SetData does not notify the selection callback, so always select explicitly
	// to keep the selection (and dependent components) in sync with the new entries.
	if entry := findEntryToSelect(poolBrowser.tableContainer.GetEntries(), previousName); entry != nil {
		poolBrowser.tableContainer.Select(entry)
	} else {
		poolBrowser.Events.Emit(SelectedPoolChangedEvent{nil})
	}
}

// findEntryToSelect returns the entry with the given name, or else the first entry. Returns nil if entries is empty.
func findEntryToSelect(entries []*zfs.PoolListEntry, name string) *zfs.PoolListEntry {
	if len(entries) == 0 {
		return nil
	}
	for _, entry := range entries {
		if entry.Name == name {
			return entry
		}
	}
	return entries[0]
}

// updateStatus shows the pool counts in the bottom border of the list.
func (poolBrowser *PoolBrowserComponent) updateStatus() {
	poolBrowser.tableContainer.SetFooter(formatStatus(
		poolBrowser.allEntries,
		len(poolBrowser.tableContainer.GetEntries()),
		poolBrowser.tableContainer.IsFilterActive(),
	))
}

// formatStatus returns the status text, e.g. "1 of 3 pools · 1 not online". visibleCount is the number of
// displayed pools. While a filter is active, the count is always shown as "x of y". allEntries is nil until the
// first load.
func formatStatus(allEntries []*zfs.PoolListEntry, visibleCount int, filterActive bool) string {
	if allEntries == nil {
		return txwidgets.Span(theme.Colors.ShortcutMap.Name, "Loading pools...")
	}

	noun := uiutil.Plural(len(allEntries), "pool", "pools")
	var parts []string
	if visibleCount == len(allEntries) && !filterActive {
		parts = append(parts, txwidgets.Span(theme.Colors.ShortcutMap.Name, "%d %s", len(allEntries), noun))
	} else {
		parts = append(parts, txwidgets.Span(theme.Colors.ShortcutMap.Name, "%d of %d %s", visibleCount, len(allEntries), noun))
	}

	unhealthy := 0
	for _, entry := range allEntries {
		if entry.Health != zfs.PoolStateOnline {
			unhealthy++
		}
	}
	if unhealthy > 0 {
		parts = append(parts, txwidgets.Span(theme.Colors.Pool.Degraded, "%d not online", unhealthy))
	}

	return strings.Join(parts, txwidgets.Span(theme.Colors.ShortcutMap.Name, " · "))
}

func (poolBrowser *PoolBrowserComponent) Focus() {
	poolBrowser.application.SetFocus(poolBrowser.layout)
}

func (poolBrowser *PoolBrowserComponent) HasFocus() bool {
	return poolBrowser.tableContainer.HasFocus()
}

func (poolBrowser *PoolBrowserComponent) GetLayout() *tview.Pages {
	return poolBrowser.layout
}

// ContentSize returns the size needed to show all pools and columns, see table.RowSelectionTable.ContentSize. Must
// be called on the UI thread.
func (poolBrowser *PoolBrowserComponent) ContentSize() (width int, height int) {
	return poolBrowser.tableContainer.ContentSize()
}

// GetSelectedPool returns the selected pool, or nil if none is selected.
func (poolBrowser *PoolBrowserComponent) GetSelectedPool() *zfs.PoolListEntry {
	return poolBrowser.tableContainer.GetSelectedEntry()
}

// GetCommands returns the commands only shown in the command menu, see shortcut_helper.CommandProvider.
func (poolBrowser *PoolBrowserComponent) GetCommands() []shortcut_helper.ShortcutEntry {
	return poolBrowser.tableContainer.SortCommands()
}

func (poolBrowser *PoolBrowserComponent) GetShortcutMap() []shortcut_helper.ShortcutEntry {
	return []shortcut_helper.ShortcutEntry{
		uiutil.TableComponentShortcutColumns.WithRun(poolBrowser.openColumnSelectionDialog).OnlyInMenu(),
		uiutil.TableComponentShortcutFilter.WithRun(poolBrowser.tableContainer.StartFilter),
	}
}
