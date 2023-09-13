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
	ModTime FileBrowserColumn = "DateTime"
	Status  FileBrowserColumn = "Status"
)

type FileBrowser struct {
	path string

	currentSnapshot *zfs.Snapshot

	fileEntries              []*FileBrowserEntry
	fileSelection            *FileBrowserEntry
	selectedFileEntryChanged chan *FileBrowserEntry

	application *tview.Application
	layout      *tview.Flex
	fileTable   *tview.Table

	selectionIndexMap map[string]int
	fileWatcher       *util.FileWatcher
}

func NewFileBrowser(application *tview.Application, path string) *FileBrowser {
	fileBrowser := &FileBrowser{
		application:              application,
		selectedFileEntryChanged: make(chan *FileBrowserEntry),
	}

	fileBrowser.createLayout(application)
	fileBrowser.SetPath(path)

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

		if fileBrowser.fileSelection != newSelection {
			fileBrowser.fileSelection = newSelection
			go func() {
				fileBrowser.selectedFileEntryChanged <- newSelection
			}()
		}

		fileBrowser.setSelectionIndex(fileBrowser.path, row)
	})

	table.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		key := event.Key()
		if key == tcell.KeyRight {
			if fileBrowser.fileSelection != nil {
				fileBrowser.SetPath(fileBrowser.fileSelection.GetRealPath())
			}
			return nil
		} else if key == tcell.KeyLeft {
			if fileBrowser.fileSelection != nil || fileBrowser.ListIsEmpty() {
				fileBrowser.goUp()
			}
			return nil
		} else if key == tcell.KeyCtrlR {
			fileBrowser.refresh()
		}
		return event
	})

	table.SetDoneFunc(func(key tcell.Key) {
		if key == tcell.KeyEscape {
			application.Stop()
		}
	})

	fileBrowserLayout.AddItem(table, 0, 1, true)

	fileBrowser.layout = fileBrowserLayout
}

func (fileBrowser *FileBrowser) updateFileEntries() {
	path := fileBrowser.path
	snapshot := fileBrowser.currentSnapshot

	// list files in current directory
	latestFiles, err := util.ListFilesIn(path)
	if os.IsPermission(err) {
		fileBrowser.showError(errors.New("Permission Error: " + err.Error()))
		return
	} else if err != nil {
		logging.Fatal("Cannot list path: %s", err.Error())
	}

	// list files in currently directory with currently selected snapshot

	var snapshotFiles []string
	if snapshot != nil {
		snapshotPath := snapshot.GetSnapshotPath(path)
		snapshotFiles, err = util.ListFilesIn(snapshotPath)
		if os.IsPermission(err) {
			fileBrowser.showError(errors.New("Permission Error: " + err.Error()))
			return
		} else if err != nil {
			logging.Error("Cannot list path: %s", err.Error())
		}
	}

	fileEntries := []*FileBrowserEntry{}

	// add entries for files which are present on the "real" location (and possibly within a snapshot as well)
	for _, latestFilePath := range latestFiles {
		_, latestFileName := path2.Split(latestFilePath)
		latestFileStat, err := os.Stat(latestFilePath)
		if err != nil {
			// TODO: this causes files to be missing from the list, we should probably handle this gracefully somehow
			logging.Error(err.Error())
			continue
		}

		var snapshotFile *SnapshotFile = nil
		if snapshot != nil {
			snapshotFilePath := snapshot.GetSnapshotPath(latestFilePath)
			statSnap, err := os.Stat(snapshotFilePath)
			if err != nil {
				logging.Error(err.Error())
				snapshotFiles = slices.DeleteFunc(snapshotFiles, func(s string) bool {
					return s == snapshotFilePath
				})
			} else {
				snapshotFile = &SnapshotFile{
					Path:         snapshotFilePath,
					OriginalPath: latestFilePath,
					Stat:         statSnap,
					Snapshot:     snapshot,
				}
				snapshotFiles = slices.DeleteFunc(snapshotFiles, func(s string) bool {
					return s == snapshotFilePath
				})
			}
		}

		snapshotFiles := []*SnapshotFile{}
		if snapshotFile != nil {
			snapshotFiles = append(snapshotFiles, snapshotFile)
		}

		latestFile := &RealFile{
			Name: latestFileName,
			Path: latestFilePath,
			Stat: latestFileStat,
		}

		fileEntries = append(fileEntries, NewFileBrowserEntry(latestFileName, latestFile, snapshotFiles))
	}

	// add remaining entries for files which are only present in the snapshot
	for _, snapshotFilePath := range snapshotFiles {
		_, snapshotFileName := path2.Split(snapshotFilePath)

		statSnap, err := os.Stat(snapshotFilePath)
		if err != nil {
			logging.Error(err.Error())
			// TODO: this causes files to be missing from the list, we should probably handle this gracefully somehow
			continue
		}

		snapshotFile := &SnapshotFile{
			Path:         snapshotFilePath,
			OriginalPath: snapshot.GetRealPath(snapshotFilePath),
			Stat:         statSnap,
			Snapshot:     snapshot,
		}

		snapshotFiles := []*SnapshotFile{}
		if snapshotFile != nil {
			snapshotFiles = append(snapshotFiles, snapshotFile)
		}

		fileEntries = append(fileEntries, NewFileBrowserEntry(snapshotFileName, nil, snapshotFiles))
	}

	fileBrowser.fileEntries = fileEntries
	fileBrowser.SortEntries()
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
		if strings.Contains(entry.GetRealPath(), selection) {
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
		fileBrowser.showError(err)
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
		fileBrowser.showError(err)
		return
	}

	fileBrowser.path = newPath
	fileBrowser.refresh()
}

