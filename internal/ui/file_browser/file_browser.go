package file_browser

import (
	"errors"
	"fmt"
	"os"
	path2 "path"
	"slices"
	"strings"
	"zfs-file-history/internal/data"
	"zfs-file-history/internal/data/diff_state"
	"zfs-file-history/internal/logging"
	"zfs-file-history/internal/ui/dialog"
	"zfs-file-history/internal/ui/status_message"
	"zfs-file-history/internal/ui/table"
	uiutil "zfs-file-history/internal/ui/util"
	"zfs-file-history/internal/util"
	"zfs-file-history/internal/zfs"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

const (
	FileBrowserPage uiutil.Page = "FileBrowserPage"
)

type FileBrowserSelectionInfo struct {
	Index int
	Entry *data.FileBrowserEntry
}

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
	columnName = &table.Column{
		Id:        4,
		Title:     "Name",
		Alignment: tview.AlignLeft,
	}

	tableColumns = []*table.Column{
		columnSize,
		columnDateTime,
		columnType,
		columnDiff,
		columnName,
	}
)

type FileBrowserComponent struct {
	eventCallback func(event FileBrowserEvent)

	path string

	currentSnapshot *data.SnapshotBrowserEntry

	application *tview.Application
	layout      *tview.Pages

	tableContainer               *table.RowSelectionTable[data.FileBrowserEntry]
	selectedEntryChangedCallback func(fileEntry *data.FileBrowserEntry)

	statusCallback func(message *status_message.StatusMessage)

	selectionIndexMap   map[string]FileBrowserSelectionInfo
	fileWatcher         *util.FileWatcher
	pathChangedCallback func(path string)
}

func NewFileBrowser(application *tview.Application) *FileBrowserComponent {
	tableContainer := createFileBrowserTable(application)

	fileBrowser := &FileBrowserComponent{
		eventCallback: func(event FileBrowserEvent) {},

		application: application,

		selectionIndexMap: map[string]FileBrowserSelectionInfo{},

		tableContainer:               tableContainer,
		selectedEntryChangedCallback: func(fileEntry *data.FileBrowserEntry) {},
		pathChangedCallback:          func(path string) {},
	}

	tableContainer.SetColumnSpec(tableColumns, columnType, true)
	tableContainer.SetDoubleClickCallback(func() {
		fileBrowser.openActionDialog(fileBrowser.GetSelection())
		application.Draw()
	})
	tableContainer.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		key := event.Key()
		if fileBrowser.GetSelection() != nil {
			switch {
			case key == tcell.KeyRight:
				fileBrowser.enterFileEntry(fileBrowser.GetSelection())
				return nil
			case key == tcell.KeyEnter:
				fileBrowser.openActionDialog(fileBrowser.GetSelection())
				return nil
			case event.Rune() == 'd':
				currentSelection := fileBrowser.GetSelection()
				if currentSelection != nil && currentSelection.HasReal() {
					fileBrowser.openDeleteDialog(currentSelection)
				}
			}
		}
		if key == tcell.KeyLeft && (fileBrowser.tableContainer.GetSelectedEntry() != nil || fileBrowser.isEmpty()) {
			fileBrowser.goUp()
			return nil
		}
		return event
	})
	tableContainer.SetSelectionChangedCallback(func(selectedEntry *data.FileBrowserEntry) {
		fileBrowser.rememberSelectionInfoForCurrentPath()
		fileBrowser.selectedEntryChangedCallback(selectedEntry)
	})

	fileBrowser.createLayout()

	return fileBrowser
}

func (fileBrowser *FileBrowserComponent) createLayout() {
	fileBrowserLayout := tview.NewFlex().SetDirection(tview.FlexColumn)

	tableContainer := fileBrowser.tableContainer.GetLayout()

	fileBrowserLayout.AddItem(tableContainer, 0, 1, true)

	fileBrowserPages := tview.NewPages()
	fileBrowserPages.AddPage(string(FileBrowserPage), tableContainer, true, true)

	fileBrowser.layout = fileBrowserPages
}

