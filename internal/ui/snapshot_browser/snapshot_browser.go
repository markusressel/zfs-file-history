package snapshot_browser

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"strings"
	"sync/atomic"
	"time"
	"zfs-file-history/internal/data"
	"zfs-file-history/internal/data/diff_state"
	"zfs-file-history/internal/logging"
	"zfs-file-history/internal/ui/dialog"
	"zfs-file-history/internal/ui/shortcut_helper"
	"zfs-file-history/internal/ui/status_message"
	"zfs-file-history/internal/ui/table"
	uiutil "zfs-file-history/internal/ui/util"
	"zfs-file-history/internal/util"
	"zfs-file-history/internal/zfs"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

type SnapshotBrowserComponent struct {
	Events util.Emitter[Event]

	application *tview.Application

	container *uiutil.LoadingContainer
	loader    *uiutil.DataLoader[snapshotLoadResult]

	tableContainer *table.RowSelectionTable[data.SnapshotBrowserEntry]

	path             string
	hostDataset      *zfs.Dataset
	currentSnapshots []*zfs.Snapshot
	currentFileEntry *data.FileBrowserEntry

	selectedSnapshotMemory *uiutil.SelectionMemory[data.SnapshotBrowserEntry]

	isRestoringSelection bool

	diffCancel context.CancelFunc
	diffSeq    atomic.Uint64
}

type snapshotLoadResult struct {
	dataset   *zfs.Dataset
	snapshots []*zfs.Snapshot
}

var (
	columnName = &table.Column{
		Id:        0,
		Title:     "Name",
		Alignment: tview.AlignLeft,
	}
	columnDate = &table.Column{
		Id:        1,
		Title:     "Creation",
		Alignment: tview.AlignLeft,
	}
	columnDiff = &table.Column{
		Id:        2,
		Title:     "Diff",
		Alignment: tview.AlignCenter,
	}
	columnUsed = &table.Column{
		Id:        3,
		Title:     "Used",
		Alignment: tview.AlignCenter,
	}
	columnRefer = &table.Column{
		Id:        4,
		Title:     "Refer",
		Alignment: tview.AlignCenter,
	}
	columnRatio = &table.Column{
		Id:        5,
		Title:     "Ratio",
		Alignment: tview.AlignCenter,
	}
	columnClones = &table.Column{
		Id:        6,
		Title:     "Clones",
		Alignment: tview.AlignCenter,
	}

	tableColumns = []*table.Column{
		columnName, columnDate, columnDiff, columnUsed, columnRefer, columnRatio, columnClones,
	}

	initialActiveTableColumns = []*table.Column{
		columnName,
		columnDiff,
		columnDate,
		columnUsed,
	}
)

func NewSnapshotBrowser(application *tview.Application) *SnapshotBrowserComponent {
	snapshotBrowser := &SnapshotBrowserComponent{
		Events:                 *util.NewEmitter[Event](),
		application:            application,
		currentSnapshots:       []*zfs.Snapshot{},
		selectedSnapshotMemory: uiutil.NewSelectionMemory[data.SnapshotBrowserEntry](),
	}

	snapshotBrowser.tableContainer = createSnapshotBrowserTable(snapshotBrowser.application)
	snapshotBrowser.tableContainer.SetMultiSelect(true)

	snapshotBrowser.container = uiutil.NewLoadingContainer(application, snapshotBrowser.tableContainer.GetLayout(), "Snapshots", "Loading snapshots...")

	snapshotBrowser.loader = uiutil.NewDataLoader[snapshotLoadResult](application).
		OnStart(func() {
			snapshotBrowser.container.SetIsLoading(true)
			snapshotBrowser.emit(SelectedSnapshotChanged{nil})
		}).
		OnLoad(func(result snapshotLoadResult) {
			snapshotBrowser.container.SetIsLoading(false)

			datasetChanged := snapshotBrowser.hostDataset == nil || result.dataset == nil || snapshotBrowser.hostDataset.Path != result.dataset.Path
			if datasetChanged {
				snapshotBrowser.ClearMultiSelection()
			}

			snapshotBrowser.hostDataset = result.dataset
			snapshotBrowser.currentSnapshots = result.snapshots
			snapshotBrowser.updateCurrentSnapshotEntries(true)

			// ALWAYS emit the event after a load to ensure all components (like FileBrowser)
			// are synced with the latest selection, even if logically it's the same path.
			snapshotBrowser.emit(SelectedSnapshotChanged{snapshotBrowser.GetSelection()})
		}).
		OnError(func(err error) {
			snapshotBrowser.container.SetIsLoading(false)
			logging.Error("Could not load snapshots: %s", err.Error())
			snapshotBrowser.currentSnapshots = []*zfs.Snapshot{}
			snapshotBrowser.updateCurrentSnapshotEntries(true)
		})

	snapshotBrowser.setupTable()

	return snapshotBrowser
}

