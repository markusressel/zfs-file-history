package snapshot_browser

import (
	"context"
	"fmt"
	"os"
	"slices"
	"sort"
	"strings"
	"time"
	"zfs-file-history/internal/data"
	"zfs-file-history/internal/data/diff_state"
	"zfs-file-history/internal/folder_listing"
	"zfs-file-history/internal/logging"
	"zfs-file-history/internal/state"
	"zfs-file-history/internal/ui/dialog"
	"zfs-file-history/internal/ui/shortcut_helper"
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
	// usedScale and writtenScale color the sizes by how big they are compared to all snapshots of the dataset
	// (also the ones hidden by the filter, so filtering does not change the colors), see updateSizeScales
	usedScale    uiutil.MagnitudeScale
	writtenScale uiutil.MagnitudeScale
	// historyTarget returns the file or folder whose history can be opened at the selected snapshot (h), or nil.
	// Not set on pages without a file browser.
	historyTarget func() *data.FileBrowserEntry

	selectedSnapshotMemory *uiutil.SelectionMemory[data.SnapshotBrowserEntry]

	isRestoringSelection bool

	selectLatestOnNextLoad bool

	diffLoader *uiutil.DebouncedLoader

	// the folder compared with its snapshots, see folder_changes.go
	workingCopyPath        string
	workingCopy            folder_listing.Listing
	folderChangesRequired  bool
	folderChangesInput     *folderChangesInput
	folderChanges          *folderChanges
	folderChangesDebouncer *uiutil.Debouncer
	cancelFolderChanges    func()

	// where new versions of the selected entry start, see versions.go
	entryVersions *entryVersionStarts
	// onlyChanges hides the snapshots in which nothing changed (v)
	onlyChanges bool
	// layoutStateKey is where the columns of the page are saved, see UseColumnLayout
	layoutStateKey string
}

type snapshotLoadResult struct {
	dataset   *zfs.Dataset
	snapshots []*zfs.Snapshot
}

var (
	columnName = &table.Column{
		Id:        0,
		Key:       "name",
		Title:     "Name",
		Alignment: tview.AlignLeft,
	}
	columnDate = &table.Column{
		Id:        1,
		Key:       "creation",
		Title:     "Creation",
		Alignment: tview.AlignLeft,
	}
	columnDiff = &table.Column{
		Id:        2,
		Key:       "diff",
		Title:     "Diff",
		Alignment: tview.AlignCenter,
	}
	columnUsed = &table.Column{
		Id:        3,
		Key:       "used",
		Title:     "Used",
		Alignment: tview.AlignCenter,
	}
	columnRefer = &table.Column{
		Id:        4,
		Key:       "referenced",
		Title:     "Refer",
		Alignment: tview.AlignCenter,
	}
	columnRatio = &table.Column{
		Id:        5,
		Key:       "compressRatio",
		Title:     "Ratio",
		Alignment: tview.AlignCenter,
	}
	columnClones = &table.Column{
		Id:        6,
		Key:       "clones",
		Title:     "Clones",
		Alignment: tview.AlignCenter,
	}

	// columnHolds is the number of holds, see zfs.ListHolds
	columnHolds = &table.Column{
		Id:        7,
		Key:       "holds",
		Title:     "Holds",
		Alignment: tview.AlignCenter,
	}

	// columnWritten is the space written since the previous snapshot: 0 for snapshots in which nothing changed
	columnWritten = &table.Column{
		Id:        8,
		Key:       "written",
		Title:     "Written",
		Alignment: tview.AlignCenter,
	}

	// the columns about the selected entry and the folder of the browser (rather than the whole dataset)

	// columnSize is the size of the selected entry in the snapshot
	columnSize = &table.Column{
		Id:        9,
		Key:       "entrySize",
		Title:     "Size",
		Alignment: tview.AlignRight,
	}
	// columnModified is the modification time of the selected entry in the snapshot: which version it holds
	columnModified = &table.Column{
		Id:        10,
		Key:       "entryModified",
		Title:     "Modified",
		Alignment: tview.AlignLeft,
	}
	// columnVsNow is the number of entries of the folder that differ between the snapshot and now
	columnVsNow = &table.Column{
		Id:        11,
		Key:       "folderVsNow",
		Title:     "vs now",
		Alignment: tview.AlignLeft,
	}
	// columnChanges is the number of entries of the folder that changed since the previous snapshot
	columnChanges = &table.Column{
		Id:        12,
		Key:       "folderChanges",
		Title:     "Changes",
		Alignment: tview.AlignLeft,
	}

	tableColumns = []*table.Column{
		columnName, columnDate, columnDiff, columnSize, columnModified, columnVsNow, columnChanges,
		columnUsed, columnWritten, columnRefer, columnRatio, columnClones, columnHolds,
	}
)

