package ui

import (
	"fmt"
	"github.com/dustin/go-humanize"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"golang.org/x/exp/slices"
	"os"
	path2 "path"
	"strings"
	"zfs-file-history/internal/logging"
	"zfs-file-history/internal/util"
	"zfs-file-history/internal/zfs"
)

type FileBrowserColumn string

const (
	Name   FileBrowserColumn = "Name"
	Size   FileBrowserColumn = "Size"
	Status FileBrowserColumn = "Status"
)

type FileBrowser struct {
	currentDataset  *zfs.Dataset
	snapshots       []*zfs.Snapshot
	currentSnapshot *zfs.Snapshot
	path            string
	fileEntries     []*FileBrowserEntry
	fileSelection   *FileBrowserEntry
	page            *tview.Flex
	table           *tview.Table
	filesInLatest   []string
}

func NewFileBrowser(application *tview.Application, path string) *FileBrowser {
	fileBrowser := FileBrowser{}

	fileBrowser.SetPath(path)
	fileBrowser.Layout(application)
	fileBrowser.updateTableContents()

	return &fileBrowser
}

func (fileBrowser *FileBrowser) Layout(application *tview.Application) {
	fileBrowserLayout := tview.NewFlex().SetDirection(tview.FlexColumn)
	fileBrowserHeaderText := fmt.Sprintf(" %s ", fileBrowser.path)

	// TODO: insert "/.." cell, if path is not /
	// TODO: use arrow keys to navigate up and down the paths

	datasetInfoBox := fileBrowser.createDatasetInfoBox()
	snapshotsInfoBox := fileBrowser.createSnapshotsInfoBox()

	table := tview.NewTable()
	fileBrowser.table = table

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
	table.Select(0, 0)

	table.SetSelectionChangedFunc(func(row int, column int) {
		fileBrowser.fileSelection = fileBrowser.fileEntries[row]
	})

	table.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		key := event.Key()
		if key == tcell.KeyRight {
			fileBrowser.SetPath(fileBrowser.fileSelection.Path)
			fileBrowser.updateTableContents()
			return nil
		} else if key == tcell.KeyLeft {
			fileBrowser.goUp()
			return nil
		}
		return event
	})

	table.SetDoneFunc(func(key tcell.Key) {
		if key == tcell.KeyEscape {
			application.Stop()
		}
	})

	infoLayout := tview.NewFlex().SetDirection(tview.FlexRow)
	infoLayout.AddItem(datasetInfoBox, 0, 1, false)
	infoLayout.AddItem(snapshotsInfoBox, 0, 1, false)
	fileBrowserLayout.AddItem(infoLayout, 0, 1, false)

	fileBrowserLayout.AddItem(table, 0, 2, true)

	fileBrowser.page = fileBrowserLayout
}

func (fileBrowser *FileBrowser) createDatasetInfoBox() *tview.Flex {
	layout := tview.NewFlex().SetDirection(tview.FlexRow)
	layout.SetBorder(true)
	layout.SetTitle(" Dataset ")

	dataset := fileBrowser.currentDataset
	datasetPath := tview.NewTextView().SetText(dataset.Path)

	layout.AddItem(datasetPath, 0, 1, false)

	return layout
}

func (fileBrowser *FileBrowser) createSnapshotsInfoBox() *tview.Flex {
	layout := tview.NewFlex().SetDirection(tview.FlexRow)
	layout.SetBorder(true)
	layout.SetTitle(" Snapshots ")

	snapshots := fileBrowser.snapshots
	for _, snapshot := range snapshots {
		datasetPath := tview.NewTextView().SetText(snapshot.Name)
		layout.AddItem(datasetPath, 0, 1, false)
	}
	return layout
}

type FileBrowserFileSnapshotEntry struct {
	Path         string
	OriginalPath string
	Stat         os.FileInfo
	Snapshot     *zfs.Snapshot
}

type FileBrowserEntry struct {
	Name         string
	Path         string
	Stat         os.FileInfo
	SnapshotOnly bool
	Snapshots    []*FileBrowserFileSnapshotEntry
}

