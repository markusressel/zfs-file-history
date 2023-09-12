package ui

import (
	"github.com/rivo/tview"
)

func CreateUi(path string, fullscreen bool) *tview.Application {
	application := tview.NewApplication()

	rootLayout := createRootLayout()

	fileBrowser := NewFileBrowser(path)
	fileBrowser.SetPath(path)
	fileBrowser.Layout(application)

	rootLayout.AddItem(fileBrowser.page, 0, 1, true)

	return application.SetRoot(rootLayout, fullscreen).SetFocus(fileBrowser.table)
}

func createRootLayout() *tview.Flex {
	rootLayout := tview.NewFlex()
	rootLayout.SetBorder(true)
	rootLayout.SetTitle("  zfs-file-history  ")
	return rootLayout
}
