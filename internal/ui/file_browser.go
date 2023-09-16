package ui

import (
	"errors"
	"fmt"
	"github.com/dustin/go-humanize"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"golang.org/x/exp/slices"
	"os"
	path2 "path"
	"strings"
	"time"
	"zfs-file-history/internal/data"
	"zfs-file-history/internal/logging"
	"zfs-file-history/internal/ui/dialog"
	"zfs-file-history/internal/ui/table"
	uiutil "zfs-file-history/internal/ui/util"
	"zfs-file-history/internal/util"
	"zfs-file-history/internal/zfs"
)

const (
	FileBrowserPage uiutil.Page = "FileBrowserPage"
)

var (
	sizeColumn = &table.Column{
		Id:        0,
		Title:     "Size",
		Alignment: tview.AlignLeft,
	}
	datetimeColumn = &table.Column{
		Id:        1,
		Title:     "Date/Time",
		Alignment: tview.AlignLeft,
	}
	typeColumn = &table.Column{
		Id:        2,
		Title:     "Type",
		Alignment: tview.AlignCenter,
	}
	diffColumn = &table.Column{
		Id:        3,
		Title:     "Diff",
		Alignment: tview.AlignCenter,
	}
	nameColumn = &table.Column{
		Id:        4,
		Title:     "Name",
		Alignment: tview.AlignLeft,
	}

	fileBrowserTableColumns = []*table.Column{
		sizeColumn,
		datetimeColumn,
		typeColumn,
		diffColumn,
		nameColumn,
	}
)

type FileBrowserComponent struct {
	path        string
	pathChanged chan string

	currentSnapshot *zfs.Snapshot

	selectedFileEntryChanged chan *data.FileBrowserEntry

	application *tview.Application
	layout      *tview.Pages

	tableContainer *table.RowSelectionTable[data.FileBrowserEntry]

	selectionIndexMap map[string]int
	fileWatcher       *util.FileWatcher
	statusChannel     chan<- *StatusMessage
}

