package dataset_browser

import (
	"cmp"
	"context"
	"fmt"
	"sort"
	"strings"
	"zfs-file-history/internal/data"
	"zfs-file-history/internal/logging"
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
	columnAvail = &table.Column{
		Id:        2,
		Title:     "Avail",
		Alignment: tview.AlignRight,
	}
	columnMountpoint = &table.Column{
		Id:        3,
		Title:     "Mountpoint",
		Alignment: tview.AlignLeft,
	}

	tableColumns = []*table.Column{
		columnName,
		columnUsed,
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

type RequestFocusEvent struct {
	Layout tview.Primitive
}

type PathChangedEvent struct {
	NewPath string
}

type DatasetBrowserStatusEvent struct {
	Message *status_message.StatusMessage
}

type RequestFileHistoryEvent struct {
	FileEntry *data.FileBrowserEntry
}

type CreateSnapshotEvent struct {
	SnapshotName string
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
}

func NewDatasetBrowser(application *tview.Application) *DatasetBrowserComponent {
	datasetBrowser := &DatasetBrowserComponent{
		Events:        util.NewEmitter[Event](),
		application:   application,
		hideUnmounted: true,
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
		case columnUsed:
			text = uiutil.StableLengthHumanizedBytes(entry.Used)
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

func (datasetBrowser *DatasetBrowserComponent) sortTableEntries(entries []*zfs.DatasetListEntry, column *table.Column, inverted bool) []*zfs.DatasetListEntry {
	return sortDatasetEntries(entries, column, inverted)
}

func sortDatasetEntries(entries []*zfs.DatasetListEntry, column *table.Column, inverted bool) []*zfs.DatasetListEntry {
	sort.SliceStable(entries, func(i, j int) bool {
		a := entries[i]
		b := entries[j]

		result := 0
		switch column {
		case columnUsed:
			result = cmp.Compare(a.Used, b.Used)
		case columnAvail:
			result = cmp.Compare(a.Available, b.Available)
		case columnMountpoint:
			result = strings.Compare(displayedMountpoint(a), displayedMountpoint(b))
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

func (datasetBrowser *DatasetBrowserComponent) setupTable() {
	datasetBrowser.tableContainer.SetTitle("Datasets")
	datasetBrowser.tableContainer.SetFilterFunc(datasetMatchesFilter)
	datasetBrowser.tableContainer.SetFilterChangedCallback(datasetBrowser.updateStatus)
	datasetBrowser.updateStatus()
	datasetBrowser.tableContainer.SetColumnSpec(tableColumns, columnName, true)
	datasetBrowser.tableContainer.SetActiveColumns(tableColumns)

	datasetBrowser.tableContainer.SetSelectionChangedCallback(func(selectedEntry *zfs.DatasetListEntry) {
		datasetBrowser.Events.Emit(SelectedDatasetChangedEvent{selectedEntry})
		if selectedEntry != nil {
			datasetBrowser.setCurrentPath(selectedEntry.MountPath)
		}
	})

	datasetBrowser.tableContainer.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		key := event.Key()

		if event.Rune() == 'u' {
			datasetBrowser.ToggleHideUnmounted()
			return nil
		}

		// on the header row, these keys are handled by the table to change the sort order
		if datasetBrowser.tableContainer.GetSelectedEntry() != nil {
			if key == tcell.KeyRight || key == tcell.KeyEnter {
				// might open an action menu for dataset later
				return nil
			}
		}
		return event
	})
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
		len(datasetBrowser.tableContainer.GetEntries()),
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

func (datasetBrowser *DatasetBrowserComponent) SetSelectedSnapshot(snapshot *data.SnapshotBrowserEntry) {
	// Optional: highlight dataset that contains this snapshot
}

func (datasetBrowser *DatasetBrowserComponent) GetShortcutMap() []shortcut_helper.ShortcutEntry {
	toggleUnmountedName := "Hide unmounted"
	if datasetBrowser.hideUnmounted {
		toggleUnmountedName = "Show unmounted"
	}
	return []shortcut_helper.ShortcutEntry{
		{KeyCombo: []string{"Enter"}, Name: "Enter Dataset"},
		uiutil.TableComponentShortcutFilter,
		{KeyCombo: []string{"u"}, Name: toggleUnmountedName},
	}
}
