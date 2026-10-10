package dataset_browser

import (
	"cmp"
	"context"
	"fmt"
	"sort"
	"strings"
	"zfs-file-history/internal/logging"
	"zfs-file-history/internal/state"
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
		Key:       "name",
		Title:     "Name",
		Alignment: tview.AlignLeft,
	}
	columnUsed = &table.Column{
		Id:        1,
		Key:       "used",
		Title:     "Used",
		Alignment: tview.AlignRight,
	}
	// columnUsedBySnapshots, columnUsedByDataset, columnUsedByChildren and columnUsedByRefreservation
	// break down columnUsed.
	columnUsedBySnapshots = &table.Column{
		Id:        2,
		Key:       "usedBySnapshots",
		Title:     "Snapshots",
		Alignment: tview.AlignRight,
	}
	columnUsedByDataset = &table.Column{
		Id:        3,
		Key:       "usedByDataset",
		Title:     "Dataset",
		Alignment: tview.AlignRight,
	}
	columnUsedByChildren = &table.Column{
		Id:        4,
		Key:       "usedByChildren",
		Title:     "Children",
		Alignment: tview.AlignRight,
	}
	columnUsedByRefreservation = &table.Column{
		Id:        5,
		Key:       "usedByRefreservation",
		Title:     "Refreserv",
		Alignment: tview.AlignRight,
	}
	// columnPermissions shows the ZFS permissions of the current user, see formatPermissionMask
	columnPermissions = &table.Column{
		Id:        8,
		Key:       "permissions",
		Title:     "Perms",
		Alignment: tview.AlignLeft,
	}
	columnAvail = &table.Column{
		Id:        6,
		Key:       "available",
		Title:     "Avail",
		Alignment: tview.AlignRight,
	}
	columnMountpoint = &table.Column{
		Id:        7,
		Key:       "mountpoint",
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
		columnPermissions,
		columnMountpoint,
	}
)

// keys of the settings remembered in state.Current
const (
	stateKeyTable       = "datasetBrowser"
	toggleHideUnmounted = "datasetBrowser.hideUnmounted"
	toggleTreeView      = "datasetBrowser.treeView"
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
	// dialogPages are the pages dialogs are shown on, see SetDialogPages
	dialogPages *tview.Pages
	// sizeScales color the size columns, see updateSizeScales
	sizeScales map[*table.Column]uiutil.MagnitudeScale
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
	// permissions are the ZFS permissions of the current user, loaded for the displayed datasets
	permissions permissionLoader
}

func NewDatasetBrowser(application *tview.Application) *DatasetBrowserComponent {
	datasetBrowser := &DatasetBrowserComponent{
		Events:        util.NewEmitter[Event](),
		application:   application,
		hideUnmounted: state.Current.Toggle(toggleHideUnmounted, true),
		treeView:      state.Current.Toggle(toggleTreeView, true),
		collapsed:     map[string]bool{},
		permissions:   permissionLoader{loaded: map[string]permissionState{}, stale: map[string]bool{}},
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
// coloredSizeColumns are the size columns that are colored by how big their sizes are (see updateSizeScales), with
// the size of an entry. Not the available space: it is mostly the free space of the pool, the same for all datasets.
var coloredSizeColumns = map[*table.Column]func(entry *zfs.DatasetListEntry) uint64{
	columnUsed:                 func(entry *zfs.DatasetListEntry) uint64 { return entry.Used },
	columnUsedBySnapshots:      func(entry *zfs.DatasetListEntry) uint64 { return entry.UsedBySnapshots },
	columnUsedByDataset:        func(entry *zfs.DatasetListEntry) uint64 { return entry.UsedByDataset },
	columnUsedByChildren:       func(entry *zfs.DatasetListEntry) uint64 { return entry.UsedByChildren },
	columnUsedByRefreservation: func(entry *zfs.DatasetListEntry) uint64 { return entry.UsedByRefreservation },
}

// updateSizeScales computes the scales of the size columns from all datasets (also the hidden ones, so hiding or
// collapsing does not change the colors). Runs on the UI thread, before the cells are rendered.
func (datasetBrowser *DatasetBrowserComponent) updateSizeScales() {
	datasetBrowser.sizeScales = map[*table.Column]uiutil.MagnitudeScale{}
	for column, size := range coloredSizeColumns {
		sizes := make([]uint64, len(datasetBrowser.allEntries))
		for i, entry := range datasetBrowser.allEntries {
			sizes[i] = size(entry)
		}
		datasetBrowser.sizeScales[column] = uiutil.NewMagnitudeScale(sizes)
	}
}

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
		case columnUsed, columnUsedBySnapshots, columnUsedByDataset, columnUsedByChildren, columnUsedByRefreservation:
			size := coloredSizeColumns[column](entry)
			text = uiutil.StableLengthHumanizedBytes(size)
			color = datasetBrowser.sizeScales[column].SizeColor(size, color)
			alignment = tview.AlignRight
		case columnAvail:
			text = uiutil.StableLengthHumanizedBytes(entry.Available)
			alignment = tview.AlignRight
		case columnMountpoint:
			text = tview.Escape(displayedMountpoint(entry))
		case columnPermissions:
			state, ok := datasetBrowser.permissions.loaded[entry.Name]
			text = formatPermissionMask(state, ok)
			color = theme.Colors.Permissions.Granted
			if !ok || state.err != nil {
				color = theme.Colors.Permissions.Unknown
			}
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
	datasetBrowser.tableContainer.SetFilterChangedCallback(func() {
		datasetBrowser.updateStatus()
		datasetBrowser.loadPermissions()
	})
	datasetBrowser.updateStatus()
	// sorted by name, ascending: parents before their children
	datasetBrowser.tableContainer.SetColumnSpec(tableColumns, columnName, false)
	datasetBrowser.tableContainer.SetActiveColumns(tableColumns)
	datasetBrowser.tableContainer.BindColumnLayout(state.Current, stateKeyTable, tableColumns)
	// the permissions are only loaded while their column is displayed
	datasetBrowser.tableContainer.SetColumnLayoutChangedCallback(func(table.ColumnLayout) { datasetBrowser.loadPermissions() })

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
		if event.Rune() == 'p' && datasetBrowser.tableContainer.GetSelectedEntry() != nil {
			datasetBrowser.openPermissionsDialog(datasetBrowser.tableContainer.GetSelectedEntry())
			return nil
		}
		if event.Rune() == 'e' && datasetBrowser.tableContainer.GetSelectedEntry() != nil {
			datasetBrowser.openPropertiesDialog(datasetBrowser.tableContainer.GetSelectedEntry())
			return nil
		}

		if datasetBrowser.treeView && datasetBrowser.handleTreeKey(event.Rune()) {
			return nil
		}
		if datasetBrowser.tableContainer.GetSelectedEntry() != nil && key == tcell.KeyEnter {
			// no action on data rows (yet), but don't let tview handle it either
			return nil
		}
		// ← and → scroll horizontally (tview), or change the sort column on the header row (table)
		return event
	})
}

