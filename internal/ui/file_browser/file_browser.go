package file_browser

import (
	"context"
	"fmt"
	"os"
	path2 "path"
	"path/filepath"
	"slices"
	"strings"
	"time"
	"zfs-file-history/internal/configuration"
	"zfs-file-history/internal/data"
	"zfs-file-history/internal/data/diff_state"
	"zfs-file-history/internal/logging"
	"zfs-file-history/internal/state"
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

var (
	columnSize = &table.Column{
		Id:        0,
		Key:       "size",
		Title:     "Size",
		Alignment: tview.AlignLeft,
	}
	columnDateTime = &table.Column{
		Id:        1,
		Key:       "modified",
		Title:     "Date/Time",
		Alignment: tview.AlignLeft,
	}
	columnType = &table.Column{
		Id:        2,
		Key:       "type",
		Title:     "Type",
		Alignment: tview.AlignCenter,
	}
	columnDiff = &table.Column{
		Id:        3,
		Key:       "diff",
		Title:     "Diff",
		Alignment: tview.AlignCenter,
	}
	columnPermissions = &table.Column{
		Id:        4,
		Key:       "permissions",
		Title:     "Perm",
		Alignment: tview.AlignLeft,
	}
	columnUID = &table.Column{
		Id:        5,
		Key:       "uid",
		Title:     "UID",
		Alignment: tview.AlignLeft,
	}
	columnGID = &table.Column{
		Id:        6,
		Key:       "gid",
		Title:     "GID",
		Alignment: tview.AlignLeft,
	}
	columnName = &table.Column{
		Id:        7,
		Key:       "name",
		Title:     "Name",
		Alignment: tview.AlignLeft,
	}

	tableColumns = []*table.Column{
		columnPermissions,
		columnUID,
		columnGID,
		columnSize,
		columnDateTime,
		columnName,
		columnType,
		columnDiff,
	}

	initialActiveTableColumns = []*table.Column{
		columnSize,
		columnDateTime,
		columnName,
		columnType,
		columnDiff,
	}
)

type FileBrowserComponent struct {
	Events *util.Emitter[Event]

	path string
	// entriesPath is the path the displayed entries belong to. It differs from path while a new path is loading.
	entriesPath string

	currentSnapshot *data.SnapshotBrowserEntry

	application *tview.Application
	layout      *tview.Pages

	tableContainer *table.RowSelectionTable[data.FileBrowserEntry]

	selectionMemory *uiutil.SelectionMemory[data.FileBrowserEntry]
	fileWatcher     *util.FileWatcher

	diffLoader    *uiutil.DebouncedLoader
	refreshLoader *uiutil.DebouncedLoader
}

func NewFileBrowser(application *tview.Application) *FileBrowserComponent {
	fileBrowser := &FileBrowserComponent{
		Events: util.NewEmitter[Event](),

		application: application,

		selectionMemory: uiutil.NewSelectionMemory[data.FileBrowserEntry](),
	}

	fileBrowser.diffLoader = uiutil.NewDebouncedLoader(application, func() {
		for _, entry := range fileBrowser.tableContainer.GetEntries() {
			if entry != nil && entry.IsLoading {
				fileBrowser.tableContainer.UpdateEntry(entry)
			}
		}
	})

	fileBrowser.refreshLoader = uiutil.NewDebouncedLoader(application, func() {})

	fileBrowser.tableContainer = fileBrowser.createFileBrowserTable(application)

	fileBrowser.createLayout()
	fileBrowser.setupTable()

	return fileBrowser
}

func (fileBrowser *FileBrowserComponent) GetPath() string {
	return fileBrowser.path
}

func (fileBrowser *FileBrowserComponent) createLayout() {
	fileBrowser.layout = tview.NewPages().
		AddPage("file-browser", fileBrowser.tableContainer.GetLayout(), true, true)
}

func (fileBrowser *FileBrowserComponent) setupTable() {
	fileBrowser.tableContainer.SetFilterFunc(fileMatchesFilter)
	fileBrowser.tableContainer.SetFilterChangedCallback(fileBrowser.updateFooter)
	fileBrowser.tableContainer.SetColumnSpec(tableColumns, columnType, true)
	fileBrowser.tableContainer.SetActiveColumns(initialActiveTableColumns)
	fileBrowser.tableContainer.BindColumnLayout(state.Current, "fileBrowser", tableColumns)
	fileBrowser.tableContainer.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		key := event.Key()
		if key == tcell.KeyF2 {
			fileBrowser.openColumnSelectionDialog()
			return nil
		}

		if event.Modifiers()&tcell.ModAlt != 0 {
			switch {
			case key == tcell.KeyUp:
				fileBrowser.goUp()
				return nil
			default:
				return nil
			}
		}

		if event.Modifiers()&tcell.ModCtrl != 0 {
			switch {
			case event.Rune() == 'r':
				openRestoreDialogOnCurrentSelection(fileBrowser)
			case event.Rune() == 'd':
				openDeleteDialogOnCurrentSelection(fileBrowser)
			}

			return nil
		}

		if fileBrowser.GetSelection() != nil {
			switch {
			case key == tcell.KeyRight:
				fileBrowser.enterFileEntry(fileBrowser.GetSelection())
				return nil
			case key == tcell.KeyEnter:
				fileBrowser.openActionDialog(fileBrowser.GetSelection())
				return nil
			case key == tcell.KeyDelete:
				openDeleteDialogOnCurrentSelection(fileBrowser)
				return nil
			}
		}
		if key == tcell.KeyRune && event.Rune() == 'h' {
			if entry := fileBrowser.historyEntry(); entry != nil {
				fileBrowser.emit(RequestFileHistoryEvent{FileEntry: entry})
				return nil
			}
		}
		if key == tcell.KeyLeft && (fileBrowser.tableContainer.GetSelectedEntry() != nil || fileBrowser.isEmpty()) {
			fileBrowser.goUp()
			return nil
		}
		return event
	})
	fileBrowser.tableContainer.SetSelectionChangedCallback(func(selectedEntry *data.FileBrowserEntry) {
		fileBrowser.rememberSelectionInfoForCurrentPath()
		fileBrowser.Events.Emit(SelectedTableEntryChangedEvent{selectedEntry})
	})
}

