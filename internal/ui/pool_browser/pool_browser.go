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

// listPools, runPoolAction, listPoolFeatures and enablePoolFeatures are replaceable in tests.
var (
	listPools          = zfs.ListPools
	runPoolAction      = zfs.RunPoolAction
	listPoolFeatures   = zfs.ListPoolFeatures
	enablePoolFeatures = zfs.EnablePoolFeatures
	previewDestroyPool = zfs.PreviewDestroyPool
	destroyPool        = zfs.DestroyPool
)

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
	// status is the status of the selected pool (nil if not known), which decides the actions offered, see
	// SetStatus. Only accessed on the UI thread.
	status *zfs.PoolStatus
	// onShowHistory shows the history of a pool (h), nil: not offered, see SetOnShowHistory. Only accessed on the UI
	// thread.
	onShowHistory func(pool string)
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
		if selection := poolBrowser.tableContainer.GetSelectedEntry(); selection != nil && event.Key() == tcell.KeyEnter {
			poolBrowser.openActionDialog(selection)
			return nil
		}
		if selection := poolBrowser.tableContainer.GetSelectedEntry(); selection != nil && event.Key() == tcell.KeyRune &&
			event.Rune() == 'h' && poolBrowser.onShowHistory != nil {
			poolBrowser.onShowHistory(selection.Name)
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
	d := dialog.NewTableColumnSelectionDialog(poolBrowser.application, "Configure Pool Columns", tableColumns, poolBrowser.tableContainer)
	poolBrowser.showDialog(d)
}

// showDialog shows the dialog on the dialog pages (see SetDialogPages). Must be called on the UI thread.
func (poolBrowser *PoolBrowserComponent) showDialog(d dialog.Dialog) {
	pages := poolBrowser.layout
	if poolBrowser.dialogPages != nil {
		pages = poolBrowser.dialogPages
	}
	dialog.ShowDialogOnPages(poolBrowser.application, pages, d, nil)
}

// SetStatus sets the status of the selected pool (nil if not known), which decides the actions offered, e.g.
// pausing a scrub only while one runs. Must be called on the UI thread.
func (poolBrowser *PoolBrowserComponent) SetStatus(status *zfs.PoolStatus) {
	poolBrowser.status = status
}

// statusOf returns the status of the pool, nil if it is not known (e.g. the status of the previously selected pool
// is still shown).
func (poolBrowser *PoolBrowserComponent) statusOf(pool *zfs.PoolListEntry) *zfs.PoolStatus {
	if poolBrowser.status == nil || poolBrowser.status.Name != pool.Name {
		return nil
	}
	return poolBrowser.status
}

// openActionDialog shows the actions for the pool, e.g. starting a scrub. Must be called on the UI thread.
func (poolBrowser *PoolBrowserComponent) openActionDialog(pool *zfs.PoolListEntry) {
	poolBrowser.showDialog(poolBrowser.newActionDialog(pool))
}

// newActionDialog creates the dialog with the actions for the pool, see openActionDialog.
func (poolBrowser *PoolBrowserComponent) newActionDialog(pool *zfs.PoolListEntry) *dialog.SelectionDialog {
	name := pool.Name
	// features are the ones listed by the last choice of the features, read on the UI thread once listed
	var features *zfs.PoolFeatures
	asyncWork := func(d *dialog.SelectionDialog, id dialog.DialogActionId) error {
		if action, ok := dialog.PoolActionOf(id); ok {
			return runPoolAction(action, name)
		}
		var err error
		switch id {
		case dialog.PoolDialogFeaturesActionId:
			features, err = listPoolFeatures(name)
		}
		return err
	}
	onComplete := func(d *dialog.SelectionDialog, option *dialog.DialogOption, err error) {
		d.Close()
		switch option.Id {
		case dialog.PoolDialogUpgradeActionId:
			poolBrowser.confirmUpgrade(name)
			return
		case dialog.PoolDialogFeaturesActionId:
			if err != nil {
				logging.Error("Could not list the features of %s: %s", name, err.Error())
				poolBrowser.showDialog(dialog.NewErrorDialog(poolBrowser.application, "Listing Features Failed", err))
				return
			}
			poolBrowser.chooseFeatures(features)
			return
		case dialog.PoolDialogDestroyActionId:
			// nothing is destroyed yet, the confirmations do that
			poolBrowser.previewAndConfirmDestroy(name)
			return
		}
		action, ok := dialog.PoolActionOf(option.Id)
		if !ok {
			return
		}
		if err != nil {
			logging.Error("Could not %s on %s: %s", action.Name, name, err.Error())
			poolBrowser.showDialog(dialog.NewErrorDialogWithRetry(poolBrowser.application, "Operation Failed", err, d.RetryFunc(option)))
			return
		}
		poolBrowser.Events.Emit(PoolBrowserStatusEvent{
			Message: status_message.NewSuccessStatusMessage(fmt.Sprintf("%s of %s", action.Done, name)),
		})
		// the event watcher may not run (see "setup"), so the pools are reloaded here
		zfs.PoolsChanged.Emit(struct{}{})
	}
	return dialog.NewPoolActionDialog(poolBrowser.application, name, poolBrowser.statusOf(pool), asyncWork, onComplete)
}

