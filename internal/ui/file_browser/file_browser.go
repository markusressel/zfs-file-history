package file_browser

import (
	"context"
	"fmt"
	"os"
	path2 "path"
	"slices"
	"strings"
	"time"
	"zfs-file-history/internal/configuration"
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

var (
	columnSize = &table.Column{
		Id:        0,
		Title:     "Size",
		Alignment: tview.AlignLeft,
	}
	columnDateTime = &table.Column{
		Id:        1,
		Title:     "Date/Time",
		Alignment: tview.AlignLeft,
	}
	columnType = &table.Column{
		Id:        2,
		Title:     "Type",
		Alignment: tview.AlignCenter,
	}
	columnDiff = &table.Column{
		Id:        3,
		Title:     "Diff",
		Alignment: tview.AlignCenter,
	}
	columnPermissions = &table.Column{
		Id:        4,
		Title:     "Perm",
		Alignment: tview.AlignLeft,
	}
	columnUID = &table.Column{
		Id:        5,
		Title:     "UID",
		Alignment: tview.AlignLeft,
	}
	columnGID = &table.Column{
		Id:        6,
		Title:     "GID",
		Alignment: tview.AlignLeft,
	}
	columnName = &table.Column{
		Id:        7,
		Title:     "Name",
		Alignment: tview.AlignLeft,
	}

	tableColumns = []*table.Column{
		table.ColumnLoading,
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
		table.ColumnLoading,
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

func (fileBrowser *FileBrowserComponent) createLayout() {
	fileBrowser.layout = tview.NewPages().
		AddPage("file-browser", fileBrowser.tableContainer.GetLayout(), true, true)
}

func (fileBrowser *FileBrowserComponent) setupTable() {
	fileBrowser.tableContainer.SetColumnSpec(tableColumns, columnType, true)
	fileBrowser.tableContainer.SetActiveColumns(initialActiveTableColumns)
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

func (fileBrowser *FileBrowserComponent) computeTableEntries(ctx context.Context) ([]*data.FileBrowserEntry, error) {
	path := fileBrowser.path
	snapshotEntry := fileBrowser.currentSnapshot

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

	previousDiffs := make(map[string]diff_state.DiffState)
	for _, entry := range fileBrowser.tableContainer.GetEntries() {
		if entry != nil {
			previousDiffs[entry.GetRealPath()] = entry.DiffState
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
		fileBrowser.Refresh()
	}
}

func (fileBrowser *FileBrowserComponent) openActionDialog(selection *data.FileBrowserEntry) {
	if selection == nil {
		return
	}
	actionDialogLayout := dialog.NewFileActionDialog(fileBrowser.application, selection)
	actionHandler := func(action dialog.DialogActionId) bool {
		switch action {
		case dialog.FileDialogShowDiffActionId:
			fileBrowser.showDiff(selection, fileBrowser.currentSnapshot)
			return true
		case dialog.FileDialogCreateSnapshotDialogActionId:
			fileBrowser.createSnapshot(selection)
			return true
		case dialog.FileDialogRestoreRecursiveDialogActionId:
			fileBrowser.runRestoreFileAction(selection, true)
			return true
		case dialog.FileDialogRestoreFileActionId:
			fileBrowser.runRestoreFileAction(selection, false)
			return true
		case dialog.FileDialogDeleteDialogActionId:
			fileBrowser.delete(selection)
			return true
		default:
			return false
		}
	}
	fileBrowser.showDialog(actionDialogLayout, actionHandler)
}

func (fileBrowser *FileBrowserComponent) openDeleteDialog(selection *data.FileBrowserEntry) {
	if selection == nil || !selection.HasReal() {
		return
	}
	deleteDialogLayout := dialog.NewDeleteFileDialog(fileBrowser.application, selection)
	deleteHandler := func(action dialog.DialogActionId) bool {
		switch action {
		case dialog.DeleteFileDialogDeleteFileActionId:
			fileBrowser.delete(selection)
			return true
		default:
			return false
		}
	}
	fileBrowser.showDialog(deleteDialogLayout, deleteHandler)
}

func (fileBrowser *FileBrowserComponent) openRestoreDialog(selection *data.FileBrowserEntry) {
	if selection == nil || !selection.HasSnapshot() {
		return
	}
	restoreDialogLayout := dialog.NewRestoreFileDialog(fileBrowser.application, selection)
	restoreHandler := func(action dialog.DialogActionId) bool {
		switch action {
		case dialog.RestoreFileDialogRestoreFileActionId:
			fileBrowser.runRestoreFileAction(selection, false)
			return true
		case dialog.RestoreFileDialogRestoreRecursiveActionId:
			fileBrowser.runRestoreFileAction(selection, true)
			return true
		default:
			return false
		}
	}
	fileBrowser.showDialog(restoreDialogLayout, restoreHandler)
}

func (fileBrowser *FileBrowserComponent) SetSelectedSnapshot(snapshot *data.SnapshotBrowserEntry) {
	if fileBrowser.currentSnapshot == snapshot {
		return
	}

	if fileBrowser.currentSnapshot != nil && snapshot != nil && fileBrowser.currentSnapshot.Snapshot.Path == snapshot.Snapshot.Path {
		return
	}

	fileBrowser.currentSnapshot = snapshot
	fileBrowser.Refresh()
}

func (fileBrowser *FileBrowserComponent) startAsyncDiffCalculation() {
	if fileBrowser.diffLoader != nil {
		fileBrowser.diffLoader.Cancel()
	}

	snapshotEntry := fileBrowser.currentSnapshot
	entriesToProcess := slices.Clone(fileBrowser.tableContainer.GetEntries())

	if len(entriesToProcess) == 0 {
		return
	}

	ctx, seq := fileBrowser.diffLoader.Start()

	go func() {
		defer fileBrowser.diffLoader.Stop(seq)

		type diffResult struct {
			entry *data.FileBrowserEntry
			state diff_state.DiffState
		}
		var batch []diffResult

		pushBatch := func() {
			if len(batch) == 0 {
				return
			}
			batchCopy := batch
			batch = nil
			fileBrowser.application.QueueUpdateDraw(func() {
				if !fileBrowser.diffLoader.IsCurrentSequence(seq) {
					return
				}
				for _, res := range batchCopy {
					res.entry.DiffState = res.state
					res.entry.IsLoading = false
					fileBrowser.tableContainer.UpdateEntry(res.entry)
				}
			})
		}

		for i, entry := range entriesToProcess {
			if ctx.Err() != nil {
				return
			}

			diffState := fileBrowser.determineDiffState(entry, snapshotEntry)

			batch = append(batch, diffResult{entry: entry, state: diffState})

			// Push batch for first few items, then every 10 items, or at the end
			if i < 5 || len(batch) >= 10 || i == len(entriesToProcess)-1 {
				pushBatch()
			}
		}
	}()
}

func (fileBrowser *FileBrowserComponent) Refresh() {
	fileBrowser.showMessage(status_message.NewInfoStatusMessage("Refreshing..."))

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

	// Synchronously clear the table so the UI correctly reflects that we are loading a new directory
	fileBrowser.tableContainer.SetData([]*data.FileBrowserEntry{})

	ctx, seq := fileBrowser.refreshLoader.Start()

	go func() {
		entries, err := fileBrowser.computeTableEntries(ctx)

		fileBrowser.application.QueueUpdateDraw(func() {
			if !fileBrowser.refreshLoader.IsCurrentSequence(seq) {
				return
			}
			if err != nil {
				fileBrowser.showError(err)
			} else {
				fileBrowser.tableContainer.SetData(entries)
				fileBrowser.restoreSelectionForPath()
				fileBrowser.updateFileWatcher()

				fileBrowser.startAsyncDiffCalculation()

				fileBrowser.emit(SelectedTableEntryChangedEvent{fileBrowser.GetSelection()})
			}
			fileBrowser.showMessage(status_message.NewInfoStatusMessage(""))
		})
	}()
}

func (fileBrowser *FileBrowserComponent) truncatePath(path string, maxWidth int) string {
	if len([]rune(path)) <= maxWidth {
		return path
	}

	separator := string(os.PathSeparator)
	parts := strings.Split(path, separator)

	if len(parts) <= 1 {
		runes := []rune(path)
		if len(runes) > maxWidth && maxWidth > 3 {
			return "..." + string(runes[len(runes)-maxWidth+3:])
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
	if len(finalRunes) > maxWidth && maxWidth > 3 {
		return "..." + string(finalRunes[len(finalRunes)-maxWidth+3:])
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
	action := func(s string) {
		fileBrowser.Refresh()
		fileBrowser.application.Draw()
	}
	err := fileBrowser.fileWatcher.Watch(action)
	if err != nil {
		fileBrowser.showError(err)
	}
}

func (fileBrowser *FileBrowserComponent) HasFocus() bool {
	return fileBrowser.layout.HasFocus()
}

func (fileBrowser *FileBrowserComponent) showDialog(d dialog.Dialog, actionHandler func(action dialog.DialogActionId) bool) {
	dialog.ShowDialogOnPages(fileBrowser.application, fileBrowser.layout, d, actionHandler, nil)
}

func (fileBrowser *FileBrowserComponent) openColumnSelectionDialog() {
	currentActive := fileBrowser.tableContainer.GetColumnSpec()
	configurableActive := slices.DeleteFunc(slices.Clone(currentActive), func(c *table.Column) bool {
		return c.Id == table.ColumnLoading.Id
	})

	d := dialog.NewColumnSelectionDialog(
		fileBrowser.application,
		"Configure File Browser Columns",
		tableColumns,
		configurableActive,
		func(activeColumns []*table.Column) {
			fileBrowser.tableContainer.SetActiveColumns(append([]*table.Column{table.ColumnLoading}, activeColumns...))
		},
	)
	fileBrowser.showDialog(d, func(action dialog.DialogActionId) bool {
		return false
	})
}

func (fileBrowser *FileBrowserComponent) enterFileEntry(selection *data.FileBrowserEntry) {
	if !selection.HasReal() && selection.HasSnapshot() {
		fileBrowser.SetPath(selection.GetRealPath(), false)
	} else if selection.HasReal() {
		fileBrowser.SetPath(selection.GetRealPath(), true)
	}
	if !fileBrowser.restoreSelectionForPath() {
		fileBrowser.SelectFirstEntryIfExists()
	}
}

func (fileBrowser *FileBrowserComponent) runRestoreFileAction(entry *data.FileBrowserEntry, recursive bool) {
	d := dialog.NewRestoreFileProgressDialog(fileBrowser.application, entry, recursive)
	fileBrowser.showDialog(d, func(action dialog.DialogActionId) bool {
		switch action {
		case dialog.DialogCloseActionId:
			fileBrowser.Refresh()
		}
		return false
	})
}

func (fileBrowser *FileBrowserComponent) showDiff(selection *data.FileBrowserEntry, snapshot *data.SnapshotBrowserEntry) {
	if selection == nil || snapshot == nil {
		return
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
			return
		}
	}

	// Internal diff display (or fallback from external if no editor path)
	d := dialog.NewFileDiffDialog(fileBrowser.application, selection, snapshot)
	fileBrowser.showDialog(d, func(action dialog.DialogActionId) bool {
		switch action {
		case dialog.DialogCloseActionId:
			fileBrowser.Refresh()
		}
		return false
	})
}

func (fileBrowser *FileBrowserComponent) delete(entry *data.FileBrowserEntry) {
	go func() {
		path := entry.RealFile.Path
		err := os.RemoveAll(path)
		if err != nil {
			fileBrowser.showError(err)
		}
	}()
}

func (fileBrowser *FileBrowserComponent) createSnapshot(entry *data.FileBrowserEntry) {
	snapshotName := fmt.Sprintf("zfh-%s", time.Now().Format(zfs.SnapshotTimeFormat))
	fileBrowser.emit(CreateSnapshotEvent{snapshotName})
}

func (fileBrowser *FileBrowserComponent) showMessage(message *status_message.StatusMessage) {
	logging.Info("%s", message.Message)
	fileBrowser.emit(FileBrowserStatusEvent{message})
}

func (fileBrowser *FileBrowserComponent) GetLayout() tview.Primitive {
	return fileBrowser.layout
}

func (fileBrowser *FileBrowserComponent) SelectHeader() {
	fileBrowser.tableContainer.SelectHeader()
}

func (fileBrowser *FileBrowserComponent) SelectFirstEntryIfExists() {
	fileBrowser.tableContainer.SelectFirstIfExists()
}

func (fileBrowser *FileBrowserComponent) GetEntries() []*data.FileBrowserEntry {
	return fileBrowser.tableContainer.GetEntries()
}

func (fileBrowser *FileBrowserComponent) showError(err error) {
	fileBrowser.showMessage(status_message.NewErrorStatusMessage(err.Error()))
}

func (fileBrowser *FileBrowserComponent) GetShortcutMap() []shortcut_helper.ShortcutEntry {
	shortcutMap := []shortcut_helper.ShortcutEntry{
		uiutil.TableComponentShortcutUp,
		uiutil.TableComponentShortcutDown,
		uiutil.TableComponentShortcutColumns,
	}

	if selection := fileBrowser.GetSelection(); selection != nil {
		shortcutMap = append(shortcutMap, shortcut_helper.ShortcutEntry{KeyCombo: []string{"←"}, Name: "Parent directory"})

		if ok, _ := selection.CanEnter(); ok {
			shortcutMap = append(shortcutMap, shortcut_helper.ShortcutEntry{KeyCombo: []string{"→"}, Name: "Enter directory"})
		}

		shortcutMap = append(shortcutMap, uiutil.TableComponentShortcutActions)

		if selection.HasReal() {
			shortcutMap = append(shortcutMap, uiutil.TableComponentShortcutDelete)
		}

		if selection.HasSnapshot() && selection.DiffState != diff_state.Equal {
			shortcutMap = append(shortcutMap, shortcut_helper.ShortcutEntry{KeyCombo: []string{"Ctrl+r"}, Name: "Restore"})
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