// ColumnLayout is where the snapshot browser of a page saves its columns, and its default columns.
type ColumnLayout struct {
	stateKey string
	columns  []*table.Column
}

var (
	// FilesLayout is the layout on the files page: about the path that is shown
	FilesLayout = ColumnLayout{
		stateKey: "snapshotBrowser.files",
		columns:  []*table.Column{columnName, columnDiff, columnDate, columnSize, columnModified, columnChanges, columnHolds},
	}
	// DatasetsLayout is the layout on the dataset page: about the whole dataset
	DatasetsLayout = ColumnLayout{
		stateKey: "snapshotBrowser.datasets",
		columns:  []*table.Column{columnName, columnDate, columnUsed, columnWritten, columnRefer, columnHolds},
	}
)

// legacyLayoutStateKey is where both pages saved their (shared) layout before they had their own.
const legacyLayoutStateKey = "snapshotBrowser"

// UseColumnLayout shows the columns of the layout, or the ones saved for it, and saves the changes. A layout saved
// before the pages had their own is taken over once. Must be called on the UI thread, once.
func (snapshotBrowser *SnapshotBrowserComponent) UseColumnLayout(layout ColumnLayout) {
	if store := state.Current; store != nil {
		if _, saved := store.TableLayout(layout.stateKey); !saved {
			if legacy, ok := store.TableLayout(legacyLayoutStateKey); ok {
				store.SetTableLayout(layout.stateKey, legacy)
			}
		}
	}
	snapshotBrowser.tableContainer.SetActiveColumns(layout.columns)
	snapshotBrowser.tableContainer.BindColumnLayout(state.Current, layout.stateKey, tableColumns)
	snapshotBrowser.layoutStateKey = layout.stateKey
	snapshotBrowser.loadOnlyChanges()
}