func (fileBrowser *FileBrowserComponent) emit(event Event) {
	fileBrowser.Events.Emit(event)
}

func openDeleteDialogOnCurrentSelection(fileBrowser *FileBrowserComponent) {
	currentSelection := fileBrowser.GetSelection()
	if currentSelection != nil && currentSelection.HasReal() {
		fileBrowser.openDeleteDialog(currentSelection)
	}
}

func openRestoreDialogOnCurrentSelection(fileBrowser *FileBrowserComponent) {
	currentSelection := fileBrowser.GetSelection()
	if currentSelection != nil && currentSelection.HasSnapshot() && currentSelection.DiffState != diff_state.Equal {
		fileBrowser.openRestoreDialog(currentSelection)
	}
}

func (fileBrowser *FileBrowserComponent) Focus() {
	fileBrowser.application.SetFocus(fileBrowser.layout)
}

// computeTableEntries lists the entries of the given path. It runs in the background, so path and snapshotEntry
// must be captured on the UI thread.
func (fileBrowser *FileBrowserComponent) computeTableEntries(
	ctx context.Context,
	path string,
	snapshotEntry *data.SnapshotBrowserEntry,
	previousDiffs map[string]diff_state.DiffState,
) ([]*data.FileBrowserEntry, error) {

	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	// list files in current path
	realFiles, err := util.ListFilesIn(path)
	if err != nil {
		return nil, err
	}

	// list snapshot files in currently path with currently selected snapshot
	var snapshotFilePaths []string
	if snapshotEntry != nil {
		snapshotPath := snapshotEntry.Snapshot.GetSnapshotPath(path)
		snapshotFilePaths, _ = util.ListFilesIn(snapshotPath)
	}

	fileEntries := []*data.FileBrowserEntry{}

	// add entries for files which are present on the "real" location (and possibly within a snapshot as well)
	for _, realFilePath := range realFiles {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		_, realFileName := path2.Split(realFilePath)
		realFileStat, err := os.Lstat(realFilePath)
		if err != nil {
			continue
		}

		entryType := fileBrowser.determineEntryType(realFilePath)
		var snapshotFile *data.SnapshotFile = nil
		if snapshotEntry != nil {
			snapshot := snapshotEntry.Snapshot
			snapshotPathOfRealFile := snapshot.GetSnapshotPath(realFilePath)
			snapshotFile = fileBrowser.computeSnapshotEntryForRealPathIfExists(
				realFilePath,
				snapshotPathOfRealFile,
				snapshot,
			)
			// remove from snapshotFilePaths so we don't add it again later
			snapshotFilePaths = slices.DeleteFunc(snapshotFilePaths, func(s string) bool {
				return s == snapshotPathOfRealFile
			})
		}

		var snapshotFiles []*data.SnapshotFile
		if snapshotFile != nil {
			snapshotFiles = append(snapshotFiles, snapshotFile)
		}

		realFile := &data.RealFile{
			Name: realFileName,
			Path: realFilePath,
			Stat: realFileStat,
		}

		fileBrowserEntry := data.NewFileBrowserEntry(realFileName, realFile, snapshotFiles, entryType)
		fileEntries = append(fileEntries, fileBrowserEntry)
	}

	if snapshotEntry != nil {
		// add remaining entries for files which are only present in the snapshot
		for _, snapshotFilePath := range snapshotFilePaths {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			_, snapshotFileName := path2.Split(snapshotFilePath)

			statSnap, err := os.Lstat(snapshotFilePath)
			if err != nil {
				continue
			}

			entryType := fileBrowser.determineEntryType(snapshotFilePath)

			snapshotFile := &data.SnapshotFile{
				Path:         snapshotFilePath,
				OriginalPath: snapshotEntry.Snapshot.GetRealPath(snapshotFilePath),
				Stat:         statSnap,
				Snapshot:     snapshotEntry.Snapshot,
			}

			snapshotFiles := []*data.SnapshotFile{snapshotFile}
			fileEntries = append(fileEntries, data.NewFileBrowserEntry(snapshotFileName, nil, snapshotFiles, entryType))
		}
	}

	for _, entry := range fileEntries {
		if oldState, exists := previousDiffs[entry.GetRealPath()]; exists {
			entry.DiffState = oldState
		} else {
			entry.DiffState = diff_state.Unknown
		}
		entry.IsLoading = true
	}

	return fileEntries, nil
}