func NewFileBrowser(application *tview.Application, statusChannel chan<- *StatusMessage, path string) *FileBrowserComponent {
	toTableCellsFunction := func(row int, columns []*table.Column, entry *data.FileBrowserEntry) (cells []*tview.TableCell) {
		var status = "="
		var statusColor = tcell.ColorGray
		switch entry.Status {
		case data.Equal:
			status = "="
			statusColor = tcell.ColorGray
		case data.Deleted:
			status = "-"
			statusColor = tcell.ColorRed
		case data.Added:
			status = "+"
			statusColor = tcell.ColorGreen
		case data.Modified:
			status = "≠"
			statusColor = tcell.ColorYellow
		case data.Unknown:
			status = "N/A"
			statusColor = tcell.ColorGray
		}

		var typeCellText = "?"
		var typeCellColor = tcell.ColorGray
		switch entry.Type {
		case data.File:
			typeCellText = "F"
		case data.Directory:
			typeCellText = "D"
			typeCellColor = tcell.ColorSteelBlue
		case data.Link:
			typeCellText = "L"
			typeCellColor = tcell.ColorYellow
		}

		for _, column := range columns {
			var cellColor = tcell.ColorWhite
			var cellText string
			var cellAlignment = tview.AlignLeft
			var cellExpansion = 0

			if column == nameColumn {
				cellText = entry.Name
				if entry.GetStat().IsDir() {
					cellText = fmt.Sprintf("/%s", cellText)
				}
				cellColor = statusColor
			} else if column == typeColumn {
				cellText = typeCellText
				cellColor = typeCellColor
				cellAlignment = tview.AlignCenter
			} else if column == diffColumn {
				cellText = status
				cellColor = statusColor
				cellAlignment = tview.AlignCenter
			} else if column == datetimeColumn {
				cellText = entry.GetStat().ModTime().Format(time.DateTime)

				switch entry.Status {
				case data.Added, data.Deleted:
					cellColor = statusColor
				case data.Modified:
					if entry.RealFile.Stat.ModTime() != entry.SnapshotFiles[0].Stat.ModTime() {
						cellColor = statusColor
					} else {
						cellColor = tcell.ColorWhite
					}
				default:
					cellColor = tcell.ColorGray
				}
			} else if column == sizeColumn {
				cellText = humanize.IBytes(uint64(entry.GetStat().Size()))
				if strings.HasSuffix(cellText, " B") {
					withoutSuffix := strings.TrimSuffix(cellText, " B")
					cellText = fmt.Sprintf("%s   B", withoutSuffix)
				}
				if len(cellText) < 10 {
					cellText = fmt.Sprintf("%s%s", strings.Repeat(" ", 10-len(cellText)), cellText)
				}

				switch entry.Status {
				case data.Added, data.Deleted:
					cellColor = statusColor
				case data.Modified:
					if entry.RealFile.Stat.Size() != entry.SnapshotFiles[0].Stat.Size() {
						cellColor = statusColor
					} else {
						cellColor = tcell.ColorWhite
					}
				default:
					cellColor = tcell.ColorGray
				}

				cellAlignment = tview.AlignRight
			} else {
				panic("Unknown column")
			}

			cell := tview.NewTableCell(cellText).
				SetTextColor(cellColor).
				SetAlign(cellAlignment).
				SetExpansion(cellExpansion)
			cells = append(cells, cell)
		}

		return cells
	}

	tableEntrySortFunction := func(entries []*data.FileBrowserEntry, columnToSortBy *table.Column, inverted bool) []*data.FileBrowserEntry {
		result := slices.Clone(entries)
		slices.SortFunc(result, func(a, b *data.FileBrowserEntry) int {
			var result int
			switch columnToSortBy {
			case nameColumn:
				result = strings.Compare(a.Name, b.Name)
			case datetimeColumn:
				result = a.GetStat().ModTime().Compare(b.GetStat().ModTime())
			case typeColumn:
				result = int(b.Type - a.Type)
			case sizeColumn:
				result = int(a.GetStat().Size() - b.GetStat().Size())
			case diffColumn:
				result = int(b.Status - a.Status)
			}

			if inverted {
				result *= -1
			}

			if result != 0 {
				return result
			}

			result = int(b.Type - a.Type)
			if result != 0 {
				return result
			}

			result = strings.Compare(a.Name, b.Name)
			if result != 0 {
				return result
			}

			return result
		})

		return result
	}

	tableContainer := table.NewTableContainer[data.FileBrowserEntry](
		application,
		toTableCellsFunction,
		tableEntrySortFunction,
	)

	fileBrowser := &FileBrowserComponent{
		application:              application,
		pathChanged:              make(chan string, 10),
		selectedFileEntryChanged: make(chan *data.FileBrowserEntry, 10),
		statusChannel:            statusChannel,
		selectionIndexMap:        map[string]int{},

		tableContainer: tableContainer,
	}

	fileBrowser.createLayout(application)
	fileBrowser.SetPath(path, false)

	return fileBrowser
}

func (fileBrowser *FileBrowserComponent) createLayout(application *tview.Application) {
	fileBrowserLayout := tview.NewFlex().SetDirection(tview.FlexColumn)

	tableContainer := fileBrowser.tableContainer.GetLayout()

	tableContainer.SetMouseCapture(func(action tview.MouseAction, event *tcell.EventMouse) (tview.MouseAction, *tcell.EventMouse) {
		switch action {
		case tview.MouseLeftDoubleClick:
			go func() {
				fileBrowser.openActionDialog(fileBrowser.getSelection())
				application.Draw()
			}()
			return action, nil
		}
		return action, event
	})

	tableContainer.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		key := event.Key()
		if fileBrowser.getSelection() != nil {
			if key == tcell.KeyRight {
				fileBrowser.enterFileEntry(fileBrowser.getSelection())
				return nil
			} else if key == tcell.KeyEnter {
				fileBrowser.openActionDialog(fileBrowser.getSelection())
				return nil
			}
		}
		if key == tcell.KeyLeft && (fileBrowser.tableContainer.GetSelectedEntry() != nil || fileBrowser.listIsEmpty()) {
			fileBrowser.goUp()
			return nil
		}
		return event
	})

	fileBrowserLayout.AddItem(tableContainer, 0, 1, true)

	fileBrowserPages := tview.NewPages()
	fileBrowserPages.AddPage(string(FileBrowserPage), tableContainer, true, true)

	fileBrowser.layout = fileBrowserPages
}