func (fileBrowser *FileBrowser) openActionDialog(selection string) {

}

func (fileBrowser *FileBrowser) checkIfFileHasChanged(originalFile *RealFile, snapshotFile *SnapshotFile) bool {
	return originalFile.Stat.IsDir() != snapshotFile.Stat.IsDir() ||
		originalFile.Stat.Mode() != snapshotFile.Stat.Mode() ||
		originalFile.Stat.ModTime() != snapshotFile.Stat.ModTime() ||
		originalFile.Stat.Size() != snapshotFile.Stat.Size() ||
		originalFile.Stat.Name() != snapshotFile.Stat.Name()
}

func (fileBrowser *FileBrowser) SetSelectedSnapshot(snapshot *zfs.Snapshot) {
	if fileBrowser.currentSnapshot != snapshot {
		fileBrowser.currentSnapshot = snapshot
		fileBrowser.refresh()
	}
}

func (fileBrowser *FileBrowser) updateTableContents() {
	columnTitles := []FileBrowserColumn{Size, ModTime, Status, Name}

	table := fileBrowser.fileTable
	if table == nil {
		return
	}

	table.Clear()

	title := fmt.Sprintf(" Current Path: %s ", fileBrowser.path)
	table.SetTitle(title)

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
					cellText = "Date/Time"
				} else if columnTitle == Status {
					cellText = "Diff"
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

		currentFileEntry := fileBrowser.fileEntries[fileIndex]

		var status = "="
		var statusColor = tcell.ColorGray
		if currentFileEntry.HasSnapshots() && !currentFileEntry.HasLatest() {
			// file only exists in snapshot but not in latest
			statusColor = tcell.ColorRed
			status = "-"
		} else if !currentFileEntry.HasSnapshots() && currentFileEntry.HasLatest() {
			// file only exists in latest but not in snapshot
			statusColor = tcell.ColorGreen
			status = "+"
		} else if fileBrowser.checkIfFileHasChanged(currentFileEntry.LatestFile, currentFileEntry.SnapshotFiles[0]) {
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
				cellText = fmt.Sprintf("%s", currentFileEntry.Name)
				if currentFileEntry.GetStat().IsDir() {
					cellText = fmt.Sprintf("/%s", cellText)
					cellColor = tcell.ColorSteelBlue
				} else {
					cellColor = statusColor
				}
			} else if columnTitle == Status {
				cellText = status
				cellColor = statusColor
				cellAlignment = tview.AlignCenter
			} else if columnTitle == ModTime {
				cellText = currentFileEntry.GetStat().ModTime().Format(time.DateTime)
			} else if columnTitle == Size {
				cellText = humanize.IBytes(uint64(currentFileEntry.GetStat().Size()))
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
		if a.GetStat().IsDir() == b.GetStat().IsDir() {
			return strings.Compare(a.Name, b.Name)
		} else {
			if a.GetStat().IsDir() {
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

func (fileBrowser *FileBrowser) refresh() {
	fileBrowser.updateFileEntries()
	fileBrowser.updateTableContents()
	fileBrowser.updateFileWatcher()
}

func (fileBrowser *FileBrowser) updateFileWatcher() {
	path := fileBrowser.path
	if fileBrowser.fileWatcher != nil {
		fileBrowser.fileWatcher.Stop()
		fileBrowser.fileWatcher = nil
	}
	fileBrowser.fileWatcher = util.NewFileWatcher(path)
	action := func(s string) {
		fileBrowser.application.QueueUpdateDraw(func() {
			fileBrowser.refresh()
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