func (fileBrowser *FileBrowserComponent) computeSnapshotEntryForRealPathIfExists(
	realFilePath string,
	snapshotPathOfRealFile string,
	snapshot *zfs.Snapshot,
) *data.SnapshotFile {
	var snapshotFile *data.SnapshotFile = nil
	snapshotFilePath := snapshotPathOfRealFile
	statSnap, err := os.Lstat(snapshotFilePath)
	if err != nil {
		logging.Error("Cannot stat snapshot file: %v", err.Error())
		return nil
	}

	snapshotFile = &data.SnapshotFile{
		Path:         snapshotFilePath,
		OriginalPath: realFilePath,
		Stat:         statSnap,
		Snapshot:     snapshot,
	}
	return snapshotFile
}

// determineEntryType determines whether the given path is a file, directory or symlink.
func (fileBrowser *FileBrowserComponent) determineEntryType(path string) data.FileBrowserEntryType {
	var entryType data.FileBrowserEntryType
	lstat, err := os.Lstat(path)
	if err == nil && lstat.Mode().Type() == os.ModeSymlink {
		entryType = data.Link
	} else if lstat != nil && lstat.IsDir() {
		entryType = data.Directory
	} else {
		entryType = data.File
	}
	return entryType
}

func (fileBrowser *FileBrowserComponent) determineDiffState(
	entry *data.FileBrowserEntry,
	snapshotEntry *data.SnapshotBrowserEntry,
) diff_state.DiffState {
	// figure out status_message
	var status = diff_state.Equal
	if snapshotEntry == nil {
		status = diff_state.Unknown
	} else if entry.HasSnapshot() && !entry.HasReal() {
		// file only exists in snapshot but not in real
		status = diff_state.Deleted
	} else if !entry.HasSnapshot() && entry.HasReal() {
		// file only exists in real but not in snapshot
		status = diff_state.Added
	} else if entry.SnapshotFiles[0].HasChanged() {
		status = diff_state.Modified
	}
	return status
}

func (fileBrowser *FileBrowserComponent) goUp() {
	newSelection := fileBrowser.path
	newPath := path2.Dir(fileBrowser.path)
	if newSelection == newPath {
		return
	}
	fileBrowser.SetPathWithSelection(newPath, newSelection)
}

func (fileBrowser *FileBrowserComponent) SetPathWithSelection(newPath string, newSelection string) {
	// Remember the intended selection for the new path before triggering async refresh
	parentEntryName := path2.Base(path2.Clean(newSelection))
	fakeEntry := &data.FileBrowserEntry{Name: parentEntryName}
	fileBrowser.selectionMemory.Remember(newPath, 0, fakeEntry)

	fileBrowser.SetPath(newPath, false)
}

