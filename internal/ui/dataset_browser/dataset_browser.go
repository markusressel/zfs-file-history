package dataset_browser

import (
	"cmp"
	"context"
	"fmt"
	"sort"
	"strings"
	"zfs-file-history/internal/logging"
	"zfs-file-history/internal/ui/dialog"
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
		Title:     "Name",
		Alignment: tview.AlignLeft,
	}
	columnUsed = &table.Column{
		Id:        1,
		Title:     "Used",
		Alignment: tview.AlignRight,
	}
	// columnUsedBySnapshots, columnUsedByDataset, columnUsedByChildren and columnUsedByRefreservation
	// break down columnUsed.
	columnUsedBySnapshots = &table.Column{
		Id:        2,
		Title:     "Snapshots",
		Alignment: tview.AlignRight,
	}
	columnUsedByDataset = &table.Column{
		Id:        3,
		Title:     "Dataset",
		Alignment: tview.AlignRight,
	}
	columnUsedByChildren = &table.Column{
		Id:        4,
		Title:     "Children",
		Alignment: tview.AlignRight,
	}
	columnUsedByRefreservation = &table.Column{
		Id:        5,
		Title:     "Refreserv",
		Alignment: tview.AlignRight,
	}
	columnAvail = &table.Column{
		Id:        6,
		Title:     "Avail",
		Alignment: tview.AlignRight,
	}
	columnMountpoint = &table.Column{
		Id:        7,
		Title:     "Mountpoint",
		Alignment: tview.AlignLeft,
	}

	tableColumns = []*table.Column{
		columnName,
		columnUsed,
		columnUsedBySnapshots,
		columnUsedByDataset,
		columnUsedByChildren,
		columnUsedByRefreservation,
		columnAvail,
		columnMountpoint,
	}
)

// listDatasets is replaceable in tests.
var listDatasets = zfs.ListAllDatasets

type Event interface{}

type SelectedDatasetChangedEvent struct {
	Dataset *zfs.DatasetListEntry
}

type PathChangedEvent struct {
	NewPath string
}

type DatasetBrowserStatusEvent struct {
	Message *status_message.StatusMessage
}

type DatasetBrowserComponent struct {
	Events *util.Emitter[Event]

	application    *tview.Application
	layout         *tview.Pages
	tableContainer *table.RowSelectionTable[zfs.DatasetListEntry]
	loader         *uiutil.DataLoader[[]*zfs.DatasetListEntry]

	// currentPath is the mount path of the selected dataset ("" if it is not mounted).
	// Until the first selection, it is the path the application was started with.
	currentPath string

	// allEntries are all datasets of the last load, including the ones hidden by hideUnmounted.
	// Only accessed on the UI thread.
	allEntries []*zfs.DatasetListEntry
	// hideUnmounted hides datasets that are not mounted. Only accessed on the UI thread.
	hideUnmounted bool
	// treeView shows the datasets as a tree instead of a flat list. Only accessed on the UI thread.
	treeView bool
	// treeRows is how each displayed dataset is shown in the tree view, rebuilt whenever the entries are sorted.
	// Only accessed on the UI thread.
	treeRows map[*zfs.DatasetListEntry]treeRow
	// collapsed contains the names of the datasets whose children are hidden in the tree view.
	// Only accessed on the UI thread.
	collapsed map[string]bool
	// matchedCount is the number of datasets matching the filters (hidden unmounted datasets, filter text),
	// including the ones within collapsed datasets. Only accessed on the UI thread.
	matchedCount int
}

func NewDatasetBrowser(application *tview.Application) *DatasetBrowserComponent {
	datasetBrowser := &DatasetBrowserComponent{
		Events:        util.NewEmitter[Event](),
		application:   application,
		hideUnmounted: true,
		treeView:      true,
		collapsed:     map[string]bool{},
	}

	datasetBrowser.tableContainer = datasetBrowser.createTable(application)
	datasetBrowser.createLayout()
	datasetBrowser.setupTable()

	datasetBrowser.loader = uiutil.NewDataLoader[[]*zfs.DatasetListEntry](application).
		OnLoad(datasetBrowser.onDatasetsLoaded).
		OnError(func(err error) {
			logging.Error("Could not load datasets: %s", err.Error())
			datasetBrowser.Events.Emit(DatasetBrowserStatusEvent{
				Message: status_message.NewErrorStatusMessage(fmt.Sprintf("Could not load datasets: %s", err.Error())),
			})
		})

	return datasetBrowser
}

func (datasetBrowser *DatasetBrowserComponent) createLayout() {
	datasetBrowser.layout = tview.NewPages().
		AddPage("dataset-browser", datasetBrowser.tableContainer.GetLayout(), true, true)
}