func (fileBrowser *FileBrowserComponent) Focus() {
	fileBrowser.application.SetFocus(fileBrowser.tableContainer.GetLayout())
}

func (fileBrowser *FileBrowserComponent) updateFileEntries() {
	path := fileBrowser.path
	snapshot := fileBrowser.currentSnapshot

	// list files in current path
	realFiles, err := util.ListFilesIn(path)
	if os.IsPermission(err) {
		fileBrowser.showError(errors.New("Permission Error: " + err.Error()))
		return
	} else if err != nil {
		fileBrowser.showError(errors.New("Cannot list real path: " + err.Error()))
	}

	// list snapshot files in currently path with currently selected snapshot
	var snapshotFilePaths []string
	if snapshot != nil {
		snapshotPath := snapshot.GetSnapshotPath(path)
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
		realFileStat, err := os.Stat(realFilePath)
		if err != nil {
			// TODO: this causes files to be missing from the list, we should probably handle this gracefully somehow
			fileBrowser.showError(err)
			continue
		}

		var entryType data.FileBrowserEntryType

		lstat, err := os.Lstat(realFilePath)
		if err == nil && lstat.Mode().Type() == os.ModeSymlink {
			entryType = data.Link
		} else if lstat.IsDir() {
			entryType = data.Directory
		} else {
			entryType = data.File
		}

		var snapshotFile *data.SnapshotFile = nil
		if snapshot != nil {
			snapshotFilePath := snapshot.GetSnapshotPath(realFilePath)
			snapshotFilePaths = slices.DeleteFunc(snapshotFilePaths, func(s string) bool {
				return s == snapshotFilePath
			})
			statSnap, err := os.Stat(snapshotFilePath)
			if err != nil {
				if !os.IsNotExist(err) {
					snapshotFilePaths = slices.DeleteFunc(snapshotFilePaths, func(s string) bool {
						return s == snapshotFilePath
					})
				}
			} else {
				snapshotFile = &data.SnapshotFile{
					Path:         snapshotFilePath,
					OriginalPath: realFilePath,
					Stat:         statSnap,
					Snapshot:     snapshot,
				}
			}
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

	// add remaining entries for files which are only present in the snapshot
	for _, snapshotFilePath := range snapshotFilePaths {
		_, snapshotFileName := path2.Split(snapshotFilePath)

		statSnap, err := os.Stat(snapshotFilePath)
		if err != nil {
			fileBrowser.showError(err)
			// TODO: this causes files to be missing from the list, we should probably handle this gracefully somehow
			continue
		}

		var entryType data.FileBrowserEntryType
		lstat, err := os.Lstat(snapshotFilePath)
		if err == nil && lstat.Mode().Type() == os.ModeSymlink {
			entryType = data.Link
		} else if lstat.IsDir() {
			entryType = data.Directory
		} else {
			entryType = data.File
		}

		snapshotFile := &data.SnapshotFile{
			Path:         snapshotFilePath,
			OriginalPath: snapshot.GetRealPath(snapshotFilePath),
			Stat:         statSnap,
			Snapshot:     snapshot,
		}

		snapshotFiles := []*data.SnapshotFile{snapshotFile}
		fileEntries = append(fileEntries, data.NewFileBrowserEntry(snapshotFileName, nil, snapshotFiles, entryType))
	}

	for _, entry := range fileEntries {
		// figure out status
		var status = data.Equal
		if fileBrowser.currentSnapshot == nil {
			status = data.Unknown
		} else if entry.HasSnapshot() && !entry.HasReal() {
			// file only exists in snapshot but not in real
			status = data.Deleted
		} else if !entry.HasSnapshot() && entry.HasReal() {
			// file only exists in real but not in snapshot
			status = data.Added
		} else if entry.SnapshotFiles[0].HasChanged() {
			status = data.Modified
		}
		entry.Status = status
	}

	fileBrowser.tableContainer.SetData(fileBrowserTableColumns, fileEntries)
}

func (fileBrowser *FileBrowserComponent) goUp() {
	newSelection := fileBrowser.path
	newPath := path2.Dir(fileBrowser.path)
	fileBrowser.SetPathWithSelection(newPath, newSelection)
}

func (fileBrowser *FileBrowserComponent) SetPathWithSelection(newPath string, selection string) {
	fileBrowser.SetPath(newPath, false)
	for _, entry := range fileBrowser.tableContainer.GetEntries() {
		if strings.Contains(entry.GetRealPath(), selection) {
			fileBrowser.selectFileEntry(entry)
			return
		}
	}
}

func (fileBrowser *FileBrowserComponent) SetPath(newPath string, checkExists bool) {
	// TODO: allow entering a path, even if it only exists within a snapshot,
	//  be careful about "restore" action from nested folders though!
	if checkExists {
		stat, err := os.Stat(newPath)
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
		go func() {
			fileBrowser.pathChanged <- newPath
		}()
		fileBrowser.refresh()
	}
}

func (fileBrowser *FileBrowserComponent) openActionDialog(selection *data.FileBrowserEntry) {
	if fileBrowser.getSelection() == nil {
		return
	}
	actionDialogLayout := dialog.NewFileActionDialog(fileBrowser.application, selection)
	actionHandler := func(action dialog.DialogAction) bool {
		switch action {
		case dialog.CreateSnapshotDialogAction:
			fileBrowser.createSnapshot(fileBrowser.getSelection())
			return true
		case dialog.RestoreRecursiveDialogAction:
			fileBrowser.runRestoreFileAction(fileBrowser.getSelection(), true)
			return true
		case dialog.RestoreFileDialogAction:
			fileBrowser.runRestoreFileAction(fileBrowser.getSelection(), false)
			return true
		case dialog.DeleteDialogAction:
			fileBrowser.delete(fileBrowser.getSelection())
			return true
		}
		return false
	}
	fileBrowser.showDialog(actionDialogLayout, actionHandler)
}

func (fileBrowser *FileBrowserComponent) SetSelectedSnapshot(snapshot *zfs.Snapshot) {
	if fileBrowser.currentSnapshot == snapshot {
		return
	}
	fileBrowser.currentSnapshot = snapshot
	fileBrowser.refresh()
}

func (fileBrowser *FileBrowserComponent) updateTableContents() {
	title := fmt.Sprintf("Path: %s", fileBrowser.path)
	fileBrowser.tableContainer.SetTitle(title)
	fileBrowser.restoreSelection()
}

func (fileBrowser *FileBrowserComponent) SelectEntry(i int) {
	if len(fileBrowser.tableContainer.GetEntries()) > 0 {
		fileBrowser.tableContainer.Select(fileBrowser.tableContainer.GetEntries()[i])
	} else {
		fileBrowser.tableContainer.Select(nil)
	}
}

func (fileBrowser *FileBrowserComponent) getRememberedSelectionIndex(path string) int {
	index, ok := fileBrowser.selectionIndexMap[path]
	if !ok {
		return -1
	} else if index < 0 {
		return 0
	} else {
		return index
	}
}

func (fileBrowser *FileBrowserComponent) selectFileEntry(newSelection *data.FileBrowserEntry) {
	if fileBrowser.getSelection() == newSelection {
		return
	}

	fileBrowser.tableContainer.Select(newSelection)
	go func() {
		fileBrowser.selectedFileEntryChanged <- newSelection
	}()

	// remember selection index
	newIndex := slices.Index(fileBrowser.tableContainer.GetEntries(), newSelection)
	fileBrowser.selectionIndexMap[fileBrowser.path] = newIndex + 1
}

func (fileBrowser *FileBrowserComponent) restoreSelection() {
	if fileBrowser.listIsEmpty() {
		fileBrowser.tableContainer.Select(nil)
	} else {
		rememberedIndex := fileBrowser.getRememberedSelectionIndex(fileBrowser.path)
		if rememberedIndex > 0 {
			entry := fileBrowser.tableContainer.GetEntries()[rememberedIndex]
			fileBrowser.tableContainer.Select(entry)
		} else {
			fileBrowser.tableContainer.Select(fileBrowser.tableContainer.GetEntries()[0])
		}
	}
}

func (fileBrowser *FileBrowserComponent) refresh() {
	fileBrowser.showWarning(NewWarningStatusMessage("Refreshing..."))
	fileBrowser.updateFileEntries()
	fileBrowser.updateTableContents()
	fileBrowser.updateFileWatcher()
	// TODO: restore selection after refresh
	//fileBrowser.selectFileEntry(fileBrowser.selectedFileEntry)
	fileBrowser.showInfo(NewInfoStatusMessage(""))
}

func (fileBrowser *FileBrowserComponent) updateFileWatcher() {
	path := fileBrowser.path
	if fileBrowser.fileWatcher != nil {
		fileBrowser.fileWatcher.Stop()
		fileBrowser.fileWatcher = nil
	}
	fileBrowser.fileWatcher = util.NewFileWatcher(path)
	action := func(s string) {
		fileBrowser.refresh()
		fileBrowser.application.Draw()
	}
	err := fileBrowser.fileWatcher.Watch(action)
	if err != nil {
		fileBrowser.showError(err)
	}
}

func (fileBrowser *FileBrowserComponent) listIsEmpty() bool {
	return len(fileBrowser.tableContainer.GetEntries()) <= 0
}

func (fileBrowser *FileBrowserComponent) HasFocus() bool {
	return fileBrowser.tableContainer.HasFocus()
}

func (fileBrowser *FileBrowserComponent) showDialog(d dialog.Dialog, actionHandler func(action dialog.DialogAction) bool) {
	layout := d.GetLayout()
	go func() {
		for {
			action := <-d.GetActionChannel()
			if actionHandler(action) {
				return
			}
			if action == dialog.ActionClose {
				fileBrowser.layout.RemovePage(d.GetName())
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
}

func (fileBrowser *FileBrowserComponent) runRestoreFileAction(entry *data.FileBrowserEntry, recursive bool) {
	d := dialog.NewRestoreFileProgressDialog(fileBrowser.application, entry, recursive)
	fileBrowser.showDialog(d, func(action dialog.DialogAction) bool {
		switch action {
		case dialog.ActionClose:
			fileBrowser.refresh()
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
	fileBrowser.showWarning(NewWarningStatusMessage("Sorry, creating snapshots is not yet supported :(").SetDuration(5 * time.Second))
}

func (fileBrowser *FileBrowserComponent) PathChangedChannel() <-chan string {
	return fileBrowser.pathChanged
}

func (fileBrowser *FileBrowserComponent) SelectedFileEntryChangedChannel() <-chan *data.FileBrowserEntry {
	return fileBrowser.selectedFileEntryChanged
}

func (fileBrowser *FileBrowserComponent) showInfo(message *StatusMessage) {
	logging.Info(message.Message)
	fileBrowser.sendStatusMessage(message)
}

func (fileBrowser *FileBrowserComponent) showWarning(message *StatusMessage) {
	logging.Warning(message.Message)
	fileBrowser.sendStatusMessage(message)
}

func (fileBrowser *FileBrowserComponent) showError(err error) {
	logging.Error(err.Error())
	fileBrowser.sendStatusMessage(NewErrorStatusMessage(err.Error()))
}

func (fileBrowser *FileBrowserComponent) sendStatusMessage(message *StatusMessage) {
	go func() {
		fileBrowser.statusChannel <- message
	}()
}

func (fileBrowser *FileBrowserComponent) getSelection() *data.FileBrowserEntry {
	return fileBrowser.tableContainer.GetSelectedEntry()
}