func (fileBrowser *FileBrowserComponent) SetPath(newPath string, checkExists bool) {
	// TODO use FileBrowserEntry.CanEnter()
	if checkExists {
		stat, err := os.Lstat(newPath)
		if err != nil {
			// cannot enter path, ignoring
			fileBrowser.showError(err)
			return
		}

		if !stat.IsDir() {
			logging.Warning("Tried to enter path which is not a directory: %s", newPath)
			fileBrowser.SetPath(path2.Dir(newPath), false)
			return
		}
	}

	if fileBrowser.path != newPath {
		// Depending on the configuration, an active filter (e.g. "*.txt") is cleared when changing directories.
		// This happens before changing the path, so the resulting selection is remembered for the old path.
		if !configuration.CurrentConfig.FileBrowser.KeepsFilterOnDirectoryChange() {
			fileBrowser.tableContainer.SetFilterText("")
		}

		fileBrowser.path = newPath

		// Optimization: only clear the current snapshot if the new path is no longer within its dataset.
		// This ensures diffs stay visible if we are just navigating within the same dataset.
		if fileBrowser.currentSnapshot != nil {
			dsPath := fileBrowser.currentSnapshot.Snapshot.ParentDataset.Path
			if newPath != dsPath && !strings.HasPrefix(newPath, dsPath+"/") {
				fileBrowser.currentSnapshot = nil
			}
		}

		fileBrowser.emit(PathChangedEvent{NewPath: newPath})
		fileBrowser.Refresh(false)
	}
}
func (fileBrowser *FileBrowserComponent) openActionDialog(selection *data.FileBrowserEntry) {
	if selection == nil {
		return
	}

	// captured on the UI thread, asyncWork runs in the background
	path := fileBrowser.path
	currentSnapshot := fileBrowser.currentSnapshot
	var createdSnapshotName string

	// 1. Define the blocking work (runs in the background: no UI access)
	asyncWork := func(d *dialog.SelectionDialog, action dialog.DialogActionId) error {
		switch action {
		case dialog.FileDialogShowDiffActionId:
			return fileBrowser.showDiff(selection, currentSnapshot)
		case dialog.FileDialogCreateSnapshotDialogActionId:
			name, err := createSnapshot(path)
			createdSnapshotName = name
			return err
		case dialog.FileDialogDeleteDialogActionId:
			return fileBrowser.delete(selection)
		}
		return nil
	}

	// 2. Define the UI updates after the work finishes
	onComplete := func(d *dialog.SelectionDialog, option *dialog.DialogOption, err error) {
		fileBrowser.Refresh(false)
		d.Close()

		if err != nil {
			errDialog := dialog.NewErrorDialogWithRetry(fileBrowser.application, "Action Failed", err, d.RetryFunc(option))
			fileBrowser.showDialog(errDialog, nil)
			return
		}

		switch option.Id {
		case dialog.FileDialogShowHistoryActionId:
			fileBrowser.emit(RequestFileHistoryEvent{FileEntry: selection})
		case dialog.FileDialogCreateSnapshotDialogActionId:
			fileBrowser.emit(SnapshotCreatedEvent{SnapshotName: createdSnapshotName})
		// restores mount the progress dialog, so they must be started on the UI thread, not in asyncWork
		case dialog.FileDialogRestoreRecursiveDialogActionId:
			fileBrowser.runRestoreFileAction(selection, true)
		case dialog.FileDialogRestoreFileActionId:
			fileBrowser.runRestoreFileAction(selection, false)
		}
	}

	actionDialog := dialog.NewFileActionDialog(fileBrowser.application, selection, asyncWork, onComplete)
	fileBrowser.showDialog(actionDialog, nil)
}

func (fileBrowser *FileBrowserComponent) openDeleteDialog(selection *data.FileBrowserEntry) {
	if selection == nil || !selection.HasReal() {
		return
	}

	// 1. The blocking background work
	asyncWork := func(d *dialog.SelectionDialog, action dialog.DialogActionId) error {
		if action == dialog.DeleteFileDialogDeleteFileActionId {
			return fileBrowser.delete(selection)
		}
		return nil
	}

	// 2. The main-thread UI update
	onComplete := func(d *dialog.SelectionDialog, option *dialog.DialogOption, err error) {
		d.Close() // Unmount the selection dialog

		if err != nil {
			errDialog := dialog.NewErrorDialog(fileBrowser.application, "Delete Failed", err)
			fileBrowser.showDialog(errDialog, nil)
		} else {
			// Refresh the UI once the file is deleted
			fileBrowser.Refresh(false)
		}
	}

	deleteDialog := dialog.NewDeleteFileDialog(fileBrowser.application, selection, asyncWork, onComplete)
	fileBrowser.showDialog(deleteDialog, nil) // nil for the onUpdate callback
}
func (fileBrowser *FileBrowserComponent) openRestoreDialog(selection *data.FileBrowserEntry) {
	if selection == nil {
		return
	}
	canRestore := selection.HasSnapshot() || (selection.DiffState != diff_state.Equal && selection.DiffState != diff_state.Unknown)
	if !canRestore {
		return
	}

	// 2. Safely trigger the next UI state on the main thread
	onComplete := func(d *dialog.SelectionDialog, option *dialog.DialogOption, err error) {
		d.Close() // Close the selection menu

		switch option.Id {
		case dialog.RestoreFileDialogRestoreFileActionId:
			fileBrowser.runRestoreFileAction(selection, false)
		case dialog.RestoreFileDialogRestoreRecursiveActionId:
			fileBrowser.runRestoreFileAction(selection, true)
		}
	}

	restoreDialog := dialog.NewRestoreFileDialog(fileBrowser.application, selection, nil, onComplete)
	fileBrowser.showDialog(restoreDialog, nil)
}

func (fileBrowser *FileBrowserComponent) SetSelectedSnapshot(snapshot *data.SnapshotBrowserEntry) {
	if fileBrowser.currentSnapshot == snapshot {
		return
	}

	if fileBrowser.currentSnapshot != nil && snapshot != nil && fileBrowser.currentSnapshot.Snapshot.Path == snapshot.Snapshot.Path {
		return
	}

	fileBrowser.currentSnapshot = snapshot
	fileBrowser.Refresh(true)
}