func (datasetBrowser *DatasetBrowserComponent) createTable(application *tview.Application) *table.RowSelectionTable[zfs.DatasetListEntry] {
	return table.NewTableContainer[zfs.DatasetListEntry](
		application,
		datasetBrowser.toTableCells,
		datasetBrowser.sortTableEntries,
	)
}

// toTableCells runs on the UI thread and must only use the pre-fetched listing values.
func (datasetBrowser *DatasetBrowserComponent) toTableCells(row int, columns []*table.Column, entry *zfs.DatasetListEntry) (cells []*tview.TableCell) {
	for _, column := range columns {
		var text string
		var color tcell.Color = tcell.ColorDefault
		var alignment int = tview.AlignLeft

		switch column {
		case columnName:
			text = tview.Escape(entry.Name)
			if row, ok := datasetBrowser.treeRows[entry]; ok && datasetBrowser.treeView {
				text = txwidgets.Span(theme.Colors.Layout.Table.TreeLines, "%s", row.prefix) + tview.Escape(row.name)
				if row.collapsed {
					text += txwidgets.Span(theme.Colors.Layout.Table.TreeCollapsedIndicator, " ▸ +%d", row.hiddenCount)
				}
			}
		case columnUsed:
			text = uiutil.StableLengthHumanizedBytes(entry.Used)
			alignment = tview.AlignRight
		case columnUsedBySnapshots:
			text = uiutil.StableLengthHumanizedBytes(entry.UsedBySnapshots)
			alignment = tview.AlignRight
		case columnUsedByDataset:
			text = uiutil.StableLengthHumanizedBytes(entry.UsedByDataset)
			alignment = tview.AlignRight
		case columnUsedByChildren:
			text = uiutil.StableLengthHumanizedBytes(entry.UsedByChildren)
			alignment = tview.AlignRight
		case columnUsedByRefreservation:
			text = uiutil.StableLengthHumanizedBytes(entry.UsedByRefreservation)
			alignment = tview.AlignRight
		case columnAvail:
			text = uiutil.StableLengthHumanizedBytes(entry.Available)
			alignment = tview.AlignRight
		case columnMountpoint:
			text = tview.Escape(displayedMountpoint(entry))
		}

		cell := tview.NewTableCell(text).
			SetTextColor(color).
			SetAlign(alignment)
		cells = append(cells, cell)
	}
	return cells
}

// displayedMountpoint prefers the actual mount path, which differs from the property for legacy mounts.
func displayedMountpoint(entry *zfs.DatasetListEntry) string {
	if entry.MountPath != "" {
		return entry.MountPath
	}
	return entry.Mountpoint
}

// sortTableEntries sorts the displayed entries. In the tree view, the sort order applies among siblings.
// Called by the table on the UI thread, before the entries are displayed.
func (datasetBrowser *DatasetBrowserComponent) sortTableEntries(entries []*zfs.DatasetListEntry, column *table.Column, inverted bool) []*zfs.DatasetListEntry {
	datasetBrowser.matchedCount = len(entries)
	sorted := sortDatasetEntries(entries, column, inverted)
	if !datasetBrowser.treeView {
		datasetBrowser.treeRows = nil
		return sorted
	}
	ordered, rows := buildDatasetTree(sorted, datasetBrowser.collapsed)
	datasetBrowser.treeRows = rows
	return ordered
}

// compareDatasetPaths compares dataset names (or mount paths) one '/'-separated component at a time, case-insensitive.
// Unlike a plain string comparison, a parent always comes directly before its children:
// "rpool" < "rpool/test" < "rpool-x" (a plain comparison would put "rpool-x" first, as '-' < '/').
func compareDatasetPaths(a string, b string) int {
	aParts := strings.Split(strings.ToLower(a), "/")
	bParts := strings.Split(strings.ToLower(b), "/")
	for i := 0; i < len(aParts) && i < len(bParts); i++ {
		if result := strings.Compare(aParts[i], bParts[i]); result != 0 {
			return result
		}
	}
	return cmp.Compare(len(aParts), len(bParts))
}

