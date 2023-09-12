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
	"zfs-file-history/internal/logging"
	"zfs-file-history/internal/util"
	"zfs-file-history/internal/zfs"
)

type FileBrowserColumn string

const (
	Name    FileBrowserColumn = "Name"
	Size    FileBrowserColumn = "Size"
	ModTime FileBrowserColumn = "ModTime"
	Status  FileBrowserColumn = "Status"
)

type FileBrowser struct {
	currentDataset       *zfs.Dataset
	snapshots            []*zfs.Snapshot
	currentSnapshot      *zfs.Snapshot
	path                 string
	fileEntries          []*FileBrowserEntry
	fileSelection        *FileBrowserEntry
	fileSelectionChanged chan *FileBrowserEntry
	page                 *tview.Flex
	fileTable            *tview.Table
	filesInLatest        []string
	selectionIndexMap    map[string]int
	fileWatcher          *util.FileWatcher
	application          *tview.Application
}

func NewFileBrowser(application *tview.Application, path string) *FileBrowser {
	fileBrowser := &FileBrowser{
		application:          application,
		fileSelectionChanged: make(chan *FileBrowserEntry),
	}

	fileBrowser.SetPath(path)
	fileBrowser.createLayout(application)
	fileBrowser.updateTableContents()

	return fileBrowser
}

func (fileBrowser *FileBrowser) Focus() {
	fileBrowser.application.SetFocus(fileBrowser.fileTable)
}

func (fileBrowser *FileBrowser) createLayout(application *tview.Application) {
	fileBrowserLayout := tview.NewFlex().SetDirection(tview.FlexColumn)
	fileBrowserHeaderText := fmt.Sprintf(" %s ", fileBrowser.path)

	// TODO: insert "/.." cell, if path is not /
	// TODO: use arrow keys to navigate up and down the paths

	table := tview.NewTable()
	fileBrowser.fileTable = table

	table.SetBorder(true)
	table.SetBorders(false)
	table.SetBorderPadding(0, 0, 1, 1)

	// fixed header row
	table.SetFixed(1, 0)

	table.SetTitle(fileBrowserHeaderText)
	table.SetTitleColor(tcell.ColorBlue)
	table.SetTitleAlign(tview.AlignLeft)

	table.SetSelectable(true, false)
	// TODO: remember the selected index for a given path and automatically update the fileSelection when entering and exiting a path
	selectionIndex := fileBrowser.getSelectionIndex(fileBrowser.path)
	table.Select(selectionIndex+1, 0)

	table.SetSelectionChangedFunc(func(row int, column int) {
		selectionIndex := util.Coerce(row-1, -1, len(fileBrowser.fileEntries)-1)
		var newSelection *FileBrowserEntry
		if selectionIndex < 0 {
			newSelection = nil
		} else {
			newSelection = fileBrowser.fileEntries[selectionIndex]
		}
		fileBrowser.fileSelection = newSelection
		go func() {
			fileBrowser.fileSelectionChanged <- newSelection
		}()

		fileBrowser.setSelectionIndex(fileBrowser.path, row)
	})

	table.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		key := event.Key()
		if key == tcell.KeyRight {
			if fileBrowser.fileSelection != nil {
				fileBrowser.SetPath(fileBrowser.fileSelection.Path)
			}
			return nil
		} else if key == tcell.KeyLeft {
			if fileBrowser.fileSelection != nil || fileBrowser.ListIsEmpty() {
				fileBrowser.goUp()
			}
			return nil
		} else if key == tcell.KeyCtrlR {
			fileBrowser.Refresh()
		}
		return event
	})

	table.SetDoneFunc(func(key tcell.Key) {
		if key == tcell.KeyEscape {
			application.Stop()
		}
	})

	fileBrowserLayout.AddItem(table, 0, 1, true)

	fileBrowser.page = fileBrowserLayout
}

func (fileBrowser *FileBrowser) readDirectory(path string) {
	// TODO: list latestFiles and directories in "real" $path as well as all (or a small subset) of the snaphots in a merged view, with an indication of
	//  - whether the file was deleted (compared to the "real" state)
	//  - how many versions there are of this file in snapshots

	latestFiles, err := util.ListFilesIn(path)
	if os.IsPermission(err) {
		fileBrowser.showError(errors.New("Permission Error: " + err.Error()))
		return
	} else if err != nil {
		logging.Fatal("Cannot list path: %s", err.Error())
	}
	mergedFileList := util.UniqueSlice(latestFiles)

	mergedFileEntries := []*FileBrowserEntry{}
	for _, file := range mergedFileList {
		_, name := path2.Split(file)

		stat, err := os.Stat(file)
		if err != nil {
			logging.Error(err.Error())
			continue
		}

		matchingFilesInSnapshots := []*zfs.SnapshotFile{}
		for _, snapshot := range fileBrowser.snapshots {
			snapshotPath := snapshot.GetSnapshotPath(file)
			stat, err := os.Stat(snapshotPath)
			if os.IsNotExist(err) {
				continue
			} else if err != nil {
				logging.Error(err.Error())
				continue
			} else {
				matchingFilesInSnapshots = append(matchingFilesInSnapshots, &zfs.SnapshotFile{
					Path:         snapshotPath,
					OriginalPath: file,
					Stat:         stat,
					Snapshot:     snapshot,
				})
			}
		}

		mergedFileEntries = append(
			mergedFileEntries,
			NewFileBrowserEntry(
				name, file, stat,
				// TODO: include entries for items, which are only found in a snapshot
				false, matchingFilesInSnapshots,
			),
		)
	}

	fileBrowser.fileEntries = mergedFileEntries
	fileBrowser.filesInLatest = latestFiles

	fileBrowser.SortEntries()
}

