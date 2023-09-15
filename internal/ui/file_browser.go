package ui

import (
	"errors"
	"fmt"
	"github.com/dustin/go-humanize"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"golang.org/x/exp/slices"
	"math"
	"os"
	path2 "path"
	"strings"
	"time"
	"zfs-file-history/internal/data"
	"zfs-file-history/internal/logging"
	"zfs-file-history/internal/ui/dialog"
	uiutil "zfs-file-history/internal/ui/util"
	"zfs-file-history/internal/util"
	"zfs-file-history/internal/zfs"
)

type FileBrowserColumn int

const (
	Name FileBrowserColumn = iota + 1
	Size
	ModTime
	Type
	Status

	FileBrowserPage uiutil.Page = "FileBrowserPage"
)

func (c FileBrowserColumn) IsValid() bool {
	return c <= Status && c >= Name
}

type FileBrowserComponent struct {
	path        string
	pathChanged chan string

	currentSnapshot *zfs.Snapshot

	fileEntries              []*data.FileBrowserEntry
	selectedFileEntry        *data.FileBrowserEntry
	selectedFileEntryChanged chan *data.FileBrowserEntry
	sortByColumn             FileBrowserColumn

	application *tview.Application
	layout      *tview.Pages
	fileTable   *tview.Table

	selectionIndexMap map[string]int
	fileWatcher       *util.FileWatcher
	statusChannel     chan<- *StatusMessage
}

func NewFileBrowser(application *tview.Application, statusChannel chan<- *StatusMessage, path string) *FileBrowserComponent {
	fileBrowser := &FileBrowserComponent{
		application:              application,
		pathChanged:              make(chan string, 10),
		selectedFileEntryChanged: make(chan *data.FileBrowserEntry, 10),
		sortByColumn:             -Type,
		statusChannel:            statusChannel,
		selectionIndexMap:        map[string]int{},
	}

	fileBrowser.createLayout(application)
	fileBrowser.SetPath(path, false)

	return fileBrowser
}

func (fileBrowser *FileBrowserComponent) createLayout(application *tview.Application) {
	fileBrowserLayout := tview.NewFlex().SetDirection(tview.FlexColumn)
	fileBrowserHeaderText := fileBrowser.path

	table := tview.NewTable()
	fileBrowser.fileTable = table

	table.SetMouseCapture(func(action tview.MouseAction, event *tcell.EventMouse) (tview.MouseAction, *tcell.EventMouse) {
		switch action {
		case tview.MouseLeftDoubleClick:
			go func() {
				fileBrowser.openActionDialog(fileBrowser.selectedFileEntry)
				application.Draw()
			}()
			return action, nil
		}
		return action, event
	})

	table.SetBorder(true)
	table.SetBorders(false)
	table.SetBorderPadding(0, 0, 1, 1)

	// fixed header row
	table.SetFixed(1, 0)

	uiutil.SetupWindow(table, fileBrowserHeaderText)

	table.SetSelectable(true, false)
	selectionIndex := fileBrowser.getSelectionIndex(fileBrowser.path)
	table.Select(selectionIndex+1, 0)

	table.SetSelectionChangedFunc(func(row int, column int) {
		selectionIndex := util.Coerce(row-1, -1, len(fileBrowser.fileEntries)-1)
		var newSelection *data.FileBrowserEntry
		if selectionIndex < 0 {
			newSelection = nil
		} else {
			newSelection = fileBrowser.fileEntries[selectionIndex]
		}

		fileBrowser.selectFileEntry(newSelection)
	})

	table.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		key := event.Key()
		if key == tcell.KeyRight {
			if fileBrowser.selectedFileEntry != nil {
				fileBrowser.enterFileEntry(fileBrowser.selectedFileEntry)
			} else {
				fileBrowser.nextSortOrder()
			}
			return nil
		} else if key == tcell.KeyLeft {
			if fileBrowser.listIsEmpty() {
				fileBrowser.goUp()
			} else if fileBrowser.selectedFileEntry != nil {
				fileBrowser.goUp()
			} else if fileBrowser.selectedFileEntry == nil {
				fileBrowser.previousSortOrder()
			}
			return nil
		} else if key == tcell.KeyEnter {
			if fileBrowser.selectedFileEntry == nil {
				fileBrowser.toggleSortOrder()
			} else {
				fileBrowser.openActionDialog(fileBrowser.selectedFileEntry)
			}
			return nil
		} else if key == tcell.KeyUp {
			if fileBrowser.selectedFileEntry == nil {
				fileBrowser.toggleSortOrder()
				return nil
			}
		}
		return event
	})

	table.SetDoneFunc(func(key tcell.Key) {
		if key == tcell.KeyEscape {
			application.Stop()
		}
	})

	fileBrowserLayout.AddItem(table, 0, 1, true)

	fileBrowserPages := tview.NewPages()
	fileBrowserPages.AddPage(string(FileBrowserPage), fileBrowserLayout, true, true)

	fileBrowser.layout = fileBrowserPages
}