func sortDatasetEntries(entries []*zfs.DatasetListEntry, column *table.Column, inverted bool) []*zfs.DatasetListEntry {
	sort.SliceStable(entries, func(i, j int) bool {
		a := entries[i]
		b := entries[j]

		result := 0
		switch column {
		case columnUsed:
			result = cmp.Compare(a.Used, b.Used)
		case columnUsedBySnapshots:
			result = cmp.Compare(a.UsedBySnapshots, b.UsedBySnapshots)
		case columnUsedByDataset:
			result = cmp.Compare(a.UsedByDataset, b.UsedByDataset)
		case columnUsedByChildren:
			result = cmp.Compare(a.UsedByChildren, b.UsedByChildren)
		case columnUsedByRefreservation:
			result = cmp.Compare(a.UsedByRefreservation, b.UsedByRefreservation)
		case columnAvail:
			result = cmp.Compare(a.Available, b.Available)
		case columnMountpoint:
			result = compareDatasetPaths(displayedMountpoint(a), displayedMountpoint(b))
		}
		if result == 0 {
			result = compareDatasetPaths(a.Name, b.Name)
		}
		if inverted {
			result *= -1
		}
		return result < 0
	})
	return entries
}

func (datasetBrowser *DatasetBrowserComponent) setupTable() {
	datasetBrowser.tableContainer.SetTitle("Datasets")
	datasetBrowser.tableContainer.SetFilterFunc(datasetMatchesFilter)
	datasetBrowser.tableContainer.SetFilterChangedCallback(datasetBrowser.updateStatus)
	datasetBrowser.updateStatus()
	// sorted by name, ascending: parents before their children
	datasetBrowser.tableContainer.SetColumnSpec(tableColumns, columnName, false)
	datasetBrowser.tableContainer.SetActiveColumns(tableColumns)

	datasetBrowser.tableContainer.SetSelectionChangedCallback(func(selectedEntry *zfs.DatasetListEntry) {
		datasetBrowser.Events.Emit(SelectedDatasetChangedEvent{selectedEntry})
		if selectedEntry != nil {
			datasetBrowser.setCurrentPath(selectedEntry.MountPath)
		}
	})

	datasetBrowser.tableContainer.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		key := event.Key()

		if key == tcell.KeyF2 {
			datasetBrowser.openColumnSelectionDialog()
			return nil
		}
		if event.Rune() == 'u' {
			datasetBrowser.ToggleHideUnmounted()
			return nil
		}
		if event.Rune() == 't' {
			datasetBrowser.ToggleTreeView()
			return nil
		}

		// on the header row, these keys are handled by the table to change the sort order
		if datasetBrowser.treeView && datasetBrowser.handleTreeKey(key) {
			return nil
		}
		if datasetBrowser.tableContainer.GetSelectedEntry() != nil {
			if key == tcell.KeyRight || key == tcell.KeyEnter {
				// no actions on data rows (yet), but don't let tview handle them either
				return nil
			}
		}
		return event
	})
}

// openColumnSelectionDialog lets the user select and order the displayed columns. Runs on the UI thread.
func (datasetBrowser *DatasetBrowserComponent) openColumnSelectionDialog() {
	d := dialog.NewColumnSelectionDialog(
		datasetBrowser.application,
		"Configure Dataset Columns",
		tableColumns,
		datasetBrowser.tableContainer.GetColumnSpec(),
		func(activeColumns []*table.Column) {
			datasetBrowser.tableContainer.SetActiveColumns(activeColumns)
		},
	)
	dialog.ShowDialogOnPages(datasetBrowser.application, datasetBrowser.layout, d, nil)
}

// setCurrentPath updates the current path and notifies listeners.
// An empty path (unmounted dataset) makes listeners clear their dataset specific content,
// instead of resolving a mountpoint property like "legacy" or "/" to an unrelated dataset.
func (datasetBrowser *DatasetBrowserComponent) setCurrentPath(path string) {
	datasetBrowser.currentPath = path
	datasetBrowser.Events.Emit(PathChangedEvent{NewPath: path})
}

func (datasetBrowser *DatasetBrowserComponent) Focus() {
	datasetBrowser.application.SetFocus(datasetBrowser.layout)
}

func (datasetBrowser *DatasetBrowserComponent) HasFocus() bool {
	return datasetBrowser.tableContainer.HasFocus()
}

func (datasetBrowser *DatasetBrowserComponent) GetLayout() *tview.Pages {
	return datasetBrowser.layout
}

// Refresh reloads the dataset list in the background.
// Must be called from the UI thread; the result is applied on the UI thread.
func (datasetBrowser *DatasetBrowserComponent) Refresh(debounce bool) {
	datasetBrowser.loader.Load(func(ctx context.Context) ([]*zfs.DatasetListEntry, error) {
		return listDatasets()
	})
}

// onDatasetsLoaded runs on the UI thread.
func (datasetBrowser *DatasetBrowserComponent) onDatasetsLoaded(entries []*zfs.DatasetListEntry) {
	datasetBrowser.allEntries = entries
	datasetBrowser.updateEntries()
}