func (fileBrowser *FileBrowserComponent) Focus() {
	fileBrowser.application.SetFocus(fileBrowser.tableContainer.GetLayout())
}

func (fileBrowser *FileBrowserComponent) computeTableEntries() []*data.FileBrowserEntry {
	path := fileBrowser.path
	snapshotEntry := fileBrowser.currentSnapshot

	// list files in current path
	realFiles, err := util.ListFilesIn(path)
	if os.IsPermission(err) {
		fileBrowser.showError(errors.New("Permission Error: " + err.Error()))
		return nil
	} else if err != nil {
		fileBrowser.showError(errors.New("Cannot list real path: " + err.Error()))
	}

	// list snapshot files in currently path with currently selected snapshot
	var snapshotFilePaths []string
	if snapshotEntry != nil {
		snapshotPath := snapshotEntry.Snapshot.GetSnapshotPath(path)
		snapshotFilePaths, err = util.ListFilesIn(snapshotPath)
		if os.IsPermission(err) {
			fileBrowser.showError(errors.New("Permission Error: " + err.Error()))
		} else if err != nil {
			fileBrowser.showError(errors.New("Cannot list snapshot path: " + err.Error()))
		}
	}

	fileEntries := []*data.FileBrowserEntry{}

	// add entries for files which are present on the "real" location (and possibly within a snapshot as well)
	for _, realFilePath := range realFiles {
		_, realFileName := path2.Split(realFilePath)
		realFileStat, err := os.Lstat(realFilePath)
		if err != nil {
			// TODO: this causes files to be missing from the list, we should probably handle this gracefully somehow
			fileBrowser.showError(err)
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
			_, snapshotFileName := path2.Split(snapshotFilePath)

			statSnap, err := os.Lstat(snapshotFilePath)
			if err != nil {
				fileBrowser.showError(err)
				// TODO: this causes files to be missing from the list, we should probably handle this gracefully somehow
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
		entry.DiffState = fileBrowser.determineDiffState(entry, snapshotEntry)
	}

	return fileEntries
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
		logging.Error("Cannot stat snapshot file: " + err.Error())
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
	fileBrowser.SetPath(newPath, false)
	for _, entry := range fileBrowser.GetEntries() {
		if strings.Contains(entry.GetRealPath(), newSelection) {
			fileBrowser.selectFileEntry(entry)
			return
		}
	}
}

func (fileBrowser *FileBrowserComponent) SetPath(newPath string, checkExists bool) {
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

		_, err = os.ReadDir(newPath)
		if err != nil {
			fileBrowser.showError(err)
			return
		}
	}

	if fileBrowser.path != newPath {
		fileBrowser.path = newPath
		fileBrowser.pathChangedCallback(newPath)
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

func (fileBrowser *FileBrowserComponent) SetSelectedSnapshot(snapshot *data.SnapshotBrowserEntry) {
	if fileBrowser.currentSnapshot == snapshot || fileBrowser.currentSnapshot != nil && snapshot != nil && fileBrowser.currentSnapshot.Snapshot.Path == snapshot.Snapshot.Path {
		return
	}
	fileBrowser.currentSnapshot = snapshot
	fileBrowser.Refresh()
}

func (fileBrowser *FileBrowserComponent) Refresh() {
	fileBrowser.showMessage(status_message.NewInfoStatusMessage("Refreshing..."))
	fileBrowser.updateTableContents()
	fileBrowser.updateFileWatcher()
	// TODO: clearing the message like this will hide error messages instantly...
	fileBrowser.showMessage(status_message.NewInfoStatusMessage(""))
}

func (fileBrowser *FileBrowserComponent) updateTableContents() {
	title := fmt.Sprintf("Path: %s", fileBrowser.path)
	fileBrowser.tableContainer.SetTitle(title)
	newEntries := fileBrowser.computeTableEntries()
	fileBrowser.tableContainer.SetData(newEntries)
	fileBrowser.restoreSelectionForPath()
}

func (fileBrowser *FileBrowserComponent) selectFileEntry(newSelection *data.FileBrowserEntry) {
	fileBrowser.selectedEntryChangedCallback(newSelection)
	if fileBrowser.GetSelection() == newSelection || fileBrowser.GetSelection() != nil && newSelection != nil && fileBrowser.GetSelection().GetRealPath() == newSelection.GetRealPath() {
		return
	}

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
				fileBrowser.selectHeader()
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
		fileBrowser.selectionIndexMap[fileBrowser.path] = FileBrowserSelectionInfo{
			Index: -1,
			Entry: nil,
		}
	} else {
		index := slices.Index(fileBrowser.GetEntries(), selectedEntry)
		fileBrowser.selectionIndexMap[fileBrowser.path] = FileBrowserSelectionInfo{
			Index: index,
			Entry: selectedEntry,
		}
	}
}

func (fileBrowser *FileBrowserComponent) getRememberedSelectionInfo(path string) *FileBrowserSelectionInfo {
	selectionInfo, ok := fileBrowser.selectionIndexMap[path]
	if !ok {
		return nil
	} else {
		return &selectionInfo
	}
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
	return fileBrowser.tableContainer.HasFocus()
}

func (fileBrowser *FileBrowserComponent) showDialog(d dialog.Dialog, actionHandler func(action dialog.DialogActionId) bool) {
	layout := d.GetLayout()
	go func() {
		for {
			action := <-d.GetActionChannel()
			if actionHandler(action) {
				return
			}
			if action == dialog.DialogCloseActionId {
				fileBrowser.layout.RemovePage(d.GetName())
				fileBrowser.application.Draw()
			}
		}
	}()
	fileBrowser.layout.AddPage(d.GetName(), layout, true, true)
}

func (fileBrowser *FileBrowserComponent) enterFileEntry(selection *data.FileBrowserEntry) {
	if !selection.HasReal() && selection.HasSnapshot() {
		fileBrowser.SetPath(selection.GetRealPath(), false)
	} else if selection.HasReal() {
		fileBrowser.SetPath(selection.GetRealPath(), true)
	}
	if !fileBrowser.restoreSelectionForPath() {
		fileBrowser.selectFirstEntryIfExists()
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
	fileBrowser.eventCallback(CreateSnapshotEvent)
}

func (fileBrowser *FileBrowserComponent) showMessage(message *status_message.StatusMessage) {
	logging.Info("%s", message.Message)
	fileBrowser.statusCallback(message)
}

func (fileBrowser *FileBrowserComponent) GetLayout() *tview.Pages {
	return fileBrowser.layout
}

func (fileBrowser *FileBrowserComponent) SetSelectedFileEntryChangedCallback(f func(fileEntry *data.FileBrowserEntry)) {
	fileBrowser.selectedEntryChangedCallback = f
}

func (fileBrowser *FileBrowserComponent) SetPathChangedCallback(f func(path string)) {
	fileBrowser.pathChangedCallback = f
}

func (fileBrowser *FileBrowserComponent) SetStatusCallback(f func(message *status_message.StatusMessage)) {
	fileBrowser.statusCallback = f
}

func (fileBrowser *FileBrowserComponent) selectHeader() {
	fileBrowser.tableContainer.SelectHeader()
}

func (fileBrowser *FileBrowserComponent) selectFirstEntryIfExists() {
	fileBrowser.tableContainer.SelectFirstIfExists()
}

func (fileBrowser *FileBrowserComponent) GetEntries() []*data.FileBrowserEntry {
	return fileBrowser.tableContainer.GetEntries()
}

func (fileBrowser *FileBrowserComponent) showError(err error) {
	fileBrowser.showMessage(status_message.NewErrorStatusMessage(err.Error()))
}

func (fileBrowser *FileBrowserComponent) SetEventCallback(f func(event FileBrowserEvent)) {
	fileBrowser.eventCallback = f
}