// confirmUpgrade upgrades the pool once its name was typed: it cannot be undone. Must be called on the UI thread.
func (poolBrowser *PoolBrowserComponent) confirmUpgrade(pool string) {
	poolBrowser.showDialog(dialog.NewPoolUpgradeDialog(poolBrowser.application, pool, func() {
		action := zfs.PoolActionUpgrade
		poolBrowser.runConfirmedInBackground(pool, "Upgrade Failed", action.Name, action.Done,
			func() error { return runPoolAction(action, pool) },
			func() { poolBrowser.confirmUpgrade(pool) })
	}))
}

// chooseFeatures shows the features of the pool, to enable the chosen ones once confirmed. Must be called on the UI
// thread.
func (poolBrowser *PoolBrowserComponent) chooseFeatures(features *zfs.PoolFeatures) {
	poolBrowser.showDialog(dialog.NewPoolFeaturesDialog(poolBrowser.application, features, func(chosen []string) {
		poolBrowser.confirmFeatures(features.Pool, chosen)
	}))
}

// confirmFeatures enables the features of the pool once its name was typed: it cannot be undone. Must be called on
// the UI thread.
func (poolBrowser *PoolBrowserComponent) confirmFeatures(pool string, features []string) {
	poolBrowser.showDialog(dialog.NewEnablePoolFeaturesDialog(poolBrowser.application, pool, features, func() {
		names := strings.Join(features, ", ")
		poolBrowser.runConfirmedInBackground(pool, "Enabling Features Failed", "enable the features "+names,
			"Enabled the features "+names,
			func() error { return enablePoolFeatures(pool, features) },
			func() { poolBrowser.confirmFeatures(pool, features) })
	}))
}

// formatDestroyPoolDescription describes what destroying the pool destroys, with style tags.
func formatDestroyPoolDescription(preview *zfs.PoolDestroyPreview) string {
	var description strings.Builder
	fmt.Fprintf(&description, "Destroy the pool %s with all its datasets, snapshots and files (%s). This %s. Its "+
		"devices can be used for other pools afterwards.", dialog.Highlight(preview.Pool),
		dialog.Highlight(uiutil.HumanizedBytes(preview.Allocated)), txwidgets.Span(theme.Colors.Dialog.Danger, "%s", "cannot be undone"))
	description.WriteString("\n\nThe datasets are unmounted first. If one is in use, nothing is destroyed.")
	fmt.Fprintf(&description, "\n\nDatasets that will be destroyed (%d):", len(preview.Datasets))
	for i, name := range preview.Datasets {
		if i == maxListedDatasets {
			fmt.Fprintf(&description, "\n  … and %d more", len(preview.Datasets)-maxListedDatasets)
			break
		}
		fmt.Fprintf(&description, "\n  %s", dialog.Highlight(name))
	}
	return description.String()
}

// maxListedDatasets is the maximum number of datasets listed in the confirmation of destroying a pool.
const maxListedDatasets = 10