func (fileBrowserEntry *FileBrowserEntry) HasSnapshots() bool {
	return len(fileBrowserEntry.Snapshots) > 0
}

func (fileBrowser *FileBrowser) readDirectory(path string) {
	// TODO: list latestFiles and directories in "real" $path as well as all (or a small subset) of the snaphots in a merged view, with an indication of
	//  - whether the file was deleted (compared to the "real" state)
	//  - how many versions there are of this file in snapshots

	latestFiles, err := util.ListFilesIn(path)
	if err != nil {
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

		matchingFilesInSnapshots := []*FileBrowserFileSnapshotEntry{}
		for _, snapshot := range fileBrowser.snapshots {
			snapshotPath := snapshot.GetPath(file)
			stat, err := os.Stat(snapshotPath)
			if os.IsNotExist(err) {
				continue
			} else if err != nil {
				logging.Error(err.Error())
				continue
			} else {
				matchingFilesInSnapshots = append(matchingFilesInSnapshots, &FileBrowserFileSnapshotEntry{
					Path:         snapshotPath,
					OriginalPath: file,
					Stat:         stat,
					Snapshot:     snapshot,
				})
			}
		}

		mergedFileEntries = append(mergedFileEntries, &FileBrowserEntry{
			Name: name,
			Stat: stat,
			Path: file,
			// TODO: include entries for items, which are only found in a snapshot
			SnapshotOnly: false,
			Snapshots:    matchingFilesInSnapshots,
		})
	}

	fileBrowser.fileEntries = mergedFileEntries
	fileBrowser.filesInLatest = latestFiles

	fileBrowser.SortEntries()

	fileBrowser.SelectEntry(0)
}

func (fileBrowser *FileBrowser) GetView() {

}

func (fileBrowser *FileBrowser) goUp() {
	fileBrowser.SetPath(path2.Dir(fileBrowser.path))
}

func (fileBrowser *FileBrowser) enterDir(name string) {
	newPath := path2.Join(fileBrowser.path, name)
	fileBrowser.SetPath(newPath)
}

func (fileBrowser *FileBrowser) SetPath(newPath string) {
	stat, err := os.Stat(newPath)
	if err != nil {
		logging.Error(err.Error())
		// cannot enter path, ignoring
	} else if !stat.IsDir() {
		logging.Warning("Tried to enter path which is not a directory: %s", newPath)
		fileBrowser.SetPath(path2.Dir(newPath))
		return
	} else {
		fileBrowser.path = newPath
		fileBrowser.updateZfsInfo()
		fileBrowser.readDirectory(fileBrowser.path)
		fileBrowser.updateTableContents()
	}
}

func (fileBrowser *FileBrowser) openActionDialog(selection string) {

}

func (fileBrowser *FileBrowser) checkIfFileHasChanged(originalFile *FileBrowserEntry, snapshotFile *FileBrowserFileSnapshotEntry) bool {
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
	fileBrowser.currentDataset = zfs.FindHostDataset(fileBrowser.path)
	snapshots, err := fileBrowser.currentDataset.GetSnapshots()
	if err != nil {
		logging.Fatal(err.Error())
	}
	fileBrowser.snapshots = snapshots
	if len(fileBrowser.snapshots) > 0 {
		fileBrowser.currentSnapshot = fileBrowser.snapshots[0]
	} else {
		fileBrowser.currentSnapshot = nil
	}
}

func (fileBrowser *FileBrowser) updateTableContents() {
	columnTitles := []FileBrowserColumn{Size, Status, Name}

	table := fileBrowser.table
	if table == nil {
		return
	}

	table.Clear()

	cols, rows := len(columnTitles), len(fileBrowser.fileEntries)
	fileIndex := 0
	for row := 0; row < rows; row++ {
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
}

func (fileBrowser *FileBrowser) SelectEntry(i int) {
	if len(fileBrowser.fileEntries) > 0 {
		fileBrowser.fileSelection = fileBrowser.fileEntries[i]
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