// ToggleHideUnmounted shows or hides datasets that are not mounted.
// Must be called on the UI thread.
func (datasetBrowser *DatasetBrowserComponent) ToggleHideUnmounted() {
	headerSelected := datasetBrowser.tableContainer.GetSelectedEntry() == nil && !datasetBrowser.tableContainer.IsEmpty()

	datasetBrowser.hideUnmounted = !datasetBrowser.hideUnmounted
	datasetBrowser.updateEntries()

	if headerSelected {
		datasetBrowser.tableContainer.SelectHeader()
	}
}

// handleTreeKey collapses and expands datasets in the tree view, like tree widgets usually do:
// ← collapses an expanded dataset, or else selects its parent. → expands a collapsed dataset, or else selects its
// first child. Returns false if the key was not handled (e.g. on the header row). Runs on the UI thread.
func (datasetBrowser *DatasetBrowserComponent) handleTreeKey(key tcell.Key) bool {
	if key != tcell.KeyLeft && key != tcell.KeyRight {
		return false
	}
	entry := datasetBrowser.tableContainer.GetSelectedEntry()
	row, ok := datasetBrowser.treeRows[entry]
	if entry == nil || !ok {
		return false
	}

	switch key {
	case tcell.KeyLeft:
		if len(row.children) > 0 && !row.collapsed {
			datasetBrowser.setCollapsed(entry.Name, true)
		} else if row.parent != nil {
			datasetBrowser.tableContainer.Select(row.parent)
		}
	case tcell.KeyRight:
		if row.collapsed {
			datasetBrowser.setCollapsed(entry.Name, false)
		} else if len(row.children) > 0 {
			datasetBrowser.tableContainer.Select(row.children[0])
		}
	}
	return true
}

// setCollapsed collapses or expands the dataset with the given name. The selection is kept.
func (datasetBrowser *DatasetBrowserComponent) setCollapsed(name string, collapsed bool) {
	if collapsed {
		datasetBrowser.collapsed[name] = true
	} else {
		delete(datasetBrowser.collapsed, name)
	}
	datasetBrowser.updateEntries()
}

// ToggleTreeView switches between the flat list and the tree view. The selection is kept.
// Must be called on the UI thread.
func (datasetBrowser *DatasetBrowserComponent) ToggleTreeView() {
	headerSelected := datasetBrowser.tableContainer.GetSelectedEntry() == nil && !datasetBrowser.tableContainer.IsEmpty()

	datasetBrowser.treeView = !datasetBrowser.treeView
	datasetBrowser.updateEntries()

	if headerSelected {
		datasetBrowser.tableContainer.SelectHeader()
	}
}

// IsTreeView returns whether the datasets are shown as a tree.
func (datasetBrowser *DatasetBrowserComponent) IsTreeView() bool {
	return datasetBrowser.treeView
}

// IsHidingUnmounted returns whether datasets that are not mounted are hidden.
func (datasetBrowser *DatasetBrowserComponent) IsHidingUnmounted() bool {
	return datasetBrowser.hideUnmounted
}

// filterEntries returns the entries to display. It always returns a new slice,
// so sorting the displayed entries never reorders allEntries.
func filterEntries(entries []*zfs.DatasetListEntry, hideUnmounted bool) []*zfs.DatasetListEntry {
	result := make([]*zfs.DatasetListEntry, 0, len(entries))
	for _, entry := range entries {
		if hideUnmounted && entry.MountPath == "" {
			continue
		}
		result = append(result, entry)
	}
	return result
}

// updateStatus shows the dataset counts and the active modifiers in the bottom border of the list.
func (datasetBrowser *DatasetBrowserComponent) updateStatus() {
	datasetBrowser.tableContainer.SetFooter(formatStatus(
		datasetBrowser.allEntries,
		// not the displayed entries: collapsing does not change the count
		datasetBrowser.matchedCount,
		datasetBrowser.hideUnmounted,
		datasetBrowser.tableContainer.IsFilterActive(),
	))
}

// datasetMatchesFilter matches the dataset name or its (displayed) mountpoint against the filter,
// as a glob, see table.MatchesGlob.
func datasetMatchesFilter(entry *zfs.DatasetListEntry, filterText string) bool {
	return table.MatchesGlob(entry.Name, filterText) || table.MatchesGlob(displayedMountpoint(entry), filterText)
}