func (fileBrowser *FileBrowserComponent) startAsyncDiffCalculation() {
	if fileBrowser.diffLoader != nil {
		fileBrowser.diffLoader.Cancel()
	}

	snapshotEntry := fileBrowser.currentSnapshot
	// also calculate the diffs of entries hidden by the filter, so they are correct once the filter changes
	entriesToProcess := slices.Clone(fileBrowser.tableContainer.GetAllEntries())

	if len(entriesToProcess) == 0 {
		return
	}

	ctx, seq := fileBrowser.diffLoader.Start()

	go func() {
		defer fileBrowser.diffLoader.Stop(seq)

		// Debounce rapid scrolling
		select {
		case <-ctx.Done():
			return
		case <-time.After(50 * time.Millisecond):
		}

		// Preemptively set loading state in case computation takes a while
		fileBrowser.application.QueueUpdate(func() {
			if !fileBrowser.diffLoader.IsCurrentSequence(seq) {
				return
			}
			for _, entry := range entriesToProcess {
				if entry != nil {
					entry.IsLoading = true
					fileBrowser.tableContainer.UpdateEntry(entry)
				}
			}
		})

		type diffResult struct {
			entry *data.FileBrowserEntry
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
				if !fileBrowser.diffLoader.IsCurrentSequence(seq) {
					return
				}
				for _, res := range batchCopy {
					res.entry.DiffState = res.state
					res.entry.IsLoading = false
					fileBrowser.tableContainer.UpdateEntry(res.entry)
				}
			}

			if forceDraw {
				fileBrowser.application.QueueUpdateDraw(updateFunc)
			} else {
				fileBrowser.application.QueueUpdate(updateFunc)
			}
		}

		for i, entry := range entriesToProcess {
			if ctx.Err() != nil {
				return
			}

			diffState := fileBrowser.determineDiffState(entry, snapshotEntry)

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

func (fileBrowser *FileBrowserComponent) Refresh(debounce bool) {
	_, _, width, _ := fileBrowser.tableContainer.GetLayout().GetRect()
	if width == 0 {
		width = 80
	}
	maxWidth := width - 10
	if maxWidth < 20 {
		maxWidth = 20
	}

	title := fmt.Sprintf("Path: %s", fileBrowser.truncatePath(fileBrowser.path, maxWidth))
	fileBrowser.tableContainer.SetTitle(title)

	previousDiffs := make(map[string]diff_state.DiffState)
	// all entries, including the ones hidden by the filter
	for _, entry := range fileBrowser.tableContainer.GetAllEntries() {
		if entry != nil {
			previousDiffs[entry.GetRealPath()] = entry.DiffState
		}
	}

	// captured on the UI thread for the background computation
	path := fileBrowser.path
	snapshotEntry := fileBrowser.currentSnapshot

	ctx, seq := fileBrowser.refreshLoader.Start()

	go func() {
		// Debounce rapid calls to Refresh (e.g. from fast scrolling in SnapshotBrowser)
		if debounce {
			select {
			case <-ctx.Done():
				return
			case <-time.After(50 * time.Millisecond):
			}
		}

		entries, err := fileBrowser.computeTableEntries(ctx, path, snapshotEntry, previousDiffs)

		fileBrowser.application.QueueUpdateDraw(func() {
			if !fileBrowser.refreshLoader.IsCurrentSequence(seq) {
				return
			}
			if err != nil {
				fileBrowser.showError(err)
			} else {
				fileBrowser.tableContainer.SetData(entries)
				fileBrowser.entriesPath = path
				fileBrowser.updateFooter()
				fileBrowser.restoreSelectionForPath()
				fileBrowser.updateFileWatcher()

				fileBrowser.startAsyncDiffCalculation()

				fileBrowser.emit(SelectedTableEntryChangedEvent{fileBrowser.GetSelection()})
			}
		})
	}()
}

// truncatePath shortens a file path string to fit within a given maximum width,
// using ellipses to indicate truncation. It uses a multi-stage strategy to
// create a readable, shortened path.
//
// The process is as follows:
//  1. If the path is already within the maxWidth, it is returned unmodified.
//  2. It splits the path into its components (directories/file).
//  3. It iteratively shortens each path component (except the last one) to its
//     first character followed by an ellipsis (e.g., "directory" becomes "d…").
//     After each component is shortened, it checks if the total path length is
//     now within the maxWidth. If it is, the process stops and the new path is
//     returned.
//  4. If the path is still too long after attempting to shorten all components,
//     it falls back to simple truncation from the left, prepending "…" to
//     the end of the path that fits the maxWidth.
//
// This ensures that the most important part of the path (the end) is preserved
// as much as possible, while providing context about the parent directories.
func (fileBrowser *FileBrowserComponent) truncatePath(path string, maxWidth int) string {
	if len([]rune(path)) <= maxWidth {
		return path
	}

	separator := string(os.PathSeparator)
	parts := strings.Split(path, separator)

	if len(parts) <= 1 {
		runes := []rune(path)
		if len(runes) > maxWidth && maxWidth > 1 {
			return "…" + string(runes[len(runes)-maxWidth+1:])
		}
		return path
	}

	// Try shortening parts from left to right, except the last one
	for i := 0; i < len(parts)-1; i++ {
		if parts[i] == "" || parts[i] == "." || parts[i] == ".." {
			continue
		}

		runes := []rune(parts[i])
		if len(runes) > 1 {
			parts[i] = string(runes[0]) + "…"
			newPath := strings.Join(parts, separator)
			if len([]rune(newPath)) <= maxWidth {
				return newPath
			}
		}
	}

	// If still too long, truncate the resulting path with ellipsis at the beginning
	finalPath := strings.Join(parts, separator)
	finalRunes := []rune(finalPath)
	if len(finalRunes) > maxWidth && maxWidth > 1 {
		return "…" + string(finalRunes[len(finalRunes)-maxWidth+1:])
	}

	return finalPath
}

func (fileBrowser *FileBrowserComponent) selectFileEntry(newSelection *data.FileBrowserEntry) {
	if fileBrowser.GetSelection() == newSelection || (fileBrowser.GetSelection() != nil && newSelection != nil && fileBrowser.GetSelection().GetRealPath() == newSelection.GetRealPath()) {
		return
	}

	defer func() {
		fileBrowser.emit(SelectedTableEntryChangedEvent{newSelection})
	}()

	fileBrowser.tableContainer.Select(newSelection)
}

func (fileBrowser *FileBrowserComponent) restoreSelectionForPath() bool {
	if !fileBrowser.entriesAreCurrent() {
		// the remembered selection must not be applied to the entries of another path
		return false
	}

	var entryToSelect *data.FileBrowserEntry
	if fileBrowser.isEmpty() {
		entryToSelect = nil
	} else {
		entries := fileBrowser.GetEntries()
		rememberedSelectionInfo := fileBrowser.getRememberedSelectionInfo(fileBrowser.path)
		if rememberedSelectionInfo == nil {
			if len(entries) > 0 {
				entryToSelect = entries[0]
			}
		} else {
			var index int
			if rememberedSelectionInfo.Entry == nil {
				fileBrowser.SelectHeader()
				return true
			} else {
				index = slices.IndexFunc(entries, func(entry *data.FileBrowserEntry) bool {
					return entry.Name == rememberedSelectionInfo.Entry.Name
				})
			}
			if index < 0 {
				closestIndex := util.Coerce(rememberedSelectionInfo.Index, 0, len(entries)-1)
				entryToSelect = entries[closestIndex]
			} else {
				entryToSelect = entries[index]
			}
		}
	}
	fileBrowser.selectFileEntry(entryToSelect)
	return true
}

func (fileBrowser *FileBrowserComponent) rememberSelectionInfoForCurrentPath() {
	if !fileBrowser.entriesAreCurrent() {
		// a selection within the entries of the previous path must not overwrite the memory of the current path
		return
	}

	selectedEntry := fileBrowser.tableContainer.GetSelectedEntry()
	if selectedEntry == nil {
		fileBrowser.selectionMemory.Remember(fileBrowser.path, -1, nil)
	} else {
		index := slices.Index(fileBrowser.GetEntries(), selectedEntry)
		fileBrowser.selectionMemory.Remember(fileBrowser.path, index, selectedEntry)
	}
}

func (fileBrowser *FileBrowserComponent) getRememberedSelectionInfo(path string) *uiutil.SelectionInfo[data.FileBrowserEntry] {
	return fileBrowser.selectionMemory.Get(path)
}

func (fileBrowser *FileBrowserComponent) GetSelection() *data.FileBrowserEntry {
	return fileBrowser.tableContainer.GetSelectedEntry()
}

func (fileBrowser *FileBrowserComponent) isEmpty() bool {
	return fileBrowser.tableContainer.IsEmpty()
}

func (fileBrowser *FileBrowserComponent) updateFileWatcher() {
	if fileBrowser.fileWatcher != nil && fileBrowser.fileWatcher.RootPath == fileBrowser.path {
		return
	}
	path := fileBrowser.path
	if fileBrowser.fileWatcher != nil {
		fileBrowser.fileWatcher.Stop()
		fileBrowser.fileWatcher = nil
	}
	fileBrowser.fileWatcher = util.NewFileWatcher(path)
	// runs on the goroutine of the file watcher: the refresh reads widgets, so it belongs on the UI thread.
	// Dispatched without waiting, as the watcher holds a lock while calling this, which Stop (on the UI thread)
	// may wait for.
	action := func(s string) {
		go fileBrowser.application.QueueUpdateDraw(func() {
			fileBrowser.Refresh(false)
		})
	}
	err := fileBrowser.fileWatcher.Watch(action)
	if err != nil {
		fileBrowser.showError(err)
	}
}

func (fileBrowser *FileBrowserComponent) HasFocus() bool {
	return fileBrowser.layout.HasFocus()
}

func (fileBrowser *FileBrowserComponent) showDialog(d dialog.Dialog, onClosed func()) {
	dialog.ShowDialogOnPages(fileBrowser.application, fileBrowser.layout, d, onClosed)
}

func (fileBrowser *FileBrowserComponent) openColumnSelectionDialog() {
	d := dialog.NewTableColumnSelectionDialog(fileBrowser.application, "Configure File Browser Columns", tableColumns, fileBrowser.tableContainer)
	fileBrowser.showDialog(d, nil)
}

// enterFileEntry opens the given directory. The remembered selection of that directory is restored
// once its entries are loaded (see Refresh).
func (fileBrowser *FileBrowserComponent) enterFileEntry(selection *data.FileBrowserEntry) {
	if !selection.HasReal() && selection.HasSnapshot() {
		fileBrowser.SetPath(selection.GetRealPath(), false)
	} else if selection.HasReal() {
		fileBrowser.SetPath(selection.GetRealPath(), true)
	}
}

// entriesAreCurrent returns whether the displayed entries belong to the current path.
// After changing the path, the table shows the entries of the previous path until they are loaded.
func (fileBrowser *FileBrowserComponent) entriesAreCurrent() bool {
	return fileBrowser.entriesPath == fileBrowser.path
}

// runRestoreFileAction shows the restore progress dialog, which runs the restore in the background.
// Must be called on the UI thread.
func (fileBrowser *FileBrowserComponent) runRestoreFileAction(entry *data.FileBrowserEntry, recursive bool) error {
	// If the file is absent in the snapshot, create a dummy SnapshotFile referencing the current snapshot.
	if len(entry.SnapshotFiles) == 0 && fileBrowser.currentSnapshot != nil {
		entry.SnapshotFiles = []*data.SnapshotFile{
			{
				Path:         "", // empty path indicates absent in snapshot
				OriginalPath: entry.GetRealPath(),
				Snapshot:     fileBrowser.currentSnapshot.Snapshot,
			},
		}
	}

	d := dialog.NewRestoreFileProgressDialog(fileBrowser.application, entry, recursive)

	// Pass the refresh logic as the onUpdate callback.
	// This will execute safely on the main thread after the dialog closes.
	fileBrowser.showDialog(d, func() {
		fileBrowser.Refresh(false)
	})

	return nil
}
func (fileBrowser *FileBrowserComponent) showDiff(selection *data.FileBrowserEntry, snapshot *data.SnapshotBrowserEntry) error {
	if selection == nil || snapshot == nil {
		return fmt.Errorf("cannot show diff: selection or snapshot is nil")
	}

	realFilePath := selection.RealFile.Path
	snapshotFilePath := snapshot.Snapshot.GetSnapshotPath(selection.RealFile.Path)

	if configuration.CurrentConfig.Diff.Mode == configuration.DiffModeExternal {
		externalConf := configuration.CurrentConfig.Diff.External
		var editorConf *ExternalDiffViewerConfig
		if externalConf == nil {
			editorConf = determineExternalDiffViewer("")
		} else {
			editorConf = &ExternalDiffViewerConfig{
				Path:        externalConf.Path,
				Args:        externalConf.Args,
				WrapInPager: externalConf.WrapInPager,
			}
		}

		if editorConf != nil {
			runExternalDiffEditor(fileBrowser.application, *editorConf, realFilePath, snapshotFilePath)
			return nil
		}
	}

	// INTERNAL DIFF FALLBACK
	// Because showDiff was called from asyncWork, we must push this UI creation
	// back onto the main event loop to avoid breaking tview.
	fileBrowser.application.QueueUpdateDraw(func() {
		d := dialog.NewFileDiffDialog(fileBrowser.application, selection, snapshot)
		fileBrowser.showDialog(d, func() {
			fileBrowser.Refresh(false)
		})
	})

	return nil
}

// delete removes the real file of the given entry. It runs in the background (asyncWork of the dialogs),
// so errors are returned to the dialog, which shows them on the UI thread.
func (fileBrowser *FileBrowserComponent) delete(entry *data.FileBrowserEntry) error {
	return os.RemoveAll(entry.RealFile.Path)
}

// createSnapshot creates a snapshot of the dataset containing the given path, replaceable in tests.
var createSnapshot = createSnapshotForPath

// createSnapshotForPath creates a snapshot of the dataset containing the given path and returns its name.
// It calls into ZFS (and spawns processes), so it must not be called on the UI thread.
func createSnapshotForPath(path string) (string, error) {
	dataset, err := zfs.FindHostDataset(path)
	if err != nil {
		return "", err
	}
	snapshotName := fmt.Sprintf("zfh-%s", time.Now().Format(zfs.SnapshotTimeFormat))
	if err := dataset.CreateSnapshot(snapshotName); err != nil {
		return "", err
	}
	return snapshotName, nil
}

func (fileBrowser *FileBrowserComponent) showMessage(message *status_message.StatusMessage) {
	logging.Info("%s", message.Message)
	fileBrowser.emit(FileBrowserStatusEvent{message})
}

func (fileBrowser *FileBrowserComponent) GetLayout() tview.Primitive {
	return fileBrowser.layout
}

func (fileBrowser *FileBrowserComponent) SetBorderColor(color tcell.Color) {
	if flex, ok := fileBrowser.tableContainer.GetLayout().(*tview.Flex); ok {
		flex.SetBorderColor(color)
	}
}

func (fileBrowser *FileBrowserComponent) SelectHeader() {
	fileBrowser.tableContainer.SelectHeader()
}

func (fileBrowser *FileBrowserComponent) SelectFirstEntryIfExists() {
	fileBrowser.tableContainer.SelectFirstIfExists()
}

// updateFooter shows the number of (matching) entries in the bottom border. Runs on the UI thread.
func (fileBrowser *FileBrowserComponent) updateFooter() {
	fileBrowser.tableContainer.SetFooter(formatFooter(
		len(fileBrowser.tableContainer.GetEntries()),
		len(fileBrowser.tableContainer.GetAllEntries()),
		fileBrowser.tableContainer.IsFilterActive(),
	))
}

// formatFooter returns e.g. "42 entries", or "5 of 42 entries" while a filter is active.
func formatFooter(matchingCount int, totalCount int, filterActive bool) string {
	noun := uiutil.Plural(totalCount, "entry", "entries")
	if filterActive {
		return fmt.Sprintf("%d of %d %s", matchingCount, totalCount, noun)
	}
	if totalCount == 0 {
		return ""
	}
	return fmt.Sprintf("%d %s", totalCount, noun)
}

// fileMatchesFilter matches the file name against the filter as a glob, see table.MatchesGlob.
func fileMatchesFilter(entry *data.FileBrowserEntry, filterText string) bool {
	return table.MatchesGlob(entry.Name, filterText)
}

func (fileBrowser *FileBrowserComponent) GetEntries() []*data.FileBrowserEntry {
	return fileBrowser.tableContainer.GetEntries()
}

func (fileBrowser *FileBrowserComponent) showError(err error) {
	fileBrowser.showMessage(status_message.NewErrorStatusMessage(err.Error()))
}

// currentFolderEntry returns the folder that is shown, as an entry (e.g. for its history).
// historyEntry returns the entry whose history h shows: the selected file or folder, or the folder that is shown
// while the header row is selected or the folder is empty. nil if the selection has no history (e.g. a symlink).
func (fileBrowser *FileBrowserComponent) historyEntry() *data.FileBrowserEntry {
	selection := fileBrowser.GetSelection()
	switch {
	case selection == nil:
		return fileBrowser.currentFolderEntry()
	case selection.Type == data.File || selection.Type == data.Directory:
		return selection
	default:
		return nil
	}
}

func (fileBrowser *FileBrowserComponent) currentFolderEntry() *data.FileBrowserEntry {
	path := fileBrowser.path
	entry := &data.FileBrowserEntry{Name: filepath.Base(path), Type: data.Directory}
	entry.RealFile = &data.RealFile{Name: entry.Name, Path: path}
	return entry
}

func (fileBrowser *FileBrowserComponent) GetShortcutMap() []shortcut_helper.ShortcutEntry {
	shortcutMap := []shortcut_helper.ShortcutEntry{
		uiutil.TableComponentShortcutMove,
		uiutil.TableComponentShortcutColumns,
		uiutil.TableComponentShortcutFilter,
	}

	if selection := fileBrowser.GetSelection(); selection != nil {
		shortcutMap = append(shortcutMap, shortcut_helper.ShortcutEntry{KeyCombo: []string{"←"}, Name: "Parent directory", Group: shortcut_helper.GroupNavigation})

		if ok, _ := selection.CanEnter(); ok {
			shortcutMap = append(shortcutMap, shortcut_helper.ShortcutEntry{KeyCombo: []string{"→"}, Name: "Enter directory", Group: shortcut_helper.GroupNavigation})
		}

		shortcutMap = append(shortcutMap, uiutil.TableComponentShortcutActions)

		if selection.HasReal() {
			shortcutMap = append(shortcutMap, uiutil.TableComponentShortcutDelete)
		}

		if selection.Type == data.File || selection.Type == data.Directory {
			shortcutMap = append(shortcutMap, shortcut_helper.ShortcutEntry{KeyCombo: []string{"h"}, Name: "History"})
		}

		if selection.HasSnapshot() && selection.DiffState != diff_state.Equal {
			shortcutMap = append(shortcutMap, shortcut_helper.ShortcutEntry{KeyCombo: []string{shortcut_helper.Ctrl("r")}, Name: "Restore"})
		}
	} else {
		shortcutMap = append(shortcutMap,
			// on the header row or in an empty folder: the folder that is shown
			shortcut_helper.ShortcutEntry{KeyCombo: []string{"h"}, Name: "Folder history"},
			uiutil.TableComponentShortcutFlipColumnDirection,
			uiutil.TableComponentShortcutCycleSortColumnLeft,
			uiutil.TableComponentShortcutCycleSortColumnRight,
		)
	}

	return shortcutMap
}