func NewSnapshotBrowser(application *tview.Application) *SnapshotBrowserComponent {
	snapshotBrowser := &SnapshotBrowserComponent{
		Events:                 *util.NewEmitter[Event](),
		application:            application,
		currentSnapshots:       []*zfs.Snapshot{},
		selectedSnapshotMemory: uiutil.NewSelectionMemory[data.SnapshotBrowserEntry](),
		layoutStateKey:         legacyLayoutStateKey,
	}
	snapshotBrowser.folderChangesDebouncer = uiutil.NewDebouncer(application, folderChangesDelay)

	snapshotBrowser.diffLoader = uiutil.NewDebouncedLoader(application, func() {
		currentSelection := snapshotBrowser.GetSelection()
		snapshotBrowser.isRestoringSelection = true
		snapshotBrowser.tableContainer.SetData(snapshotBrowser.tableContainer.GetAllEntries())
		if currentSelection != nil {
			snapshotBrowser.tableContainer.Select(currentSelection)
		}
		snapshotBrowser.isRestoringSelection = false
		snapshotBrowser.updateTitleAndFooter()
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
			} else if key == tcell.KeyRune && event.Rune() == 'v' {
				snapshotBrowser.toggleOnlyChanges()
				return nil
			} else if key == tcell.KeyRune && event.Rune() == 'h' && !snapshotBrowser.HasMultiSelection() {
				if target := snapshotBrowser.getHistoryTarget(); target != nil {
					snapshotBrowser.emit(RequestHistoryEvent{Entry: target, Snapshot: snapshotBrowser.GetSelection()})
					return nil
				}
			} else if event.Rune() == 'd' || key == tcell.KeyDelete {
				currentSelection := snapshotBrowser.GetSelection()
				if currentSelection != nil {
					snapshotBrowser.openDeleteDialog(currentSelection)
				}
				return nil
			}
		}
		return event
	})

	snapshotBrowser.tableContainer.SetFilterFunc(snapshotMatchesFilter)
	snapshotBrowser.tableContainer.SetFilterChangedCallback(snapshotBrowser.updateTitleAndFooter)
	snapshotBrowser.tableContainer.SetColumnSpec(tableColumns, columnDate, true)
	// until the page sets its own, see UseColumnLayout
	snapshotBrowser.tableContainer.SetActiveColumns(FilesLayout.columns)
	// the folder changes are only computed while they are shown
	snapshotBrowser.tableContainer.SetColumnLayoutChangedCallback(func(table.ColumnLayout) { snapshotBrowser.updateFolderChanges() })
	snapshotBrowser.tableContainer.SetSelectionChangedCallback(func(entry *data.SnapshotBrowserEntry) {
		if snapshotBrowser.isRestoringSelection {
			return
		}
		snapshotBrowser.rememberSelectionForDataset(entry)
		snapshotBrowser.updateTitleAndFooter()
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

func (snapshotBrowser *SnapshotBrowserComponent) startAsyncDiffCalculation() {
	snapshots := snapshotBrowser.currentSnapshots
	fileEntry := snapshotBrowser.currentFileEntry
	folderPath := snapshotBrowser.path
	// before the cells are rendered below
	snapshotBrowser.updateSizeScales(snapshots)
	// does nothing if the folder, its snapshots and its entries are the same as before
	snapshotBrowser.updateFolderChanges()

	if len(snapshots) == 0 {
		snapshotBrowser.tableContainer.SetData([]*data.SnapshotBrowserEntry{})
		snapshotBrowser.updateTitleAndFooter()
		snapshotBrowser.emit(PathVersionsLoaded{FolderPath: folderPath, Entry: fileEntry})
		return
	}

	ctx, seq := snapshotBrowser.diffLoader.Start()

	// all entries, including the ones hidden by the filter
	currentEntries := snapshotBrowser.tableContainer.GetAllEntries()
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
		snapshotBrowser.updateTitleAndFooter()
	}

	// also calculate the diffs of entries hidden by the filter, so they are correct once the filter changes
	entriesToProcess := slices.Clone(snapshotBrowser.tableContainer.GetAllEntries())

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
			// info is the selected entry in the snapshot, nil if it does not contain it
			info os.FileInfo
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
					res.entry.HasEntryInfo = filePath != ""
					res.entry.EntryInfo = res.info
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

		// the versions of the folder and the entry, for the path overview (see PathVersionsLoaded)
		folderVersions := make([]data.PathVersion, 0, len(entriesToProcess))
		var entryVersions []data.PathVersion

		for i, entry := range entriesToProcess {
			if ctx.Err() != nil {
				return
			}

			diffState := diff_state.Unknown
			var info os.FileInfo
			if filePath != "" {
				diffState, info = entry.Snapshot.DiffStateAndInfo(filePath)
				entryVersions = append(entryVersions, data.PathVersion{Snapshot: entry.Snapshot, Info: info})
			}
			if folderPath != "" {
				folderInfo, err := os.Lstat(entry.Snapshot.GetSnapshotPath(folderPath))
				if err != nil {
					folderInfo = nil
				}
				folderVersions = append(folderVersions, data.PathVersion{Snapshot: entry.Snapshot, Info: folderInfo})
			}

			batch = append(batch, diffResult{entry: entry, state: diffState, info: info})

			now := time.Now()
			isLast := i == len(entriesToProcess)-1
			if isLast {
				// drawn together with the versions below
				pushBatch(false)
			} else if now.Sub(lastDrawTime) > 50*time.Millisecond {
				// Draw at most once every 50ms to prevent SSH connection flooding
				pushBatch(true)
				lastDrawTime = now
			} else if len(batch) >= 10 {
				pushBatch(false)
			}
		}

		loaded := PathVersionsLoaded{FolderPath: folderPath, Folder: folderVersions, Entry: fileEntry, EntryVersions: entryVersions}
		if len(entriesToProcess) > 0 && entriesToProcess[0].Snapshot.ParentDataset != nil {
			// may read a libzfs property, so not on the UI thread
			dataset := entriesToProcess[0].Snapshot.ParentDataset
			loaded.DatasetName, loaded.DatasetPath = dataset.GetName(), dataset.Path
		}
		var versions *entryVersionStarts
		if filePath != "" {
			versions = &entryVersionStarts{path: filePath, newVersion: data.NewVersions(entryVersions)}
		}
		snapshotBrowser.application.QueueUpdateDraw(func() {
			if snapshotBrowser.diffLoader.IsCurrentSequence(seq) {
				snapshotBrowser.setEntryVersions(versions)
				snapshotBrowser.emit(loaded)
			}
		})
	}()
}