func (snapshotBrowser *SnapshotBrowserComponent) setupTable() {
	snapshotBrowser.tableContainer.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		key := event.Key()
		if key == tcell.KeyF2 || (event.Modifiers()&tcell.ModShift != 0 && (event.Rune() == 'C' || event.Rune() == 'c')) {
			snapshotBrowser.openColumnSelectionDialog()
			return nil
		}
		if snapshotBrowser.GetSelection() != nil {
			if key == tcell.KeyEnter {
				if snapshotBrowser.HasMultiSelection() {
					multiSelectionEntries := snapshotBrowser.tableContainer.GetMultiSelection()
					if len(multiSelectionEntries) <= 1 {
						snapshotBrowser.openActionDialog(multiSelectionEntries[0])
					} else {
						snapshotBrowser.openMultiActionDialog(multiSelectionEntries)
					}
				} else {
					snapshotBrowser.openActionDialog(snapshotBrowser.GetSelection())
				}
				return nil
			} else if event.Rune() == 'd' {
				currentSelection := snapshotBrowser.GetSelection()
				if currentSelection != nil {
					snapshotBrowser.openDeleteDialog(currentSelection)
				}
				return nil
			}
		}
		return event
	})

	snapshotBrowser.tableContainer.SetColumnSpec(tableColumns, columnDate, true)
	snapshotBrowser.tableContainer.SetActiveColumns(initialActiveTableColumns)
	snapshotBrowser.tableContainer.SetSelectionChangedCallback(func(entry *data.SnapshotBrowserEntry) {
		if snapshotBrowser.isRestoringSelection {
			return
		}
		snapshotBrowser.rememberSelectionForDataset(entry)
		snapshotBrowser.updateTableTitle()
		snapshotBrowser.emit(SelectedSnapshotChanged{entry})
	})
}

func (snapshotBrowser *SnapshotBrowserComponent) GetLayout() *uiutil.LoadingContainer {
	return snapshotBrowser.container
}

func (snapshotBrowser *SnapshotBrowserComponent) SetPath(path string, force bool) {
	if !force && snapshotBrowser.path == path {
		return
	}
	snapshotBrowser.path = path
	snapshotBrowser.reloadSnapshotEntries(force)
}

func (snapshotBrowser *SnapshotBrowserComponent) Refresh(force bool) {
	zfs.RefreshZfsData()
	snapshotBrowser.SetPath(snapshotBrowser.path, force)
}

func (snapshotBrowser *SnapshotBrowserComponent) SetFileEntry(fileEntry *data.FileBrowserEntry) {
	if fileEntry != nil &&
		snapshotBrowser.currentFileEntry != nil &&
		snapshotBrowser.currentFileEntry.GetRealPath() == fileEntry.GetRealPath() &&
		snapshotBrowser.currentFileEntry.DiffState == fileEntry.DiffState {
		return
	}
	snapshotBrowser.currentFileEntry = fileEntry
	snapshotBrowser.updateCurrentSnapshotEntries(false)
}

