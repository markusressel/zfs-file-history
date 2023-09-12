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

type FileBrowser struct {
	currentDataset  *zfs.Dataset
	snapshots       []*zfs.Snapshot
	currentSnapshot *zfs.Snapshot
	path            string
	fileSelection   *FileBrowserEntry
	page            *tview.Flex
	table           *tview.Table
}

func NewFileBrowser(path string) *FileBrowser {
	fileBrowser := FileBrowser{}

	fileBrowser.SetPath(path)

	return &fileBrowser
}

func (fileBrowser *FileBrowser) Layout(application *tview.Application) {
	mergedFileList, latestFiles := fileBrowser.readDirectory(fileBrowser.path)

	fileBrowserLayout := tview.NewFlex().SetDirection(tview.FlexColumn)
	fileBrowserHeaderText := fmt.Sprintf(" %s ", fileBrowser.path)

	// TODO: insert "/.." cell, if path is not /
	// TODO: use arrow keys to navigate up and down the paths

	datasetInfoBox := fileBrowser.createDatasetInfoBox()
	snapshotsInfoBox := fileBrowser.createSnapshotsInfoBox()

	table := tview.NewTable()
	table.SetBorder(true)
	table.SetBorders(true)
	table.SetBorderPadding(0, 0, 1, 1)

	// fixed header row
	table.SetFixed(1, 0)

	table.SetTitle(fileBrowserHeaderText)
	table.SetTitleColor(tcell.ColorBlue)
	table.SetTitleAlign(tview.AlignLeft)

	columnTitles := []string{"Name", "Status", "Size"}

	cols, rows := len(columnTitles), len(mergedFileList)
	fileIndex := 0
	for r := 0; r < rows; r++ {
		currentFilePath := mergedFileList[fileIndex]

		var status = "UNCHANGED"
		var statusColor = tcell.ColorWhite
		if currentFilePath.HasSnapshots() && !slices.Contains(latestFiles, currentFilePath.Path) {
			// file only exists in snapshot but not in latest
			statusColor = tcell.ColorRed
			status = "DELETED"
		} else if !currentFilePath.HasSnapshots() && slices.Contains(latestFiles, currentFilePath.Path) {
			// file only exists in latest but not in snapshot
			statusColor = tcell.ColorGreen
			status = "NEW"
		} else if fileBrowser.checkIfFileHasChanged(currentFilePath, currentFilePath.Snapshots[0]) {

			// TODO: check for changes of this file since the last snapshot version to determine whether this file has changed
			statusColor = tcell.ColorYellow
			//snapshotFile := snapshotFiles[fileIndex]
			//
			//if currentFilePath.Stat.Size() != snapshotFiles[fileIndex]
		}

		for c := 0; c < cols; c++ {
			var color = tcell.ColorWhite

			var cellText string
			var alignment = tview.AlignLeft
			if c == 0 {
				cellText = fmt.Sprintf("%s", currentFilePath.Name)
				var nameColor = color
				if currentFilePath.Stat.IsDir() {
					cellText = fmt.Sprintf("/%s", cellText)
					nameColor = tcell.ColorSteelBlue
				}
				color = nameColor
			} else if c == 1 {
				cellText = status
				color = statusColor
				alignment = tview.AlignCenter
			} else {
				cellText = humanize.IBytes(uint64(currentFilePath.Stat.Size()))
				alignment = tview.AlignRight
			}

			table.SetCell(r, c,
				tview.NewTableCell(cellText).
					SetTextColor(color).
					SetAlign(alignment),
			)
		}
		fileIndex = (fileIndex + 1) % rows
	}

	table.SetSelectable(true, false)
	// TODO: remember the selected index for a given path and automatically update the fileSelection when entering and exiting a path
	table.Select(0, 0)

	table.SetSelectionChangedFunc(func(row int, column int) {
		//cell := table.GetCell(row, column)
		fileBrowser.fileSelection = mergedFileList[row]
	})

	table.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		key := event.Key()
		if key == tcell.KeyRight {
			fileBrowser.SetPath(fileBrowser.fileSelection.Path)
			// TODO: figure out how to redraw when the state changes
			return nil
		} else if key == tcell.KeyLeft {
			fileBrowser.goUp()
			return nil
		}
		//} else if key == tcell.KeyEnter {
		//	_, column := table.GetSelection()
		//	currentSelection := mergedFileList[column]
		//	fileBrowser.openActionDialog(currentSelection)
		//}
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
	fileBrowser.table = table
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

func (fileBrowser *FileBrowser) readDirectory(path string) (mergedFileEntries []*FileBrowserEntry, latestFiles []string) {
	hostDataset := fileBrowser.currentDataset
	logging.Info("Current Path: %s", path)
	logging.Info("Host Dataset: %s", hostDataset.Path)

	// TODO: list latestFiles and directories in "real" $path as well as all (or a small subset) of the snaphots in a merged view, with an indication of
	//  - whether the file was deleted (compared to the "real" state)
	//  - how many versions there are of this file in snapshots

	latestFiles, err := util.ListFilesIn(path)
	if err != nil {
		logging.Fatal("Cannot list path: %s", err.Error())
	}

	mergedFileList := util.UniqueSlice(latestFiles)

	for _, file := range mergedFileList {
		_, name := path2.Split(file)

		stat, err := os.Stat(file)
		if err != nil {
			logging.Fatal(err.Error())
		}

		matchingFilesInSnapshots := []*FileBrowserFileSnapshotEntry{}
		for _, snapshot := range fileBrowser.snapshots {
			snapshotPath := snapshot.GetPath(file)
			stat, err := os.Stat(snapshotPath)
			if os.IsNotExist(err) {
				continue
			} else if err != nil {
				logging.Fatal(err.Error())
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

	slices.SortFunc(mergedFileEntries, func(a, b *FileBrowserEntry) int {
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

	return mergedFileEntries, latestFiles
}

func (fileBrowser *FileBrowser) GetView() {

}

func (fileBrowser *FileBrowser) goUp() {
	fileBrowser.path = path2.Dir(fileBrowser.path)
}

func (fileBrowser *FileBrowser) enterDir(name string) {
	newPath := path2.Join(fileBrowser.path, name)
	fileBrowser.SetPath(newPath)
}

func (fileBrowser *FileBrowser) SetPath(newPath string) {
	_, err := os.Stat(newPath)
	if err == nil {
		fileBrowser.path = newPath
		fileBrowser.updateZfsInfo()
	} else {
		logging.Error(err.Error())
		// cannot enter path, ignoring
	}
}

func (fileBrowser *FileBrowser) openActionDialog(selection string) {

}

func (fileBrowser *FileBrowser) checkIfFileHasChanged(originalFile *FileBrowserEntry, snapshotFile *FileBrowserFileSnapshotEntry) bool {
	return originalFile.Stat == snapshotFile.Stat
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
	fileBrowser.currentSnapshot = fileBrowser.snapshots[0]
}