// updateSizeScales computes the scales of the size columns from the given snapshots. Runs on the UI thread.
func (snapshotBrowser *SnapshotBrowserComponent) updateSizeScales(snapshots []*zfs.Snapshot) {
	used := make([]uint64, len(snapshots))
	written := make([]uint64, len(snapshots))
	for i, snapshot := range snapshots {
		used[i] = snapshot.Properties.Used
		written[i] = snapshot.Properties.Written
	}
	snapshotBrowser.usedScale = uiutil.NewMagnitudeScale(used)
	snapshotBrowser.writtenScale = uiutil.NewMagnitudeScale(written)
}

func (snapshotBrowser *SnapshotBrowserComponent) Focus() {
	snapshotBrowser.application.SetFocus(snapshotBrowser.container)
}

func (snapshotBrowser *SnapshotBrowserComponent) HasFocus() bool {
	return snapshotBrowser.container.HasFocus()
}

// updateTitleAndFooter shows the number of (matching) snapshots in the bottom border.
// The table shows an active filter in the title. Runs on the UI thread.
func (snapshotBrowser *SnapshotBrowserComponent) updateTitleAndFooter() {
	snapshotBrowser.tableContainer.SetTitle("Snapshots")
	snapshotBrowser.tableContainer.SetFooter(formatFooter(
		len(snapshotBrowser.tableContainer.GetEntries()),
		len(snapshotBrowser.tableContainer.GetAllEntries()),
		snapshotBrowser.tableContainer.IsFiltered(),
	))
}

// formatFooter returns e.g. "48 snapshots", or "3 of 48 snapshots" while a filter is active.
func formatFooter(matchingCount int, totalCount int, filterActive bool) string {
	noun := uiutil.Plural(totalCount, "snapshot", "snapshots")
	if filterActive {
		return fmt.Sprintf("%d of %d %s", matchingCount, totalCount, noun)
	}
	if totalCount == 0 {
		return ""
	}
	return fmt.Sprintf("%d %s", totalCount, noun)
}