func (snapshotBrowser *SnapshotBrowserComponent) reloadSnapshotEntries(force bool) {
	path := snapshotBrowser.path

	// Optimized check: if we are not forcing a refresh and we are still
	// within the current dataset, we can load quietly.
	isSubpath := false
	if snapshotBrowser.hostDataset != nil {
		dsPath := snapshotBrowser.hostDataset.Path
		if path == dsPath || strings.HasPrefix(path, dsPath+"/") {
			isSubpath = true
		}
	}

	capturedHostDataset := snapshotBrowser.hostDataset
	capturedSnapshots := snapshotBrowser.currentSnapshots

	// If we know it's a different dataset (or force), clear state immediately
	// to avoid showing outdated snapshots while the new ones load.
	if force || !isSubpath {
		snapshotBrowser.hostDataset = nil
		snapshotBrowser.currentSnapshots = []*zfs.Snapshot{}
		snapshotBrowser.ClearMultiSelection()
		snapshotBrowser.updateTableEntries()
		snapshotBrowser.emit(SelectedSnapshotChanged{nil})
	}

	loadFunc := func(ctx context.Context) (snapshotLoadResult, error) {
		ds, err := zfs.FindHostDataset(path)
		if err != nil {
			return snapshotLoadResult{}, err
		}

		// Optimization: if the dataset hasn't changed, we don't need to reload the snapshots
		if !force && capturedHostDataset != nil && capturedHostDataset.Path == ds.Path {
			return snapshotLoadResult{dataset: ds, snapshots: capturedSnapshots}, nil
		}

		snapshots, err := ds.GetSnapshots()
		if err != nil {
			return snapshotLoadResult{dataset: ds}, err
		}

		return snapshotLoadResult{dataset: ds, snapshots: snapshots}, nil
	}

	if !force && isSubpath {
		snapshotBrowser.loader.LoadQuietly(loadFunc)
	} else {
		snapshotBrowser.loader.Load(loadFunc)
	}
}
func (snapshotBrowser *SnapshotBrowserComponent) updateCurrentSnapshotEntries(quiet bool) {
	snapshotBrowser.updateTableEntries()
	snapshotBrowser.restoreSelectionForDataset(quiet)
}

func (snapshotBrowser *SnapshotBrowserComponent) updateTableEntries() {
	snapshotBrowser.startAsyncDiffCalculation()
}

func (snapshotBrowser *SnapshotBrowserComponent) cancelDiffCalculation() {
	if snapshotBrowser.diffCancel != nil {
		snapshotBrowser.diffCancel()
		snapshotBrowser.diffCancel = nil
	}
}