// previewAndConfirmDestroy lists what destroying the pool destroys in the background and then asks for
// confirmation. Meanwhile, a dialog says that it is checked, and that nothing is destroyed yet; it can be cancelled.
// It runs there, not in the action dialog: a spinner next to "Destroy pool" looked like it was destroyed without a
// confirmation. Must be called on the UI thread.
func (poolBrowser *PoolBrowserComponent) previewAndConfirmDestroy(pool string) {
	checking := dialog.NewPreviewDialog(poolBrowser.application, " 💥 Destroy Pool ", fmt.Sprintf(
		"Checking what destroying the pool %s would destroy: listing its datasets and where they are mounted. "+
			"Nothing is destroyed yet.", pool))
	poolBrowser.showDialog(checking)
	go func() {
		preview, err := previewDestroyPool(pool)
		poolBrowser.application.QueueUpdateDraw(func() {
			if !checking.Done() {
				return // cancelled
			}
			if err != nil {
				logging.Error("Cannot destroy %s: %s", pool, err.Error())
				retry := func() { poolBrowser.previewAndConfirmDestroy(pool) }
				poolBrowser.showDialog(dialog.NewErrorDialogWithRetry(poolBrowser.application, "Cannot Destroy", err, retry))
				return
			}
			poolBrowser.showDestroyConfirmation(preview)
		})
	}()
}

// showDestroyConfirmation asks for confirmation to destroy the pool as previewed: a checkbox that the data is lost
// first, then its name typed. Must be called on the UI thread.
func (poolBrowser *PoolBrowserComponent) showDestroyConfirmation(preview *zfs.PoolDestroyPreview) {
	description := formatDestroyPoolDescription(preview)
	count := len(preview.Datasets)
	lost := fmt.Sprintf("Destroy its %d %s with all snapshots and files", count, uiutil.Plural(count, "dataset", "datasets"))
	onComplete := func(d *dialog.SelectionDialog, option *dialog.DialogOption, err error) {
		d.Close()
		if option.Id != dialog.DestroyPoolDialogContinueActionId {
			return
		}
		if !d.Checked()[0] {
			// shows this dialog again, so the box can be checked
			retry := func() { poolBrowser.showDestroyConfirmation(preview) }
			poolBrowser.showDialog(dialog.NewErrorDialogWithRetry(poolBrowser.application, "Nothing Destroyed",
				fmt.Errorf("check '%s' to destroy the pool %s", lost, preview.Pool), retry))
			return
		}
		poolBrowser.showDialog(dialog.NewConfirmByNameDialog(poolBrowser.application, "DestroyPoolByNameDialog",
			" 💥 Destroy Pool ", description, preview.Pool, func() { poolBrowser.destroyInBackground(preview) }))
	}
	poolBrowser.showDialog(dialog.NewDestroyPoolDialog(poolBrowser.application, description, lost, nil, onComplete))
}

// destroyInBackground destroys the pool as confirmed in the background and shows the result. Must be called on the
// UI thread.
func (poolBrowser *PoolBrowserComponent) destroyInBackground(preview *zfs.PoolDestroyPreview) {
	pool := preview.Pool
	go func() {
		err := destroyPool(preview)
		poolBrowser.application.QueueUpdateDraw(func() {
			if err != nil {
				logging.Error("Failed to destroy %s: %s", pool, err.Error())
				// a retry asks for confirmation again, with a new preview
				retry := func() { poolBrowser.previewAndConfirmDestroy(pool) }
				poolBrowser.showDialog(dialog.NewErrorDialogWithRetry(poolBrowser.application, "Destroy Failed", err, retry))
				return
			}
			count := len(preview.Datasets)
			poolBrowser.showDialog(dialog.NewSuccessDialog(poolBrowser.application, "Pool Destroyed",
				fmt.Sprintf("Destroyed the pool '%s' with its %d %s.", pool, count, uiutil.Plural(count, "dataset", "datasets"))))
			// the event watcher may not run (see "setup"), so the pools are reloaded here
			zfs.PoolsChanged.Emit(struct{}{})
			zfs.RefreshZfsData()
		})
	}()
}

