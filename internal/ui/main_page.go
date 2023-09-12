package ui

import (
	"fmt"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"zfs-file-history/internal/logging"
	"zfs-file-history/internal/zfs"
)

type MainPage struct {
	application     *tview.Application
	fileBrowser     *FileBrowser
	datasetInfo     *DatasetInfo
	snapshotBrowser *SnapshotBrowser
	layout          *tview.Flex
}

func NewMainPage(application *tview.Application, path string) *MainPage {
	fileBrowser := NewFileBrowser(application, path)

	datasetInfo := NewDatasetInfo(fileBrowser.application, fileBrowser.currentDataset)
	snapshotBrowser := NewSnapshotBrowser(fileBrowser.application, fileBrowser.snapshots)

	mainPage := &MainPage{
		application:     application,
		fileBrowser:     fileBrowser,
		datasetInfo:     datasetInfo,
		snapshotBrowser: snapshotBrowser,
	}

	mainPage.layout = mainPage.createLayout()

	// listen for selection changes within the file browser
	go func() {
		for {
			select {
			case newSelection := <-fileBrowser.fileSelectionChanged:
				// update Snapshot Browser path
				snapshotsContainingSelection := []*zfs.Snapshot{}
				if newSelection != nil {
					for _, snapshot := range newSelection.Snapshots {
						snapshotsContainingSelection = append(snapshotsContainingSelection, snapshot.Snapshot)
					}
				}
				snapshotBrowser.SetSnapshots(snapshotsContainingSelection)

				// update Dataset Info path
				var datasetPath string
				if newSelection != nil {
					datasetPath = newSelection.Path
				} else {
					datasetPath = ""
				}
				application.QueueUpdateDraw(func() {
					datasetInfo.SetPath(datasetPath)
				})
			}
		}
	}()

	return mainPage
}

func (mainPage *MainPage) createLayout() *tview.Flex {
	mainPageLayout := tview.NewFlex().SetDirection(tview.FlexRow)

	header := NewApplicationHeader()
	mainPageLayout.AddItem(header.layout, 1, 0, false)

	windowLayout := tview.NewFlex().SetDirection(tview.FlexColumn)
	//dialog := createFileBrowserActionDialog()

	windowLayout.AddItem(mainPage.fileBrowser.layout, 0, 2, true)

	infoLayout := tview.NewFlex().SetDirection(tview.FlexRow)
	infoLayout.AddItem(mainPage.datasetInfo.layout, 0, 1, false)
	infoLayout.AddItem(mainPage.snapshotBrowser.snapshotTable, 0, 2, false)
	windowLayout.AddItem(infoLayout, 0, 1, false)

	mainPageLayout.AddItem(windowLayout, 0, 1, true)

	mainPageLayout.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		key := event.Key()
		if key == tcell.KeyTab || key == tcell.KeyBacktab {
			mainPage.ToggleFocus()
		}
		return event
	})

	return mainPageLayout
}

func (mainPage *MainPage) ToggleFocus() {
	if mainPage.fileBrowser.HasFocus() {
		mainPage.snapshotBrowser.Focus()
	} else if mainPage.snapshotBrowser.HasFocus() {
		mainPage.fileBrowser.Focus()
	} else {
		logging.Fatal("Unexpected focus state")
	}
}

func createFileBrowserActionDialog() tview.Primitive {
	dialogTitle := " Select Action "

	optionTable := tview.NewTable()
	optionTable.SetSelectable(true, false)
	optionTable.Select(0, 0)
	optionTable.SetSelectedFunc(func(row, column int) {

	})

	dialogOptions := []*DialogOption{
		{
			Name: "Restore",
		},
	}

	_, rows := 1, len(dialogOptions)
	fileIndex := 0
	for row := 0; row < rows; row++ {
		columnTitle := dialogOptions[row]

		var cellColor = tcell.ColorWhite
		var cellText string
		var cellAlignment = tview.AlignLeft
		var cellExpansion = 0

		cellText = fmt.Sprintf("%s", columnTitle.Name)

		optionTable.SetCell(row, 0,
			tview.NewTableCell(cellText).
				SetTextColor(cellColor).
				SetAlign(cellAlignment).
				SetExpansion(cellExpansion),
		)
		fileIndex = (fileIndex + 1) % rows
	}

	dialog := createModal(dialogTitle, optionTable, 40, 10)
	return dialog
}