func (snapshotBrowser *SnapshotBrowserComponent) startAsyncDiffCalculation() {
	snapshotBrowser.cancelDiffCalculation()

	snapshots := snapshotBrowser.currentSnapshots
	fileEntry := snapshotBrowser.currentFileEntry

	// If no snapshots, clear the table instantly
	if len(snapshots) == 0 {
		snapshotBrowser.tableContainer.SetData([]*data.SnapshotBrowserEntry{})
		snapshotBrowser.updateTableTitle()
		return
	}

	// Create cancellable context and increment sequence
	ctx, cancel := context.WithCancel(context.Background())
	snapshotBrowser.diffCancel = cancel
	seq := snapshotBrowser.diffSeq.Add(1)

	// Step 1: Instantly render table with Unknown ("N/A") states
	initialEntries := make([]*data.SnapshotBrowserEntry, len(snapshots))
	for i, snap := range snapshots {
		initialEntries[i] = &data.SnapshotBrowserEntry{
			Snapshot:  snap,
			DiffState: diff_state.Unknown,
		}
	}
	snapshotBrowser.tableContainer.SetData(initialEntries)
	snapshotBrowser.updateTableTitle()

	// Get the sorted entries from the table container (determines screen top-to-bottom layout)
	sortedEntries := snapshotBrowser.tableContainer.GetEntries()

	// Step 2: Compute actual diffs in background goroutine, processing them in sorted order
	go func() {
		filePath := ""
		if fileEntry != nil {
			filePath = fileEntry.GetRealPath()
		}

		// Keep a local working copy in the exact sorted order
		localEntries := make([]*data.SnapshotBrowserEntry, len(sortedEntries))
		for i, entry := range sortedEntries {
			localEntries[i] = &data.SnapshotBrowserEntry{
				Snapshot:  entry.Snapshot,
				DiffState: diff_state.Unknown,
			}
		}

		// Closure to safely push local updates to UI thread
		updateUI := func() {
			entriesCopy := make([]*data.SnapshotBrowserEntry, len(localEntries))
			for i, entry := range localEntries {
				entriesCopy[i] = &data.SnapshotBrowserEntry{
					Snapshot:  entry.Snapshot,
					DiffState: entry.DiffState,
				}
			}

			snapshotBrowser.application.QueueUpdateDraw(func() {
				// Discard update if a newer selection calculation has started
				if seq != snapshotBrowser.diffSeq.Load() {
					return
				}
				snapshotBrowser.tableContainer.SetData(entriesCopy)
				snapshotBrowser.updateTableTitle()
			})
		}

		for i, entry := range localEntries {
			// Abort immediately if user changed selection and cancelled context
			if ctx.Err() != nil {
				return
			}

			if filePath != "" {
				entry.DiffState = entry.Snapshot.DetermineDiffState(filePath)
			}

			// Incremental Redraw Logic:
			// - First 5 entries: Redraw after each (gives instant feedback on visible entries)
			// - Remaining entries: Batch update every 10 entries (optimizes UI rendering)
			// - Final entry: Always push the final update
			isFirstFew := i < 5
			isBatchEnd := (i+1)%10 == 0
			isLast := i == len(localEntries)-1

			if isFirstFew || isBatchEnd || isLast {
				updateUI()
			}
		}
	}()
}

func (snapshotBrowser *SnapshotBrowserComponent) Focus() {
	snapshotBrowser.application.SetFocus(snapshotBrowser.container)
}

func (snapshotBrowser *SnapshotBrowserComponent) HasFocus() bool {
	return snapshotBrowser.container.HasFocus()
}

func (snapshotBrowser *SnapshotBrowserComponent) updateTableTitle() {
	title := "Snapshots"
	if snapshotBrowser.GetSelection() != nil {
		currentSelectionIndex := slices.Index(snapshotBrowser.GetEntries(), snapshotBrowser.GetSelection()) + 1
		totalEntriesCount := len(snapshotBrowser.GetEntries())
		title = fmt.Sprintf("Snapshot: %s (%d/%d)", snapshotBrowser.GetSelection().Snapshot.Name, currentSelectionIndex, totalEntriesCount)
	}
	snapshotBrowser.tableContainer.SetTitle(title)
}

func (snapshotBrowser *SnapshotBrowserComponent) clear() {
	snapshotBrowser.path = ""
	snapshotBrowser.currentSnapshots = []*zfs.Snapshot{}
	snapshotBrowser.updateCurrentSnapshotEntries(false)
}

func (snapshotBrowser *SnapshotBrowserComponent) rememberSelectionForDataset(selection *data.SnapshotBrowserEntry) {
	if snapshotBrowser.hostDataset == nil {
		return
	}
	snapshotBrowser.selectedSnapshotMemory.Remember(
		snapshotBrowser.hostDataset.Path,
		slices.Index(snapshotBrowser.GetEntries(), selection),
		selection,
	)
}

func (snapshotBrowser *SnapshotBrowserComponent) getRememberedSelectionInfo(path string) *uiutil.SelectionInfo[data.SnapshotBrowserEntry] {
	return snapshotBrowser.selectedSnapshotMemory.Get(path)
}