// snapshotMatchesFilter matches the snapshot name against the filter as a glob, see table.MatchesGlob.
func snapshotMatchesFilter(entry *data.SnapshotBrowserEntry, filterText string) bool {
	return table.MatchesGlob(entry.Snapshot.Name, filterText)
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

// GetAllEntries returns all snapshot entries, including the ones hidden by the filter.
func (snapshotBrowser *SnapshotBrowserComponent) GetAllEntries() []*data.SnapshotBrowserEntry {
	return snapshotBrowser.tableContainer.GetAllEntries()
}

// GetEntries returns the displayed snapshot entries, i.e. the ones that match the filter.
func (snapshotBrowser *SnapshotBrowserComponent) GetEntries() []*data.SnapshotBrowserEntry {
	return snapshotBrowser.tableContainer.GetEntries()
}

func (snapshotBrowser *SnapshotBrowserComponent) GetCurrentSnapshots() []*zfs.Snapshot {
	return snapshotBrowser.currentSnapshots
}

func (snapshotBrowser *SnapshotBrowserComponent) selectHeader() {
	snapshotBrowser.tableContainer.SelectHeader()
}

// SetHistoryTarget enables opening the history of a file or folder at the selected snapshot (h, and in the action
// dialog), see RequestHistoryEvent. target returns the file or folder, or nil if there is none.
func (snapshotBrowser *SnapshotBrowserComponent) SetHistoryTarget(target func() *data.FileBrowserEntry) {
	snapshotBrowser.historyTarget = target
}

// getHistoryTarget returns the file or folder whose history can be opened at the selected snapshot, or nil.
func (snapshotBrowser *SnapshotBrowserComponent) getHistoryTarget() *data.FileBrowserEntry {
	if snapshotBrowser.historyTarget == nil {
		return nil
	}
	return snapshotBrowser.historyTarget()
}

func (snapshotBrowser *SnapshotBrowserComponent) openActionDialog(selection *data.SnapshotBrowserEntry) {
	if snapshotBrowser.GetSelection() == nil {
		return
	}
	// captured now, the selection of the file browser may change while the dialog is open
	historyTarget := snapshotBrowser.getHistoryTarget()
	historyTargetName := ""
	if historyTarget != nil {
		historyTargetName = historyTarget.Name
	}

	var createdName string
	// destroying asks for confirmation first, with the result of a dry run
	var destroy *destroyRequest
	var destroyPreviewResult *zfs.DestroyPreview
	var holdResultValue *holdResult
	selectedNames := []string{selection.Snapshot.FullName}

	asyncWork := func(d *dialog.SelectionDialog, action dialog.DialogActionId) error {
		switch action {
		case dialog.SnapshotDialogHoldSnapshotActionId, dialog.SnapshotDialogReleaseSnapshotActionId:
			result, err := holdOrRelease(selectedNames, action == dialog.SnapshotDialogHoldSnapshotActionId)
			holdResultValue = result
			// shown in onComplete, together with the reload
			return err
		case dialog.SnapshotDialogCreateSnapshotActionId:
			name, err := snapshotBrowser.createSnapshot(selection)
			createdName = name
			return err
		case dialog.SnapshotDialogDestroySnapshotActionId:
			destroy = &destroyRequest{entries: []*data.SnapshotBrowserEntry{selection}}
			preview, err := destroy.preview()
			destroyPreviewResult = preview
			return err
		case dialog.SnapshotDialogDestroySnapshotRecursivelyActionId:
			destroy = &destroyRequest{entries: []*data.SnapshotBrowserEntry{selection}, recursive: true, dependantClones: true}
			preview, err := destroy.preview()
			destroyPreviewResult = preview
			return err
		}
		return nil
	}

	onComplete := func(d *dialog.SelectionDialog, option *dialog.DialogOption, err error) {
		d.Close() // Dismiss selection menu

		if option.Id == dialog.SnapshotDialogHoldSnapshotActionId || option.Id == dialog.SnapshotDialogReleaseSnapshotActionId {
			snapshotBrowser.showHoldResult(holdResultValue, option.Id == dialog.SnapshotDialogHoldSnapshotActionId, err, d.RetryFunc(option))
			return
		}

		if err != nil {
			logging.Error("Action failed: %s", err.Error())
			errDialog := dialog.NewErrorDialogWithRetry(snapshotBrowser.application, "Operation Failed", err, d.RetryFunc(option))
			snapshotBrowser.showDialog(errDialog, nil)
			return
		}

		// Handle downstream states depending on what succeeded
		switch option.Id {
		case dialog.SnapshotDialogShowHistoryActionId:
			snapshotBrowser.emit(RequestHistoryEvent{Entry: historyTarget, Snapshot: selection})
			return
		case dialog.SnapshotDialogCloneSnapshotActionId:
			// asks for the name first, the clone itself is created afterwards
			snapshotBrowser.openCloneDialog(selection)
			return
		case dialog.SnapshotDialogCreateSnapshotActionId:
			snapshotBrowser.selectLatestOnNextLoad = true

			successDialog := dialog.NewSuccessDialog(snapshotBrowser.application, "Snapshot Created", fmt.Sprintf("Snapshot '%s' created successfully.", createdName))
			snapshotBrowser.showDialog(successDialog, nil)

		case dialog.SnapshotDialogDestroySnapshotActionId, dialog.SnapshotDialogDestroySnapshotRecursivelyActionId:
			// nothing was destroyed yet, the confirmation does that
			snapshotBrowser.showDestroyConfirmation(destroy, destroyPreviewResult)
			return
		}

		snapshotBrowser.Refresh(true)
	}

	actionDialog := dialog.NewSnapshotActionDialog(snapshotBrowser.application, selection, historyTargetName, asyncWork, onComplete)
	snapshotBrowser.showDialog(actionDialog, nil)
}

// cloneSnapshot creates a new dataset from a snapshot, replaceable in tests.
var cloneSnapshot = func(snapshot *zfs.Snapshot, targetName string) error {
	return snapshot.Clone(targetName)
}

// openCloneDialog asks for the name of the new dataset and creates the clone. Must be called on the UI thread.
func (snapshotBrowser *SnapshotBrowserComponent) openCloneDialog(selection *data.SnapshotBrowserEntry) {
	snapshot := selection.Snapshot
	cloneDialog := dialog.NewTextInputDialog(
		snapshotBrowser.application,
		"CloneSnapshotDialog",
		" 🧬 Clone Snapshot ",
		fmt.Sprintf("Create a new dataset from snapshot '%s'. Name of the new dataset (in the same pool):", snapshot.FullName),
		snapshot.SuggestCloneName(),
		func(targetName string) {
			snapshotBrowser.cloneInBackground(snapshot, targetName)
		},
	)
	snapshotBrowser.showDialog(cloneDialog, nil)
}

// cloneInBackground creates the clone in the background and shows the result. Must be called on the UI thread.
func (snapshotBrowser *SnapshotBrowserComponent) cloneInBackground(snapshot *zfs.Snapshot, targetName string) {
	go func() {
		err := cloneSnapshot(snapshot, targetName)

		snapshotBrowser.application.QueueUpdateDraw(func() {
			if err != nil {
				logging.Error("Failed to clone snapshot %s to %s: %s", snapshot.FullName, targetName, err.Error())
				retry := func() { snapshotBrowser.cloneInBackground(snapshot, targetName) }
				snapshotBrowser.showDialog(dialog.NewErrorDialogWithRetry(snapshotBrowser.application, "Clone Failed", err, retry), nil)
			} else {
				snapshotBrowser.showDialog(dialog.NewSuccessDialog(
					snapshotBrowser.application,
					"Snapshot Cloned",
					fmt.Sprintf("Created dataset '%s' from snapshot '%s'.", targetName, snapshot.FullName),
				), nil)
			}
			// the clone may exist even if an error occurred (e.g. it could not be mounted)
			zfs.RefreshZfsData()
		})
	}()
}

func (snapshotBrowser *SnapshotBrowserComponent) openMultiActionDialog(entries []*data.SnapshotBrowserEntry) {
	if len(entries) <= 0 {
		return
	}

	// destroying asks for confirmation first, with the result of a dry run
	var destroy *destroyRequest
	var destroyPreviewResult *zfs.DestroyPreview

	var holdResultValue *holdResult
	selectedNames := snapshotFullNames(entries)

	asyncWork := func(d *dialog.SelectionDialog, action dialog.DialogActionId) error {
		switch action {
		case dialog.MultiSnapshotDialogHoldSnapshotsActionId, dialog.MultiSnapshotDialogReleaseSnapshotsActionId:
			result, err := holdOrRelease(selectedNames, action == dialog.MultiSnapshotDialogHoldSnapshotsActionId)
			holdResultValue = result
			// shown in onComplete, together with the reload
			return err
		case dialog.MultiSnapshotDialogDestroySnapshotActionId:
			destroy = &destroyRequest{entries: entries}
		case dialog.MultiSnapshotDialogDestroySnapshotRecursivelyActionId:
			destroy = &destroyRequest{entries: entries, recursive: true, dependantClones: true}
		default:
			return nil
		}
		preview, err := destroy.preview()
		destroyPreviewResult = preview
		return err
	}

	onComplete := func(d *dialog.SelectionDialog, option *dialog.DialogOption, err error) {
		d.Close()

		if option.Id == dialog.MultiSnapshotDialogHoldSnapshotsActionId || option.Id == dialog.MultiSnapshotDialogReleaseSnapshotsActionId {
			snapshotBrowser.showHoldResult(holdResultValue, option.Id == dialog.MultiSnapshotDialogHoldSnapshotsActionId, err, d.RetryFunc(option))
			return
		}

		if err != nil {
			logging.Error("Cannot destroy snapshots: %s", err.Error())
			snapshotBrowser.showDialog(dialog.NewErrorDialogWithRetry(snapshotBrowser.application, "Cannot Destroy", err, d.RetryFunc(option)), nil)
			return
		}

		switch option.Id {
		case dialog.MultiSnapshotDialogClearSelectionActionId:
			snapshotBrowser.ClearMultiSelection()
		case dialog.MultiSnapshotDialogDestroySnapshotActionId, dialog.MultiSnapshotDialogDestroySnapshotRecursivelyActionId:
			// nothing was destroyed yet, the confirmation does that
			snapshotBrowser.showDestroyConfirmation(destroy, destroyPreviewResult)
		}
	}

	actionDialog := dialog.NewMultiSnapshotActionDialog(snapshotBrowser.application, entries, asyncWork, onComplete)
	snapshotBrowser.showDialog(actionDialog, nil)
}

// openDeleteDialog asks for confirmation to destroy the given snapshot, with the result of a dry run.
// Must be called on the UI thread.
func (snapshotBrowser *SnapshotBrowserComponent) openDeleteDialog(selection *data.SnapshotBrowserEntry) {
	if selection == nil {
		return
	}

	snapshotBrowser.previewAndConfirmDestroy(&destroyRequest{entries: []*data.SnapshotBrowserEntry{selection}})
}

// previewAndConfirmDestroy does the dry run of request in the background and then asks for confirmation.
// Must be called on the UI thread.
func (snapshotBrowser *SnapshotBrowserComponent) previewAndConfirmDestroy(request *destroyRequest) {
	go func() {
		preview, err := request.preview()
		snapshotBrowser.application.QueueUpdateDraw(func() {
			if err != nil {
				logging.Error("Cannot destroy snapshots: %s", err.Error())
				retry := func() { snapshotBrowser.previewAndConfirmDestroy(request) }
				snapshotBrowser.showDialog(dialog.NewErrorDialogWithRetry(snapshotBrowser.application, "Cannot Destroy", err, retry), nil)
				return
			}
			snapshotBrowser.showDestroyConfirmation(request, preview)
		})
	}()
}

// destroyRequest describes which snapshots to destroy and how, captured on the UI thread.
type destroyRequest struct {
	entries         []*data.SnapshotBrowserEntry
	recursive       bool
	dependantClones bool
}

func (r *destroyRequest) snapshots() []*zfs.Snapshot {
	result := make([]*zfs.Snapshot, 0, len(r.entries))
	for _, entry := range r.entries {
		result = append(result, entry.Snapshot)
	}
	return result
}

// preview does a dry run of the destroy. Runs in the background.
// Held snapshots are reported as an error, as the dry run does not check holds, but the destroy would fail.
func (r *destroyRequest) preview() (*zfs.DestroyPreview, error) {
	holds, err := listHolds(snapshotFullNames(r.entries), r.recursive)
	if err != nil {
		return nil, err
	}
	if len(holds) > 0 {
		return nil, heldSnapshotsError(holds)
	}
	return previewDestroySnapshots(r.snapshots(), r.recursive, r.dependantClones)
}

// previewDestroySnapshots and destroySnapshots are replaceable in tests, so tests never destroy anything.
var (
	previewDestroySnapshots = zfs.PreviewDestroySnapshots
	destroySnapshots        = func(snapshots []*zfs.Snapshot, recursive bool, dependantClones bool) error {
		for _, snapshot := range snapshots {
			if err := snapshot.Destroy(recursive, dependantClones); err != nil {
				return err
			}
		}
		return nil
	}
)

// maxListedDestroyed is the maximum number of destroyed snapshots / datasets listed in the confirmation.
const maxListedDestroyed = 10

// formatDestroyDescription describes what a destroy would do, based on its dry run.
func formatDestroyDescription(preview *zfs.DestroyPreview) string {
	var description strings.Builder
	fmt.Fprintf(&description, "This frees %s and cannot be undone.\n\n", uiutil.HumanizedBytes(preview.Reclaim))
	fmt.Fprintf(&description, "Will be destroyed (%d):", len(preview.Destroyed))
	for i, name := range preview.Destroyed {
		if i == maxListedDestroyed {
			fmt.Fprintf(&description, "\n  … and %d more", len(preview.Destroyed)-maxListedDestroyed)
			break
		}
		fmt.Fprintf(&description, "\n  %s", name)
	}
	return description.String()
}

// showDestroyConfirmation shows what would be destroyed and destroys it on confirmation, exactly as previewed.
// Must be called on the UI thread.
func (snapshotBrowser *SnapshotBrowserComponent) showDestroyConfirmation(request *destroyRequest, preview *zfs.DestroyPreview) {
	snapshots := request.snapshots()

	asyncWork := func(d *dialog.SelectionDialog, action dialog.DialogActionId) error {
		if action != dialog.DestroySnapshotsDialogDestroyActionId {
			return nil
		}
		return destroySnapshots(snapshots, request.recursive, request.dependantClones)
	}

	onComplete := func(d *dialog.SelectionDialog, option *dialog.DialogOption, err error) {
		d.Close()

		if err != nil {
			logging.Error("Failed to destroy snapshots: %s", err.Error())
			// a retry asks for confirmation again, with a new dry run (some snapshots may be gone already)
			retry := func() { snapshotBrowser.previewAndConfirmDestroy(request) }
			snapshotBrowser.showDialog(dialog.NewErrorDialogWithRetry(snapshotBrowser.application, "Destroy Failed", err, retry), nil)
			// some snapshots may have been destroyed already
			snapshotBrowser.ClearMultiSelection()
			snapshotBrowser.Refresh(true)
			return
		}

		message := fmt.Sprintf("Destroyed %d %s, freed %s.",
			len(preview.Destroyed), uiutil.Plural(len(preview.Destroyed), "snapshot", "snapshots"), uiutil.HumanizedBytes(preview.Reclaim))
		snapshotBrowser.showDialog(dialog.NewSuccessDialog(snapshotBrowser.application, "Snapshots Destroyed", message), nil)
		snapshotBrowser.ClearMultiSelection()
		snapshotBrowser.Refresh(true)
	}

	confirmation := dialog.NewDestroySnapshotsDialog(snapshotBrowser.application, formatDestroyDescription(preview), asyncWork, onComplete)
	snapshotBrowser.showDialog(confirmation, nil)
}

func (snapshotBrowser *SnapshotBrowserComponent) showDialog(d dialog.Dialog, onClosed func()) {
	dialog.ShowDialogOnPages(snapshotBrowser.application, snapshotBrowser.container.Pages, d, onClosed)
}

func (snapshotBrowser *SnapshotBrowserComponent) openColumnSelectionDialog() {
	d := dialog.NewTableColumnSelectionDialog(snapshotBrowser.application, "Configure Snapshot Columns", tableColumns, snapshotBrowser.tableContainer)
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

// SelectLatestOnNextLoad selects the latest snapshot after the next (re)load, e.g. after creating a snapshot.
// Must be called on the UI thread.
func (snapshotBrowser *SnapshotBrowserComponent) SelectLatestOnNextLoad() {
	snapshotBrowser.selectLatestOnNextLoad = true
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

func (snapshotBrowser *SnapshotBrowserComponent) emit(event Event) {
	snapshotBrowser.Events.Emit(event)
}

func (snapshotBrowser *SnapshotBrowserComponent) HasMultiSelection() bool {
	return snapshotBrowser.tableContainer.HasMultiSelection()
}

func (snapshotBrowser *SnapshotBrowserComponent) ClearMultiSelection() {
	snapshotBrowser.tableContainer.ClearMultiSelection()
}

func (snapshotBrowser *SnapshotBrowserComponent) GetShortcutMap() []shortcut_helper.ShortcutEntry {
	shortcutMap := []shortcut_helper.ShortcutEntry{
		uiutil.TableComponentShortcutMove,
		uiutil.TableComponentShortcutColumns,
		uiutil.TableComponentShortcutFilter,
		snapshotBrowser.onlyChangesShortcut(),
	}

	if snapshotBrowser.GetSelection() != nil {
		shortcutMap = append(shortcutMap,
			uiutil.TableComponentShortcutActions,
			uiutil.TableComponentShortcutDelete,
		)
		if snapshotBrowser.getHistoryTarget() != nil && !snapshotBrowser.HasMultiSelection() {
			shortcutMap = append(shortcutMap, shortcut_helper.ShortcutEntry{KeyCombo: []string{"h"}, Name: "History at snapshot"})
		}
	} else {
		shortcutMap = append(shortcutMap,
			uiutil.TableComponentShortcutFlipColumnDirection,
			uiutil.TableComponentShortcutCycleSortColumnLeft,
			uiutil.TableComponentShortcutCycleSortColumnRight,
		)
	}

	return shortcutMap
}