// openColumnSelectionDialog lets the user select and order the displayed columns. Runs on the UI thread.
func (datasetBrowser *DatasetBrowserComponent) openColumnSelectionDialog() {
	d := dialog.NewTableColumnSelectionDialog(datasetBrowser.application, "Configure Dataset Columns", tableColumns, datasetBrowser.tableContainer)
	datasetBrowser.showDialog(d)
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
	// e.g. after "zfs allow" changed them
	datasetBrowser.invalidatePermissions()
	datasetBrowser.updateEntries()
}

// ToggleHideUnmounted shows or hides datasets that are not mounted.
// Must be called on the UI thread.
func (datasetBrowser *DatasetBrowserComponent) ToggleHideUnmounted() {
	headerSelected := datasetBrowser.tableContainer.GetSelectedEntry() == nil && !datasetBrowser.tableContainer.IsEmpty()

	datasetBrowser.hideUnmounted = !datasetBrowser.hideUnmounted
	state.Current.SetToggle(toggleHideUnmounted, datasetBrowser.hideUnmounted)
	datasetBrowser.updateEntries()

	if headerSelected {
		datasetBrowser.tableContainer.SelectHeader()
	}
}

// handleTreeKey collapses and expands datasets in the tree view, like htop's tree view:
// - collapses the selected dataset, or else (collapsed or no children) selects its parent.
// + expands the selected dataset, or else (already expanded) selects its first child.
// * collapses all datasets if any is expanded, or else expands all.
// ← and → are left to scroll horizontally. Returns false if the key was not handled. Runs on the UI thread.
func (datasetBrowser *DatasetBrowserComponent) handleTreeKey(key rune) bool {
	if key == '*' {
		datasetBrowser.toggleCollapseAll()
		return true
	}
	if key != '-' && key != '+' {
		return false
	}
	entry := datasetBrowser.tableContainer.GetSelectedEntry()
	row, ok := datasetBrowser.treeRows[entry]
	if entry == nil || !ok {
		// e.g. on the header row: consumed anyway, so typing them does nothing unexpected
		return true
	}

	switch key {
	case '-':
		if len(row.children) > 0 && !row.collapsed {
			datasetBrowser.setCollapsed(entry.Name, true)
		} else if row.parent != nil {
			datasetBrowser.tableContainer.Select(row.parent)
		}
	case '+':
		if row.collapsed {
			datasetBrowser.setCollapsed(entry.Name, false)
		} else if len(row.children) > 0 {
			datasetBrowser.tableContainer.Select(row.children[0])
		}
	}
	return true
}

