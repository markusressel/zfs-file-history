package dialog

import (
	"github.com/rivo/tview"
	uiutil "zfs-file-history/internal/ui/util"
)

const (
	DialogCloseActionId DialogActionId = iota
)

type Dialog interface {
	GetName() string
	GetLayout() *tview.Flex
	GetActionChannel() <-chan DialogActionId
}

type DialogActionId int

type DialogOption struct {
	Id   DialogActionId
	Name string
}

func createModal(title string, content tview.Primitive, width int, height int) *tview.Flex {
	dialogFrame := tview.NewFlex()
	dialogFrame.SetBorder(true)
	uiutil.SetupDialogWindow(dialogFrame, title)
	dialogFrame.AddItem(content, 0, 1, true)

	dialogContentColumnWrapper := tview.NewFlex()
	dialogContentColumnWrapper.AddItem(nil, 0, 1, false)

	dialogContentRowWrapper := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(nil, 0, 1, false).
		AddItem(dialogFrame, height, 1, true).
		AddItem(nil, 0, 1, false)

	dialogContentColumnWrapper.
		AddItem(dialogContentRowWrapper, width, 1, true).
		AddItem(nil, 0, 1, false)

	return dialogContentColumnWrapper
}