func (fileBrowser *FileBrowser) GetView() {

}

func (fileBrowser *FileBrowser) goUp() {
	newSelection := fileBrowser.path
	newPath := path2.Dir(fileBrowser.path)
	fileBrowser.SetPathWithSelection(newPath, newSelection)
}

func (fileBrowser *FileBrowser) enterDir(name string) {
	newPath := path2.Join(fileBrowser.path, name)
	fileBrowser.SetPath(newPath)
}

func (fileBrowser *FileBrowser) SetPathWithSelection(newPath string, selection string) {
	fileBrowser.SetPath(newPath)
	for i, entry := range fileBrowser.fileEntries {
		if strings.Contains(entry.Path, selection) {
			fileBrowser.SelectEntry(i)
			return
		}
	}
}

func (fileBrowser *FileBrowser) SetPath(newPath string) {
	stat, err := os.Stat(newPath)
	if err != nil {
		logging.Error(err.Error())
		// cannot enter path, ignoring
		return
	}

	if !stat.IsDir() {
		logging.Warning("Tried to enter path which is not a directory: %s", newPath)
		fileBrowser.SetPath(path2.Dir(newPath))
		return
	}

	_, err = os.ReadDir(newPath)
	if err != nil {
		logging.Error(err.Error())
		return
	}

	fileBrowser.path = newPath
	fileBrowser.updateZfsInfo()
	fileBrowser.readDirectory(fileBrowser.path)
	fileBrowser.updateTableContents()

	fileBrowser.updateFileWatcher(newPath)
}

func (fileBrowser *FileBrowser) openActionDialog(selection string) {

}

func (fileBrowser *FileBrowser) checkIfFileHasChanged(originalFile *FileBrowserEntry, snapshotFile *zfs.SnapshotFile) bool {
	return originalFile.Stat.IsDir() != snapshotFile.Stat.IsDir() ||
		originalFile.Stat.Mode() != snapshotFile.Stat.Mode() ||
		originalFile.Stat.ModTime() != snapshotFile.Stat.ModTime() ||
		originalFile.Stat.Size() != snapshotFile.Stat.Size() ||
		originalFile.Stat.Name() != snapshotFile.Stat.Name()
}

func (fileBrowser *FileBrowser) updateSelectedSnapshot(index int) {
	fileBrowser.currentSnapshot = fileBrowser.snapshots[index]
}

func (fileBrowser *FileBrowser) updateZfsInfo() {
	fileBrowser.currentDataset, _ = zfs.FindHostDataset(fileBrowser.path)

	if fileBrowser.currentDataset != nil {
		snapshots, err := fileBrowser.currentDataset.GetSnapshots()
		if err != nil {
			logging.Fatal(err.Error())
		}
		fileBrowser.snapshots = snapshots
	} else {
		fileBrowser.snapshots = []*zfs.Snapshot{}
	}

	if len(fileBrowser.snapshots) > 0 {
		fileBrowser.currentSnapshot = fileBrowser.snapshots[0]
	} else {
		fileBrowser.currentSnapshot = nil
	}
}