func (snapshotBrowser *SnapshotBrowserComponent) restoreSelectionForDataset(quiet bool) {
	if quiet {
		snapshotBrowser.isRestoringSelection = true
		defer func() {
			snapshotBrowser.isRestoringSelection = false
		}()
	}

	var entryToSelect *data.SnapshotBrowserEntry
	if snapshotBrowser.hostDataset == nil {
		snapshotBrowser.selectSnapshot(entryToSelect, quiet)
		return
	}

	entries := snapshotBrowser.GetEntries()
	if len(entries) == 0 {
		snapshotBrowser.selectSnapshot(nil, quiet)
		return
	}

	rememberedSelectionInfo := snapshotBrowser.getRememberedSelectionInfo(snapshotBrowser.hostDataset.Path)
	if rememberedSelectionInfo == nil {
		if len(entries) > 0 {
			entryToSelect = entries[0]
		}
	} else {
		var index int
		if rememberedSelectionInfo.Entry == nil {
			snapshotBrowser.selectHeader()
			return
		} else {
			index = slices.IndexFunc(entries, func(entry *data.SnapshotBrowserEntry) bool {
				return entry.Snapshot.Name == rememberedSelectionInfo.Entry.Snapshot.Name
			})
		}
		if index < 0 {
			closestIndex := util.Coerce(rememberedSelectionInfo.Index, 0, len(entries)-1)
			entryToSelect = entries[closestIndex]
		} else {
			entryToSelect = entries[index]
		}
	}
	snapshotBrowser.selectSnapshot(entryToSelect, quiet)
}

func (snapshotBrowser *SnapshotBrowserComponent) selectSnapshot(snapshot *data.SnapshotBrowserEntry, quiet bool) {
	if snapshotBrowser.GetSelection() == snapshot || (snapshotBrowser.GetSelection() != nil && snapshot != nil && snapshotBrowser.GetSelection().Snapshot.Path == snapshot.Snapshot.Path) {
		return
	}

	if !quiet {
		defer func() {
			snapshotBrowser.emit(SelectedSnapshotChanged{snapshot})
		}()
	}

	snapshotBrowser.tableContainer.Select(snapshot)
}

func (snapshotBrowser *SnapshotBrowserComponent) GetSelection() *data.SnapshotBrowserEntry {
	return snapshotBrowser.tableContainer.GetSelectedEntry()
}

func (snapshotBrowser *SnapshotBrowserComponent) GetEntries() []*data.SnapshotBrowserEntry {
	return snapshotBrowser.tableContainer.GetEntries()
}

func (snapshotBrowser *SnapshotBrowserComponent) selectHeader() {
	snapshotBrowser.tableContainer.SelectHeader()
}

func (snapshotBrowser *SnapshotBrowserComponent) selectFirstIfExists() {
	snapshotBrowser.tableContainer.SelectFirstIfExists()
}

func (snapshotBrowser *SnapshotBrowserComponent) openActionDialog(selection *data.SnapshotBrowserEntry) {
	if snapshotBrowser.GetSelection() == nil {
		return
	}
	actionDialogLayout := dialog.NewSnapshotActionDialog(snapshotBrowser.application, selection)
	actionHandler := func(action dialog.DialogActionId) bool {
		switch action {
		case dialog.SnapshotDialogCreateSnapshotActionId:
			name, err := snapshotBrowser.createSnapshot(selection)
			if err != nil {
				logging.Error("Failed to create snapshot: %s", err.Error())
				snapshotBrowser.showStatusMessage(status_message.NewErrorStatusMessage(fmt.Sprintf("Failed to create snapshot: %s", err)))
			}
			snapshotBrowser.SelectLatest()
			snapshotBrowser.emit(SnapshotCreated{
				SnapshotName: name,
			})
			return true
		case dialog.SnapshotDialogDestroySnapshotActionId:
			err := snapshotBrowser.destroySnapshot(selection, false, false)
			if err != nil {
				logging.Error("Failed to destroy snapshot: %s", err.Error())
				snapshotBrowser.emit(StatusMessageEvent{
					Message: status_message.NewErrorStatusMessage(fmt.Sprintf("Failed to destroy snapshot: %s", err)),
				})
			} else {
				snapshotBrowser.emit(StatusMessageEvent{
					Message: status_message.NewSuccessStatusMessage(fmt.Sprintf("Snapshot '%s' destroyed.", selection.Snapshot.Name)),
				})
			}
			return true
		case dialog.SnapshotDialogDestroySnapshotRecursivelyActionId:
			err := snapshotBrowser.destroySnapshot(selection, true, true)
			if err != nil {
				logging.Error("Failed to destroy snapshot: %s", err.Error())
				snapshotBrowser.emit(StatusMessageEvent{
					Message: status_message.NewErrorStatusMessage(fmt.Sprintf("Failed to destroy snapshot: %s", err)),
				})
			} else {
				snapshotBrowser.emit(StatusMessageEvent{
					Message: status_message.NewSuccessStatusMessage(fmt.Sprintf("Snapshot '%s' destroyed.", selection.Snapshot.Name)),
				})
			}
			return true
		}
		return false
	}
	snapshotBrowser.showDialog(actionDialogLayout, actionHandler)
}