// toggleCollapseAll collapses all datasets with children if any of the displayed ones is expanded, or else expands
// all. The selection moves to the nearest displayed ancestor if it gets hidden. Runs on the UI thread.
func (datasetBrowser *DatasetBrowserComponent) toggleCollapseAll() {
	anyExpanded := false
	for _, row := range datasetBrowser.treeRows {
		if len(row.children) > 0 && !row.collapsed {
			anyExpanded = true
			break
		}
	}

	if !anyExpanded {
		clear(datasetBrowser.collapsed)
		datasetBrowser.updateEntries()
		return
	}

	selected := datasetBrowser.tableContainer.GetSelectedEntry()
	var newSelection *zfs.DatasetListEntry
	if selected != nil {
		// the top-most ancestor stays visible
		newSelection = selected
		for row, ok := datasetBrowser.treeRows[newSelection]; ok && row.parent != nil; row, ok = datasetBrowser.treeRows[newSelection] {
			newSelection = row.parent
		}
	}
	for _, entry := range datasetBrowser.allEntries {
		datasetBrowser.collapsed[entry.Name] = true
	}
	datasetBrowser.updateEntries()
	if newSelection != nil && newSelection != selected {
		datasetBrowser.tableContainer.Select(newSelection)
	}
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
	state.Current.SetToggle(toggleTreeView, datasetBrowser.treeView)
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

	datasetBrowser.updateSizeScales()
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
	datasetBrowser.loadPermissions()
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

// SetDialogPages sets the pages its dialogs are shown on: the pages of the application, so they are modal for the
// whole screen, not only the area of this component. Without them, they are shown on its own layout.
func (datasetBrowser *DatasetBrowserComponent) SetDialogPages(pages *tview.Pages) {
	datasetBrowser.dialogPages = pages
}

func (datasetBrowser *DatasetBrowserComponent) GetPath() string {
	return datasetBrowser.currentPath
}

// SetPath sets the initial path, used to preselect the dataset containing it once the datasets are loaded.
func (datasetBrowser *DatasetBrowserComponent) SetPath(path string, checkExists bool) {
	datasetBrowser.currentPath = path
}

// GetCommands returns the commands only shown in the command menu, see shortcut_helper.CommandProvider.
func (datasetBrowser *DatasetBrowserComponent) GetCommands() []shortcut_helper.ShortcutEntry {
	return datasetBrowser.tableContainer.SortCommands()
}

func (datasetBrowser *DatasetBrowserComponent) GetShortcutMap() []shortcut_helper.ShortcutEntry {
	toggleUnmountedName, toggleUnmountedDescription := "Hide unmounted", "Hide the datasets that are not mounted"
	if datasetBrowser.hideUnmounted {
		toggleUnmountedName, toggleUnmountedDescription = "Show unmounted", "Show the datasets that are not mounted as well"
	}
	toggleTreeViewName, toggleTreeViewDescription := "Tree view", "Show the datasets as a tree, children below their parent"
	if datasetBrowser.treeView {
		toggleTreeViewName, toggleTreeViewDescription = "Flat list", "Show the datasets as a flat list"
	}
	shortcuts := []shortcut_helper.ShortcutEntry{
		uiutil.TableComponentShortcutColumns.WithRun(datasetBrowser.openColumnSelectionDialog).OnlyInMenu(),
		uiutil.TableComponentShortcutFilter.WithRun(datasetBrowser.tableContainer.StartFilter),
		{KeyCombo: []string{"u"}, Name: toggleUnmountedName, Description: toggleUnmountedDescription, Group: shortcut_helper.GroupView, Run: datasetBrowser.ToggleHideUnmounted, MenuOnly: true},
		{KeyCombo: []string{"t"}, Name: toggleTreeViewName, Description: toggleTreeViewDescription, Group: shortcut_helper.GroupView, Run: datasetBrowser.ToggleTreeView, MenuOnly: true},
	}
	selection := datasetBrowser.tableContainer.GetSelectedEntry()
	if datasetBrowser.treeView && selection != nil {
		shortcuts = append(shortcuts,
			shortcut_helper.ShortcutEntry{KeyCombo: []string{"-", "+"}, Name: "Collapse/expand", Group: shortcut_helper.GroupView},
			shortcut_helper.ShortcutEntry{KeyCombo: []string{"-"}, Name: "Collapse", Description: "Hide the children of the dataset, or go to its parent", Group: shortcut_helper.GroupView, Run: func() { datasetBrowser.handleTreeKey('-') }, MenuOnly: true},
			shortcut_helper.ShortcutEntry{KeyCombo: []string{"+"}, Name: "Expand", Description: "Show the children of the dataset, or go to the first one", Group: shortcut_helper.GroupView, Run: func() { datasetBrowser.handleTreeKey('+') }, MenuOnly: true},
			shortcut_helper.ShortcutEntry{KeyCombo: []string{"*"}, Name: "Collapse/expand all", Description: "Collapse or expand all datasets at once", Group: shortcut_helper.GroupView, Run: datasetBrowser.toggleCollapseAll, MenuOnly: true},
		)
	}
	if selection != nil {
		shortcuts = append(shortcuts,
			shortcut_helper.ShortcutEntry{KeyCombo: []string{"p"}, Name: "Permissions", Description: "Show and change the delegated ZFS permissions (zfs allow)", Run: func() { datasetBrowser.openPermissionsDialog(selection) }, MenuOnly: true},
			shortcut_helper.ShortcutEntry{KeyCombo: []string{"e"}, Name: "Properties", Description: "Show and change the ZFS properties (zfs get/set)", Run: func() { datasetBrowser.openPropertiesDialog(selection) }, MenuOnly: true},
		)
	}
	return shortcuts
}