// formatStatus returns the status text, e.g. "16 of 589 datasets · 573 unmounted hidden".
// visibleCount is the number of displayed datasets, after hiding unmounted ones and applying the filter.
// While a filter is active, the count is always shown as "x of y".
// allEntries is nil until the first load.
func formatStatus(allEntries []*zfs.DatasetListEntry, visibleCount int, hideUnmounted bool, filterActive bool) string {
	if allEntries == nil {
		return txwidgets.Span(theme.Colors.ShortcutMap.Name, "Loading datasets...")
	}

	noun := uiutil.Plural(len(allEntries), "dataset", "datasets")
	var parts []string
	if visibleCount == len(allEntries) && !filterActive {
		parts = append(parts, txwidgets.Span(theme.Colors.ShortcutMap.Name, "%d %s", len(allEntries), noun))
	} else {
		parts = append(parts, txwidgets.Span(theme.Colors.ShortcutMap.Name, "%d of %d %s", visibleCount, len(allEntries), noun))
	}

	if hideUnmounted {
		unmountedCount := 0
		for _, entry := range allEntries {
			if entry.MountPath == "" {
				unmountedCount++
			}
		}
		parts = append(parts, txwidgets.Span(theme.Colors.ShortcutMap.KeyCombo, "%d unmounted hidden", unmountedCount))
	}

	return strings.Join(parts, txwidgets.Span(theme.Colors.ShortcutMap.Name, " · "))
}

// updateEntries displays allEntries, filtered by the current settings, and keeps the selection in sync.
// Runs on the UI thread.
func (datasetBrowser *DatasetBrowserComponent) updateEntries() {
	previousName := ""
	if previous := datasetBrowser.tableContainer.GetSelectedEntry(); previous != nil {
		previousName = previous.Name
	}

	datasetBrowser.tableContainer.SetData(filterEntries(datasetBrowser.allEntries, datasetBrowser.hideUnmounted))
	datasetBrowser.updateStatus()

	// SetData does not notify the selection callback, so always select explicitly
	// to keep the selection (and dependent components) in sync with the new entries.
	entryToSelect := findEntryToSelect(datasetBrowser.tableContainer.GetEntries(), previousName, datasetBrowser.currentPath)
	if entryToSelect != nil {
		datasetBrowser.tableContainer.Select(entryToSelect)
	} else if datasetBrowser.currentPath != "" {
		datasetBrowser.setCurrentPath("")
	}
}

// findEntryToSelect returns the entry with the given name, or else the mounted dataset containing path,
// or else the first entry. Returns nil if entries is empty.
func findEntryToSelect(entries []*zfs.DatasetListEntry, previousName string, path string) *zfs.DatasetListEntry {
	if len(entries) == 0 {
		return nil
	}

	if previousName != "" {
		for _, entry := range entries {
			if entry.Name == previousName {
				return entry
			}
		}
	}

	var bestMatch *zfs.DatasetListEntry
	if path != "" {
		for _, entry := range entries {
			mountPath := entry.MountPath
			if mountPath == "" {
				continue
			}
			isParent := path == mountPath || mountPath == "/" || strings.HasPrefix(path, mountPath+"/")
			if isParent && (bestMatch == nil || len(mountPath) > len(bestMatch.MountPath)) {
				bestMatch = entry
			}
		}
	}
	if bestMatch != nil {
		return bestMatch
	}

	return entries[0]
}

func (datasetBrowser *DatasetBrowserComponent) GetPath() string {
	return datasetBrowser.currentPath
}

// SetPath sets the initial path, used to preselect the dataset containing it once the datasets are loaded.
func (datasetBrowser *DatasetBrowserComponent) SetPath(path string, checkExists bool) {
	datasetBrowser.currentPath = path
}

func (datasetBrowser *DatasetBrowserComponent) GetShortcutMap() []shortcut_helper.ShortcutEntry {
	toggleUnmountedName := "Hide unmounted"
	if datasetBrowser.hideUnmounted {
		toggleUnmountedName = "Show unmounted"
	}
	toggleTreeViewName := "Tree view"
	if datasetBrowser.treeView {
		toggleTreeViewName = "Flat list"
	}
	shortcuts := []shortcut_helper.ShortcutEntry{
		uiutil.TableComponentShortcutColumns,
		uiutil.TableComponentShortcutFilter,
		{KeyCombo: []string{"u"}, Name: toggleUnmountedName},
		{KeyCombo: []string{"t"}, Name: toggleTreeViewName},
	}
	if datasetBrowser.treeView && datasetBrowser.tableContainer.GetSelectedEntry() != nil {
		shortcuts = append(shortcuts, shortcut_helper.ShortcutEntry{KeyCombo: []string{"←", "→"}, Name: "Collapse/expand"})
	}
	return shortcuts
}
