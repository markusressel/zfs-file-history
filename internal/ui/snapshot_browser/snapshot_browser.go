package snapshot_browser

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"strings"
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

	selectLatestOnNextLoad bool

	diffLoader *uiutil.DebouncedLoader
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

	snapshotBrowser.diffLoader = uiutil.NewDebouncedLoader(application, func() {
		currentSelection := snapshotBrowser.GetSelection()
		snapshotBrowser.isRestoringSelection = true
		snapshotBrowser.tableContainer.SetData(snapshotBrowser.tableContainer.GetEntries())
		if currentSelection != nil {
			snapshotBrowser.tableContainer.Select(currentSelection)
		}
		snapshotBrowser.isRestoringSelection = false
		snapshotBrowser.updateTableTitle()
	})

	snapshotBrowser.tableContainer = snapshotBrowser.createSnapshotBrowserTable(snapshotBrowser.application)
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

			if snapshotBrowser.selectLatestOnNextLoad {
				snapshotBrowser.SelectLatest()
				snapshotBrowser.selectLatestOnNextLoad = false
			}

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

func (snapshotBrowser *SnapshotBrowserComponent) SetBorderColor(color tcell.Color) {
	if flex, ok := snapshotBrowser.tableContainer.GetLayout().(*tview.Flex); ok {
		flex.SetBorderColor(color)
	}
	if snapshotBrowser.container != nil {
		snapshotBrowser.container.SetBorderColor(color)
	}
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
	if snapshotBrowser.diffLoader != nil {
		snapshotBrowser.diffLoader.Cancel()
	}
}

