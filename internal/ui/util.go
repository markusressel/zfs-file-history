package ui

import "github.com/rivo/tview"

type DialogOption struct {
	Name string
}

func createModal(title string, content tview.Primitive, width, height int) tview.Primitive {
	dialogFrame := tview.NewFlex()
	dialogFrame.SetBorder(true)
	dialogFrame.SetTitle(title)
	dialogFrame.AddItem(content, 0, 1, true)

	return tview.NewFlex().
		AddItem(nil, 0, 1, false).
		AddItem(tview.NewFlex().SetDirection(tview.FlexRow).
			AddItem(nil, 0, 1, false).
			AddItem(dialogFrame, height, 1, true).
			AddItem(nil, 0, 1, false), width, 1, true).AddItem(nil, 0, 1, false)
}