func (fileBrowser *FileBrowserComponent) Focus() {
	fileBrowser.application.SetFocus(fileBrowser.fileTable)
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

	fileBrowser.fileEntries = fileEntries
	fileBrowser.sortEntries()
}

func (fileBrowser *FileBrowserComponent) goUp() {
	newSelection := fileBrowser.path
	newPath := path2.Dir(fileBrowser.path)
	fileBrowser.SetPathWithSelection(newPath, newSelection)
}

func (fileBrowser *FileBrowserComponent) SetPathWithSelection(newPath string, selection string) {
	fileBrowser.SetPath(newPath, false)
	for i, entry := range fileBrowser.fileEntries {
		if strings.Contains(entry.GetRealPath(), selection) {
			fileBrowser.SelectEntry(i)
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
	if fileBrowser.selectedFileEntry == nil {
		return
	}
	actionDialogLayout := dialog.NewFileActionDialog(fileBrowser.application, selection)
	actionHandler := func(action dialog.DialogAction) bool {
		switch action {
		case dialog.CreateSnapshotDialogAction:
			fileBrowser.createSnapshot(fileBrowser.selectedFileEntry)
			return true
		case dialog.RestoreRecursiveDialogAction:
			fileBrowser.runRestoreFileAction(fileBrowser.selectedFileEntry, true)
			return true
		case dialog.RestoreFileDialogAction:
			fileBrowser.runRestoreFileAction(fileBrowser.selectedFileEntry, false)
			return true
		case dialog.DeleteDialogAction:
			fileBrowser.delete(fileBrowser.selectedFileEntry)
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
	columnTitles := []FileBrowserColumn{Size, ModTime, Type, Status, Name}

	table := fileBrowser.fileTable
	if table == nil {
		return
	}

	table.Clear()

	title := fmt.Sprintf("Path: %s", fileBrowser.path)
	uiutil.SetupWindow(table, title)

	tableEntries := slices.Clone(fileBrowser.fileEntries)

	cols, rows := len(columnTitles), len(tableEntries)+1
	fileIndex := 0
	for row := 0; row < rows; row++ {
		if (row) == 0 {
			// Draw Table Column Headers
			for column := 0; column < cols; column++ {
				columnId := columnTitles[column]
				var cellColor = tcell.ColorWhite
				var cellText string
				var cellAlignment = tview.AlignLeft
				var cellExpansion = 0

				if columnId == Name {
					cellText = "Name"
				} else if columnId == ModTime {
					cellText = "Date/Time"
				} else if columnId == Type {
					cellText = "Type"
					cellAlignment = tview.AlignCenter
				} else if columnId == Status {
					cellText = "Diff"
					cellAlignment = tview.AlignCenter
				} else if columnId == Size {
					cellText = "Size"
					cellAlignment = tview.AlignCenter
				} else {
					panic("Unknown column")
				}

				if columnId == FileBrowserColumn(math.Abs(float64(fileBrowser.sortByColumn))) {
					var sortDirectionIndicator = "↓"
					if fileBrowser.sortByColumn > 0 {
						sortDirectionIndicator = "↑"
					}
					cellText = fmt.Sprintf("%s %s", cellText, sortDirectionIndicator)
				}

				table.SetCell(row, column,
					tview.NewTableCell(cellText).
						SetTextColor(cellColor).
						SetAlign(cellAlignment).
						SetExpansion(cellExpansion),
				)
			}
			continue
		}

		currentFileEntry := tableEntries[fileIndex]

		var status = "="
		var statusColor = tcell.ColorGray
		switch currentFileEntry.Status {
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
		switch currentFileEntry.Type {
		case data.File:
			typeCellText = "F"
		case data.Directory:
			typeCellText = "D"
			typeCellColor = tcell.ColorSteelBlue
		case data.Link:
			typeCellText = "L"
			typeCellColor = tcell.ColorYellow
		}

		for column := 0; column < cols; column++ {
			columnId := columnTitles[column]
			var cellColor = tcell.ColorWhite
			var cellText string
			var cellAlignment = tview.AlignLeft
			var cellExpansion = 0

			if columnId == Name {
				cellText = currentFileEntry.Name
				if currentFileEntry.GetStat().IsDir() {
					cellText = fmt.Sprintf("/%s", cellText)
				}
				cellColor = statusColor
			} else if columnId == Type {
				cellText = typeCellText
				cellColor = typeCellColor
				cellAlignment = tview.AlignCenter
			} else if columnId == Status {
				cellText = status
				cellColor = statusColor
				cellAlignment = tview.AlignCenter
			} else if columnId == ModTime {
				cellText = currentFileEntry.GetStat().ModTime().Format(time.DateTime)

				switch currentFileEntry.Status {
				case data.Added, data.Deleted:
					cellColor = statusColor
				case data.Modified:
					if currentFileEntry.RealFile.Stat.ModTime() != currentFileEntry.SnapshotFiles[0].Stat.ModTime() {
						cellColor = statusColor
					} else {
						cellColor = tcell.ColorWhite
					}
				default:
					cellColor = tcell.ColorGray
				}
			} else if columnId == Size {
				cellText = humanize.IBytes(uint64(currentFileEntry.GetStat().Size()))
				if strings.HasSuffix(cellText, " B") {
					withoutSuffix := strings.TrimSuffix(cellText, " B")
					cellText = fmt.Sprintf("%s   B", withoutSuffix)
				}
				if len(cellText) < 10 {
					cellText = fmt.Sprintf("%s%s", strings.Repeat(" ", 10-len(cellText)), cellText)
				}

				switch currentFileEntry.Status {
				case data.Added, data.Deleted:
					cellColor = statusColor
				case data.Modified:
					if currentFileEntry.RealFile.Stat.Size() != currentFileEntry.SnapshotFiles[0].Stat.Size() {
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

			table.SetCell(row, column,
				tview.NewTableCell(cellText).
					SetTextColor(cellColor).
					SetAlign(cellAlignment).
					SetExpansion(cellExpansion),
			)
		}
		fileIndex = (fileIndex + 1) % rows
	}

	fileBrowser.restoreSelection()
}

func sortTableEntries(entries []*data.FileBrowserEntry, column FileBrowserColumn) []*data.FileBrowserEntry {
	result := slices.Clone(entries)
	slices.SortFunc(result, func(a, b *data.FileBrowserEntry) int {
		var result int
		columnToSortBy := FileBrowserColumn(math.Abs(float64(column)))
		switch columnToSortBy {
		case Name:
			result = strings.Compare(a.Name, b.Name)
		case ModTime:
			result = a.GetStat().ModTime().Compare(b.GetStat().ModTime())
		case Type:
			result = int(b.Type - a.Type)
		case Size:
			result = int(a.GetStat().Size() - b.GetStat().Size())
		case Status:
			result = int(b.Status - a.Status)
		}

		if column < 0 {
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

func (fileBrowser *FileBrowserComponent) SelectEntry(i int) {
	if len(fileBrowser.fileEntries) > 0 {
		fileBrowser.selectedFileEntry = fileBrowser.fileEntries[i]
		fileBrowser.fileTable.Select(i+1, 0)
	} else {
		fileBrowser.selectedFileEntry = nil
	}
}

func (fileBrowser *FileBrowserComponent) sortEntries() {
	fileBrowser.fileEntries = sortTableEntries(fileBrowser.fileEntries, fileBrowser.sortByColumn)
}

func (fileBrowser *FileBrowserComponent) getSelectionIndex(path string) int {
	index, ok := fileBrowser.selectionIndexMap[path]
	if !ok {
		return 1
	} else if index < 0 {
		return 0
	} else {
		return index
	}
}

func (fileBrowser *FileBrowserComponent) selectFileEntry(newSelection *data.FileBrowserEntry) {
	if fileBrowser.selectedFileEntry == newSelection {
		return
	}

	fileBrowser.selectedFileEntry = newSelection
	go func() {
		fileBrowser.selectedFileEntryChanged <- newSelection
	}()

	// remember selection index
	newIndex := slices.Index(fileBrowser.fileEntries, newSelection)
	fileBrowser.selectionIndexMap[fileBrowser.path] = newIndex + 1
}

func (fileBrowser *FileBrowserComponent) restoreSelection() {
	var selectionIndex int
	if fileBrowser.listIsEmpty() {
		selectionIndex = 0
	} else {
		selectionIndex = fileBrowser.getSelectionIndex(fileBrowser.path)
	}
	fileBrowser.fileTable.Select(selectionIndex, 0)
}

func (fileBrowser *FileBrowserComponent) refresh() {
	fileBrowser.showWarning(NewWarningStatusMessage("Refreshing..."))
	fileBrowser.updateFileEntries()
	fileBrowser.updateTableContents()
	fileBrowser.updateFileWatcher()
	fileBrowser.selectFileEntry(fileBrowser.selectedFileEntry)
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
	return len(fileBrowser.fileEntries) <= 0
}

func (fileBrowser *FileBrowserComponent) HasFocus() bool {
	return fileBrowser.fileTable.HasFocus()
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

func (fileBrowser *FileBrowserComponent) toggleSortOrder() {
	fileBrowser.sortByColumn *= -1
	fileBrowser.refresh()
}

func (fileBrowser *FileBrowserComponent) nextSortOrder() {
	column := FileBrowserColumn(math.Abs(float64(fileBrowser.sortByColumn)) + 1)
	if column.IsValid() {
		if fileBrowser.sortByColumn < 0 {
			column *= -1
		}
		fileBrowser.sortByColumn = column
	} else {
		column = 1
		if fileBrowser.sortByColumn < 0 {
			column *= -1
		}
		fileBrowser.sortByColumn = column
	}
	fileBrowser.refresh()
}

func (fileBrowser *FileBrowserComponent) previousSortOrder() {
	column := FileBrowserColumn(math.Abs(float64(fileBrowser.sortByColumn)) - 1)
	if column.IsValid() {
		if fileBrowser.sortByColumn < 0 {
			column *= -1
		}
		fileBrowser.sortByColumn = column
	} else {
		column = 5
		if fileBrowser.sortByColumn < 0 {
			column *= -1
		}
		fileBrowser.sortByColumn = column
	}
	fileBrowser.refresh()
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

func (fileBrowser *FileBrowserComponent) OnPathChanged() <-chan string {
	return fileBrowser.pathChanged
}

func (fileBrowser *FileBrowserComponent) OnSelectedFileEntryChanged() <-chan *data.FileBrowserEntry {
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