func (snapshotBrowser *SnapshotBrowserComponent) openMultiActionDialog(entries []*data.SnapshotBrowserEntry) {
	if len(entries) <= 0 {
		return
	}
	actionDialogLayout := dialog.NewMultiSnapshotActionDialog(snapshotBrowser.application, entries)
	actionHandler := func(action dialog.DialogActionId) bool {
		switch action {
		case dialog.MultiSnapshotDialogClearSelectionActionId:
			snapshotBrowser.ClearMultiSelection()
		case dialog.MultiSnapshotDialogDestroySnapshotActionId:
			snapshotBrowser.ClearMultiSelection()
			for _, entry := range entries {
				err := snapshotBrowser.destroySnapshot(entry, false, false)
				if err != nil {
					logging.Error("Failed to destroy snapshot: %s", err.Error())
					snapshotBrowser.emit(StatusMessageEvent{
						Message: status_message.NewErrorStatusMessage(fmt.Sprintf("Failed to destroy snapshot: %s", err)),
					})
				}
			}
			return true
		case dialog.MultiSnapshotDialogDestroySnapshotRecursivelyActionId:
			snapshotBrowser.ClearMultiSelection()
			for _, entry := range entries {
				err := snapshotBrowser.destroySnapshot(entry, true, true)
				if err != nil {
					logging.Error("Failed to destroy snapshot: %s", err.Error())
					snapshotBrowser.emit(StatusMessageEvent{
						Message: status_message.NewErrorStatusMessage(fmt.Sprintf("Failed to destroy snapshot: %s", err)),
					})
				}
			}
			return true
		}
		return false
	}
	snapshotBrowser.showDialog(actionDialogLayout, actionHandler)
}

func (snapshotBrowser *SnapshotBrowserComponent) openDeleteDialog(selection *data.SnapshotBrowserEntry) {
	if selection == nil {
		return
	}
	deleteDialogLayout := dialog.NewDeleteSnapshotDialog(snapshotBrowser.application, selection)
	deleteHandler := func(action dialog.DialogActionId) bool {
		switch action {
		case dialog.DeleteSnapshotDialogDeleteSnapshotActionId:
			err := snapshotBrowser.destroySnapshot(selection, false, false)
			if err != nil {
				logging.Error("Failed to destroy snapshot: %s", err.Error())
				snapshotBrowser.emit(StatusMessageEvent{
					Message: status_message.NewErrorStatusMessage(fmt.Sprintf("Failed to destroy snapshot: %s", err)),
				})
			}
			// TODO: this could be optimized by simply removing the entry on success instead of reloading all entries
			snapshotBrowser.reloadSnapshotEntries(true)
			return true
		default:
			return false
		}
	}
	snapshotBrowser.showDialog(deleteDialogLayout, deleteHandler)
}

