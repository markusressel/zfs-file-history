package ui

import (
	"fmt"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

type MainPage struct {
	application *tview.Application
	fileBrowser *FileBrowser
	layout      *tview.Flex
}

func NewMainPage(application *tview.Application, path string) *MainPage {
	fileBrowser := NewFileBrowser(application, path)

	mainPage := &MainPage{
		application: application,
		fileBrowser: fileBrowser,
	}

	mainPage.layout = mainPage.createLayout()

	return mainPage
}

func (mainPage *MainPage) createLayout() *tview.Flex {
	mainPageLayout := tview.NewFlex().SetDirection(tview.FlexRow)

	//dialog := createFileBrowserActionDialog()
	header := NewApplicationHeader()

	mainPageLayout.AddItem(header.layout, 1, 0, false)
	mainPageLayout.AddItem(mainPage.fileBrowser.page, 0, 1, true)

	return mainPageLayout
}

func (mainPage *MainPage) ToggleFocus() {

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