// runConfirmedInBackground runs what was confirmed for the pool in the background and shows the result: done (e.g.
// "Enabled all features") in the status bar, or the error with retry, which asks for confirmation again. Must be
// called on the UI thread.
func (poolBrowser *PoolBrowserComponent) runConfirmedInBackground(pool string, failedTitle string, name string, done string, run func() error, retry func()) {
	go func() {
		err := run()
		poolBrowser.application.QueueUpdateDraw(func() {
			if err != nil {
				logging.Error("Could not %s on %s: %s", name, pool, err.Error())
				poolBrowser.showDialog(dialog.NewErrorDialogWithRetry(poolBrowser.application, failedTitle, err, retry))
				return
			}
			poolBrowser.Events.Emit(PoolBrowserStatusEvent{
				Message: status_message.NewSuccessStatusMessage(fmt.Sprintf("%s of %s", done, pool)),
			})
			// the event watcher may not run (see "setup"), so the pools are reloaded here
			zfs.PoolsChanged.Emit(struct{}{})
		})
	}()
}

// SetDialogPages sets the pages its dialogs are shown on: the pages of the application, so they are modal for the
// whole screen, not only the area of this component. Without them, they are shown on its own layout.
func (poolBrowser *PoolBrowserComponent) SetDialogPages(pages *tview.Pages) {
	poolBrowser.dialogPages = pages
}

// SetOnShowHistory sets the function that shows the history of a pool (h, see zfs.PoolHistory), nil while it cannot
// be read (it needs root). Must be called on the UI thread.
func (poolBrowser *PoolBrowserComponent) SetOnShowHistory(onShowHistory func(pool string)) {
	poolBrowser.onShowHistory = onShowHistory
}

// SelectOnLoad selects the pool with the given name once the pools are loaded (if there is one), e.g. to restore
// the last session. Must be called on the UI thread.
func (poolBrowser *PoolBrowserComponent) SelectOnLoad(name string) {
	poolBrowser.initialSelection = name
}

// SelectPool selects the pool with the given name, or once the pools are loaded if they are not yet. Nothing
// happens if it is not listed (e.g. hidden by the filter). Must be called on the UI thread.
func (poolBrowser *PoolBrowserComponent) SelectPool(name string) {
	if poolBrowser.allEntries == nil {
		poolBrowser.SelectOnLoad(name)
		return
	}
	for _, entry := range poolBrowser.tableContainer.GetEntries() {
		if entry.Name == name {
			poolBrowser.tableContainer.Select(entry)
			return
		}
	}
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

// GetCommands returns the commands only shown in the command menu, see shortcut_helper.CommandProvider: the actions
// of the selected pool, and sorting.
func (poolBrowser *PoolBrowserComponent) GetCommands() []shortcut_helper.ShortcutEntry {
	var commands []shortcut_helper.ShortcutEntry
	if selection := poolBrowser.GetSelectedPool(); selection != nil {
		show := func(d dialog.Dialog) { poolBrowser.showDialog(d) }
		commands = poolBrowser.newActionDialog(selection).OptionCommands(show)
	}
	return append(commands, poolBrowser.tableContainer.SortCommands()...)
}

func (poolBrowser *PoolBrowserComponent) GetShortcutMap() []shortcut_helper.ShortcutEntry {
	shortcutMap := []shortcut_helper.ShortcutEntry{
		uiutil.TableComponentShortcutColumns.WithRun(poolBrowser.openColumnSelectionDialog).OnlyInMenu(),
		uiutil.TableComponentShortcutFilter.WithRun(poolBrowser.tableContainer.StartFilter),
	}
	if selection := poolBrowser.GetSelectedPool(); selection != nil {
		shortcutMap = append(shortcutMap, uiutil.TableComponentShortcutActions.WithRun(func() { poolBrowser.openActionDialog(selection) }).
			WithDescription("Scrub, TRIM or clear the errors of the pool"))
		if poolBrowser.onShowHistory != nil {
			shortcutMap = append(shortcutMap, shortcut_helper.ShortcutEntry{KeyCombo: []string{"h"}, Name: "History",
				Description: "What was done to the pool and its datasets, when and by whom (zpool history)",
				Run:         func() { poolBrowser.onShowHistory(selection.Name) }})
		}
	}
	return shortcutMap
}