func (snapshotBrowser *SnapshotBrowserComponent) showDialog(d dialog.Dialog, actionHandler func(action dialog.DialogActionId) bool) {
	dialog.ShowDialogOnPages(snapshotBrowser.application, snapshotBrowser.container.Pages, d, actionHandler, nil)
}

func (snapshotBrowser *SnapshotBrowserComponent) openColumnSelectionDialog() {
	d := dialog.NewColumnSelectionDialog(
		snapshotBrowser.application,
		"Configure Snapshot Columns",
		tableColumns,
		snapshotBrowser.tableContainer.GetColumnSpec(),
		func(activeColumns []*table.Column) {
			snapshotBrowser.tableContainer.SetActiveColumns(activeColumns)
		},
	)
	snapshotBrowser.showDialog(d, func(action dialog.DialogActionId) bool {
		return false
	})
}

func (snapshotBrowser *SnapshotBrowserComponent) createSnapshot(entry *data.SnapshotBrowserEntry) (name string, err error) {
	name = fmt.Sprintf("zfh-%s", time.Now().Format(zfs.SnapshotTimeFormat))
	err = entry.Snapshot.ParentDataset.CreateSnapshot(name)
	if err != nil {
		return "", err
	}
	snapshotBrowser.Refresh(true)
	return name, nil
}

func (snapshotBrowser *SnapshotBrowserComponent) destroySnapshot(entry *data.SnapshotBrowserEntry, recursive bool, dependantClones bool) (err error) {
	snapshot := entry.Snapshot
	err = snapshot.Destroy(recursive, dependantClones)
	if err != nil {
		return err
	}
	snapshotBrowser.Refresh(true)
	return nil
}

func (snapshotBrowser *SnapshotBrowserComponent) SelectLatest() {
	entries := snapshotBrowser.GetEntries()

	var sortedEntries []*data.SnapshotBrowserEntry
	sortedEntries = append(sortedEntries, entries...)
	if len(sortedEntries) <= 0 {
		return
	}

	sort.SliceStable(sortedEntries, func(i, j int) bool {
		a := sortedEntries[i]
		b := sortedEntries[j]
		return a.Snapshot.Properties.CreationDate.After(b.Snapshot.Properties.CreationDate)
	})

	latestEntry := sortedEntries[0]
	snapshotBrowser.tableContainer.Select(latestEntry)
}

func (snapshotBrowser *SnapshotBrowserComponent) showStatusMessage(message *status_message.StatusMessage) {
	snapshotBrowser.emit(StatusMessageEvent{
		Message: message,
	})
}

func (snapshotBrowser *SnapshotBrowserComponent) emit(event Event) {
	snapshotBrowser.Events.Emit(event)
}

func (snapshotBrowser *SnapshotBrowserComponent) HasMultiSelection() bool {
	return snapshotBrowser.tableContainer.HasMultiSelection()
}

func (snapshotBrowser *SnapshotBrowserComponent) ClearMultiSelection() {
	snapshotBrowser.tableContainer.ClearMultiSelection()
	snapshotBrowser.application.ForceDraw()
}

func (snapshotBrowser *SnapshotBrowserComponent) GetShortcutMap() []shortcut_helper.ShortcutEntry {
	shortcutMap := []shortcut_helper.ShortcutEntry{
		uiutil.TableComponentShortcutUp,
		uiutil.TableComponentShortcutDown,
		uiutil.TableComponentShortcutColumns,
	}

	if snapshotBrowser.GetSelection() != nil {
		shortcutMap = append(shortcutMap,
			uiutil.TableComponentShortcutActions,
			uiutil.TableComponentShortcutDelete,
		)
	} else {
		shortcutMap = append(shortcutMap,
			uiutil.TableComponentShortcutFlipColumnDirection,
			uiutil.TableComponentShortcutCycleSortColumnLeft,
			uiutil.TableComponentShortcutCycleSortColumnRight,
		)
	}

	return shortcutMap
}