func (snapshotBrowser *SnapshotBrowserComponent) startAsyncDiffCalculation() {
	snapshots := snapshotBrowser.currentSnapshots
	fileEntry := snapshotBrowser.currentFileEntry

	if len(snapshots) == 0 {
		snapshotBrowser.tableContainer.SetData([]*data.SnapshotBrowserEntry{})
		snapshotBrowser.updateTableTitle()
		return
	}

	ctx, seq := snapshotBrowser.diffLoader.Start()

	currentEntries := snapshotBrowser.tableContainer.GetEntries()
	sameSnapshots := len(currentEntries) == len(snapshots)
	if sameSnapshots {
		for i, entry := range currentEntries {
			if entry == nil || entry.Snapshot == nil || entry.Snapshot.Name != snapshots[i].Name {
				sameSnapshots = false
				break
			}
		}
	}

	if !sameSnapshots {
		previousDiffs := make(map[string]diff_state.DiffState)
		for _, entry := range currentEntries {
			if entry != nil {
				previousDiffs[entry.Snapshot.Name] = entry.DiffState
			}
		}

		initialEntries := make([]*data.SnapshotBrowserEntry, len(snapshots))
		for i, snap := range snapshots {
			diffState := diff_state.Unknown
			if oldState, exists := previousDiffs[snap.Name]; exists {
				diffState = oldState
			}
			initialEntries[i] = &data.SnapshotBrowserEntry{
				Snapshot:  snap,
				DiffState: diffState,
				IsLoading: true,
			}
		}
		snapshotBrowser.tableContainer.SetData(initialEntries)
		snapshotBrowser.updateTableTitle()
	}

	entriesToProcess := slices.Clone(snapshotBrowser.tableContainer.GetEntries())

	go func() {
		defer snapshotBrowser.diffLoader.Stop(seq)

		// Debounce rapid scrolling
		if sameSnapshots {
			select {
			case <-ctx.Done():
				return
			case <-time.After(50 * time.Millisecond):
			}

			// Preemptively set loading state in case computation takes a while
			snapshotBrowser.application.QueueUpdate(func() {
				if !snapshotBrowser.diffLoader.IsCurrentSequence(seq) {
					return
				}
				for _, entry := range entriesToProcess {
					if entry != nil {
						entry.IsLoading = true
						snapshotBrowser.tableContainer.UpdateEntry(entry)
					}
				}
			})
		}

		filePath := ""
		if fileEntry != nil {
			filePath = fileEntry.GetRealPath()
		}

		type diffResult struct {
			entry *data.SnapshotBrowserEntry
			state diff_state.DiffState
		}
		var batch []diffResult
		lastDrawTime := time.Now()

		pushBatch := func(forceDraw bool) {
			if len(batch) == 0 {
				return
			}
			batchCopy := batch
			batch = nil

			updateFunc := func() {
				if !snapshotBrowser.diffLoader.IsCurrentSequence(seq) {
					return
				}
				for _, res := range batchCopy {
					res.entry.DiffState = res.state
					res.entry.IsLoading = false
					snapshotBrowser.tableContainer.UpdateEntry(res.entry)
				}
			}

			if forceDraw {
				snapshotBrowser.application.QueueUpdateDraw(updateFunc)
			} else {
				snapshotBrowser.application.QueueUpdate(updateFunc)
			}
		}

		for i, entry := range entriesToProcess {
			if ctx.Err() != nil {
				return
			}

			diffState := diff_state.Unknown
			if filePath != "" {
				diffState = entry.Snapshot.DetermineDiffState(filePath)
			}

			batch = append(batch, diffResult{entry: entry, state: diffState})

			now := time.Now()
			isLast := i == len(entriesToProcess)-1
			// Draw at most once every 50ms to prevent SSH connection flooding
			if isLast || now.Sub(lastDrawTime) > 50*time.Millisecond {
				pushBatch(true)
				lastDrawTime = now
			} else if len(batch) >= 10 {
				pushBatch(false)
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

func (snapshotBrowser *SnapshotBrowserComponent) GetCurrentSnapshots() []*zfs.Snapshot {
	return snapshotBrowser.currentSnapshots
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

	var createdName string

	asyncWork := func(d *dialog.SelectionDialog, action dialog.DialogActionId) error {
		switch action {
		case dialog.SnapshotDialogCreateSnapshotActionId:
			name, err := snapshotBrowser.createSnapshot(selection)
			createdName = name
			return err
		case dialog.SnapshotDialogDestroySnapshotActionId:
			return snapshotBrowser.destroySnapshot(selection, false, false)
		case dialog.SnapshotDialogDestroySnapshotRecursivelyActionId:
			return snapshotBrowser.destroySnapshot(selection, true, true)
		}
		return nil
	}

	onComplete := func(d *dialog.SelectionDialog, option *dialog.DialogOption, err error) {
		d.Close() // Dismiss selection menu

		if err != nil {
			logging.Error("Action failed: %s", err.Error())
			errDialog := dialog.NewErrorDialog(snapshotBrowser.application, "Operation Failed", err)
			snapshotBrowser.showDialog(errDialog, nil)
			return
		}

		// Handle downstream states depending on what succeeded
		switch option.Id {
		case dialog.SnapshotDialogCreateSnapshotActionId:
			snapshotBrowser.selectLatestOnNextLoad = true

			successDialog := dialog.NewSuccessDialog(snapshotBrowser.application, "Snapshot Created", fmt.Sprintf("Snapshot '%s' created successfully.", createdName))
			snapshotBrowser.showDialog(successDialog, nil)

		case dialog.SnapshotDialogDestroySnapshotActionId, dialog.SnapshotDialogDestroySnapshotRecursivelyActionId:
			successDialog := dialog.NewSuccessDialog(snapshotBrowser.application, "Snapshot Destroyed", fmt.Sprintf("Snapshot '%s' destroyed.", selection.Snapshot.Name))
			snapshotBrowser.showDialog(successDialog, nil)
		}

		snapshotBrowser.Refresh(true)
	}

	actionDialog := dialog.NewSnapshotActionDialog(snapshotBrowser.application, selection, asyncWork, onComplete)
	snapshotBrowser.showDialog(actionDialog, nil)
}

func (snapshotBrowser *SnapshotBrowserComponent) openMultiActionDialog(entries []*data.SnapshotBrowserEntry) {
	if len(entries) <= 0 {
		return
	}

	asyncWork := func(d *dialog.SelectionDialog, action dialog.DialogActionId) error {
		switch action {
		case dialog.MultiSnapshotDialogDestroySnapshotActionId:
			for _, entry := range entries {
				if err := snapshotBrowser.destroySnapshot(entry, false, false); err != nil {
					logging.Error("Failed to destroy snapshot: %s", err.Error())
					return err // Break early on failure
				}
			}
		case dialog.MultiSnapshotDialogDestroySnapshotRecursivelyActionId:
			for _, entry := range entries {
				if err := snapshotBrowser.destroySnapshot(entry, true, true); err != nil {
					logging.Error("Failed to destroy snapshot: %s", err.Error())
					return err // Break early on failure
				}
			}
		}
		return nil
	}

	onComplete := func(d *dialog.SelectionDialog, option *dialog.DialogOption, err error) {
		d.Close()

		// Always clear selection states after an action choice completes
		if option.Id == dialog.MultiSnapshotDialogClearSelectionActionId || option.Id == dialog.MultiSnapshotDialogDestroySnapshotActionId || option.Id == dialog.MultiSnapshotDialogDestroySnapshotRecursivelyActionId {
			snapshotBrowser.ClearMultiSelection()
		}

		if err != nil {
			errDialog := dialog.NewErrorDialog(snapshotBrowser.application, "Batch Destroy Failed", err)
			snapshotBrowser.showDialog(errDialog, nil)
			return
		}

		if option.Id == dialog.MultiSnapshotDialogDestroySnapshotActionId || option.Id == dialog.MultiSnapshotDialogDestroySnapshotRecursivelyActionId {
			successDialog := dialog.NewSuccessDialog(snapshotBrowser.application, "Snapshots Destroyed", fmt.Sprintf("Successfully destroyed %d snapshots.", len(entries)))
			snapshotBrowser.showDialog(successDialog, nil)
		}
	}

	actionDialog := dialog.NewMultiSnapshotActionDialog(snapshotBrowser.application, entries, asyncWork, onComplete)
	snapshotBrowser.showDialog(actionDialog, nil)
}

func (snapshotBrowser *SnapshotBrowserComponent) openDeleteDialog(selection *data.SnapshotBrowserEntry) {
	if selection == nil {
		return
	}

	asyncWork := func(d *dialog.SelectionDialog, action dialog.DialogActionId) error {
		if action == dialog.DeleteSnapshotDialogDeleteSnapshotActionId {
			return snapshotBrowser.destroySnapshot(selection, false, false)
		}
		return nil
	}

	onComplete := func(d *dialog.SelectionDialog, option *dialog.DialogOption, err error) {
		d.Close()

		if err != nil {
			logging.Error("Failed to destroy snapshot: %s", err.Error())
			errDialog := dialog.NewErrorDialog(snapshotBrowser.application, "Delete Failed", err)
			snapshotBrowser.showDialog(errDialog, nil)
			return
		}

		// Reload on success thread
		snapshotBrowser.reloadSnapshotEntries(true)
	}

	deleteDialog := dialog.NewDeleteSnapshotDialog(snapshotBrowser.application, selection, asyncWork, onComplete)
	snapshotBrowser.showDialog(deleteDialog, nil)
}

func (snapshotBrowser *SnapshotBrowserComponent) showDialog(d dialog.Dialog, onClosed func()) {
	dialog.ShowDialogOnPages(snapshotBrowser.application, snapshotBrowser.container.Pages, d, onClosed)
}

func (snapshotBrowser *SnapshotBrowserComponent) openColumnSelectionDialog() {
	currentActive := snapshotBrowser.tableContainer.GetColumnSpec()

	d := dialog.NewColumnSelectionDialog(
		snapshotBrowser.application,
		"Configure Snapshot Columns",
		tableColumns,
		slices.Clone(currentActive),
		func(activeColumns []*table.Column) {
			snapshotBrowser.tableContainer.SetActiveColumns(activeColumns)
		},
	)
	snapshotBrowser.showDialog(d, nil)
}

func (snapshotBrowser *SnapshotBrowserComponent) createSnapshot(entry *data.SnapshotBrowserEntry) (name string, err error) {
	name = fmt.Sprintf("zfh-%s", time.Now().Format(zfs.SnapshotTimeFormat))
	err = entry.Snapshot.ParentDataset.CreateSnapshot(name)
	if err != nil {
		return "", err
	}
	return name, nil
}

func (snapshotBrowser *SnapshotBrowserComponent) destroySnapshot(entry *data.SnapshotBrowserEntry, recursive bool, dependantClones bool) (err error) {
	snapshot := entry.Snapshot
	return snapshot.Destroy(recursive, dependantClones)
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
		uiutil.TableComponentShortcutPageUp,
		uiutil.TableComponentShortcutPageDown,
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