func (fileBrowser *FileBrowser) updateTableContents() {
	columnTitles := []FileBrowserColumn{Size, ModTime, Status, Name}

	table := fileBrowser.fileTable
	if table == nil {
		return
	}

	table.Clear()

	table.SetTitle(fileBrowser.path)

	cols, rows := len(columnTitles), len(fileBrowser.fileEntries)+1
	fileIndex := 0
	for row := 0; row < rows; row++ {
		if (row) == 0 {
			// Draw Table Column Headers
			for column := 0; column < cols; column++ {
				columnTitle := columnTitles[column]
				var cellColor = tcell.ColorWhite
				var cellText string
				var cellAlignment = tview.AlignLeft
				var cellExpansion = 0

				if columnTitle == Name {
					cellText = "Name"
				} else if columnTitle == ModTime {
					cellText = "ModTime"
				} else if columnTitle == Status {
					cellText = "Status"
					cellAlignment = tview.AlignCenter
				} else if columnTitle == Size {
					cellText = "Size"
					cellAlignment = tview.AlignCenter
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
			continue
		}

		currentFilePath := fileBrowser.fileEntries[fileIndex]

		var status = "U"
		var statusColor = tcell.ColorGray
		if currentFilePath.HasSnapshots() && !slices.Contains(fileBrowser.filesInLatest, currentFilePath.Path) {
			// file only exists in snapshot but not in latest
			statusColor = tcell.ColorRed
			status = "D"
		} else if !currentFilePath.HasSnapshots() && slices.Contains(fileBrowser.filesInLatest, currentFilePath.Path) {
			// file only exists in latest but not in snapshot
			statusColor = tcell.ColorGreen
			status = "N"
		} else if fileBrowser.checkIfFileHasChanged(currentFilePath, currentFilePath.Snapshots[0]) {
			statusColor = tcell.ColorYellow
			status = "M"
		}

		for column := 0; column < cols; column++ {
			columnTitle := columnTitles[column]
			var cellColor = tcell.ColorWhite
			var cellText string
			var cellAlignment = tview.AlignLeft
			var cellExpansion = 0

			if columnTitle == Name {
				cellText = fmt.Sprintf("%s", currentFilePath.Name)
				var nameColor = cellColor
				if currentFilePath.Stat.IsDir() {
					cellText = fmt.Sprintf("/%s", cellText)
					nameColor = tcell.ColorSteelBlue
				}
				cellColor = nameColor
			} else if columnTitle == Status {
				cellText = status
				cellColor = statusColor
				cellAlignment = tview.AlignCenter
			} else if columnTitle == ModTime {
				cellText = currentFilePath.Stat.ModTime().Format(time.DateTime)
			} else if columnTitle == Size {
				cellText = humanize.IBytes(uint64(currentFilePath.Stat.Size()))
				if strings.HasSuffix(cellText, " B") {
					withoutSuffix := strings.TrimSuffix(cellText, " B")
					cellText = fmt.Sprintf("%s   B", withoutSuffix)
				}
				if len(cellText) < 10 {
					cellText = fmt.Sprintf("%s%s", strings.Repeat(" ", 10-len(cellText)), cellText)
				}
				cellAlignment = tview.AlignRight
				cellExpansion = 0
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

	table.ScrollToBeginning()

	var selectionIndex int
	if fileBrowser.ListIsEmpty() {
		selectionIndex = 0
	} else {
		selectionIndex = fileBrowser.getSelectionIndex(fileBrowser.path)
	}
	fileBrowser.fileTable.Select(selectionIndex, 0)
}

func (fileBrowser *FileBrowser) SelectEntry(i int) {
	if len(fileBrowser.fileEntries) > 0 {
		fileBrowser.fileSelection = fileBrowser.fileEntries[i]
		fileBrowser.fileTable.Select(i+1, 0)
	} else {
		fileBrowser.fileSelection = nil
	}
}

func (fileBrowser *FileBrowser) SortEntries() {
	slices.SortFunc(fileBrowser.fileEntries, func(a, b *FileBrowserEntry) int {
		if a.Stat.IsDir() == b.Stat.IsDir() {
			return strings.Compare(a.Name, b.Name)
		} else {
			if a.Stat.IsDir() {
				return -1
			} else {
				return 1
			}
		}
	})
}

func (fileBrowser *FileBrowser) showError(err error) {
	fileBrowser.fileTable.SetTitle(err.Error())
}

func (fileBrowser *FileBrowser) getSelectionIndex(path string) int {
	if fileBrowser.selectionIndexMap == nil {
		fileBrowser.selectionIndexMap = map[string]int{}
	}

	index := fileBrowser.selectionIndexMap[path]
	if index <= 1 {
		return 1
	} else {
		return index
	}
}

func (fileBrowser *FileBrowser) setSelectionIndex(path string, index int) {
	if fileBrowser.selectionIndexMap == nil {
		fileBrowser.selectionIndexMap = map[string]int{}
	}
	fileBrowser.selectionIndexMap[path] = index
}

func (fileBrowser *FileBrowser) Refresh() {
	fileBrowser.SetPath(fileBrowser.path)
}

func (fileBrowser *FileBrowser) updateFileWatcher(path string) {
	if fileBrowser.fileWatcher != nil {
		fileBrowser.fileWatcher.Stop()
		fileBrowser.fileWatcher = nil
	}
	fileBrowser.fileWatcher = util.NewFileWatcher(path)
	action := func(s string) {
		fileBrowser.application.QueueUpdateDraw(func() {
			fileBrowser.Refresh()
		})
	}
	err := fileBrowser.fileWatcher.Watch(action)
	if err != nil {
		logging.Fatal(err.Error())
	}
}

func (fileBrowser *FileBrowser) ListIsEmpty() bool {
	return len(fileBrowser.fileEntries) <= 0
}

func (fileBrowser *FileBrowser) HasFocus() bool {
	return fileBrowser.fileTable.HasFocus()
}
